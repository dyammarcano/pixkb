package epoch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/store/postgres"
)

// Runner orchestrates an ingest pass: it writes the canonical OKF bundle, cuts
// a new epoch, updates the derived Postgres index (concepts, embeddings, edges,
// bitemporal facts), regenerates indexes, appends log.md, and git-commits.
//
// The write entry points (Run, UpsertBatch, Reindex) are serialized by mu: the
// MCP server holds one Runner and dispatches tool handlers concurrently, and the
// store delegates epoch-allocation serialization to its caller (see
// postgres.Store.NextEpoch). Without this, two concurrent concept_upsert calls
// could both allocate the same epoch (PK conflict), interleave log.md appends,
// or corrupt the shared git worktree mid-stage. Runner is always used as a
// pointer, so the zero-value mutex is never copied.
type Runner struct {
	Bundle string
	Store  *postgres.Store
	Emb    embed.Embedder
	Git    Committer

	mu sync.Mutex // serializes the Run/UpsertBatch/Reindex write path
}

// Result summarizes one epoch.
type Result struct {
	Epoch   int
	Added   int
	Changed int
	Removed int
	Commit  string
}

// Run ingests concepts as a new epoch and returns the result. Removed concepts
// (present before, absent now) are counted but their index rows persist until a
// reindex; the canonical history lives in git regardless.
//
// Run is intentionally NOT atomic across Postgres, the on-disk bundle, and git:
// the OKF markdown bundle is the source of truth, and the Postgres index is a
// derived, rebuildable view. A failure mid-Run can leave a partially-written
// epoch (index rows ahead of, or behind, the bundle/commit). Recovery is
// Reindex, which rebuilds every index row from the canonical bundle — run it
// after any interrupted Run. Do not add cross-system transactions here; treat
// the bundle as authoritative and the index as reconstructible.
func (r *Runner) Run(ctx context.Context, concepts []okf.Concept, source string) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	oldSHA, err := r.Store.CurrentSHAs(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read current shas: %w", err)
	}
	newSHA := make(map[string]string, len(concepts))
	for _, c := range concepts {
		newSHA[c.ID] = c.ContentSHA
	}
	d := diffSets(oldSHA, newSHA)

	n, createdAt, err := r.Store.NextEpoch(ctx, source, "", len(d.Added), len(d.Changed), len(d.Removed))
	if err != nil {
		return Result{}, fmt.Errorf("next epoch: %w", err)
	}

	for _, c := range concepts {
		c.Epoch = n
		c.EmbeddedAt = createdAt
		c.EmbedModel = r.Emb.Name()
		if err := r.applyConcept(ctx, c, createdAt); err != nil {
			return Result{}, err
		}
	}

	// Delete bundle files for concepts no longer emitted by any source, so the
	// on-disk bundle exactly materializes the current set and a later Reindex
	// cannot resurrect removed concepts (e.g. junk PDF sections).
	keep := make(map[string]struct{}, len(concepts))
	for _, c := range concepts {
		keep[c.ID] = struct{}{}
	}
	if err := okf.ReconcileBundle(r.Bundle, keep); err != nil {
		return Result{}, fmt.Errorf("reconcile bundle: %w", err)
	}

	if err := okf.WriteIndexes(r.Bundle); err != nil {
		return Result{}, fmt.Errorf("write indexes: %w", err)
	}
	line := fmt.Sprintf("%s epoch %d (%s): +%d ~%d -%d",
		createdAt.UTC().Format(time.RFC3339), n, source, len(d.Added), len(d.Changed), len(d.Removed))
	if err := okf.AppendLog(r.Bundle, line); err != nil {
		return Result{}, fmt.Errorf("append log: %w", err)
	}

	sha, err := r.Git.Commit(ctx, fmt.Sprintf("epoch %d: %s (+%d ~%d -%d)", n, source, len(d.Added), len(d.Changed), len(d.Removed)))
	if err != nil {
		return Result{}, fmt.Errorf("git commit: %w", err)
	}
	if err := r.Store.SetEpochCommit(ctx, n, sha); err != nil {
		return Result{}, fmt.Errorf("set epoch commit: %w", err)
	}
	// Drop superseded embeddings so vector search stays fast as epochs accumulate.
	if err := r.Store.PruneEmbeddings(ctx); err != nil {
		return Result{}, fmt.Errorf("prune embeddings: %w", err)
	}

	return Result{Epoch: n, Added: len(d.Added), Changed: len(d.Changed), Removed: len(d.Removed), Commit: sha}, nil
}

// UpsertBatch writes concepts into the bundle + index as a new epoch WITHOUT
// diffing against the current set, so a partial agent write-back never removes
// concepts it didn't mention. This is the write path for the agent fleet, which
// reads pixdb (search/related/stats), curates, and writes results back here —
// pixdb as the central source. Use Run for full-corpus ingests (which DO
// reconcile removals); use UpsertBatch for incremental agent contributions.
func (r *Runner) UpsertBatch(ctx context.Context, concepts []okf.Concept, source string) (Result, error) {
	if len(concepts) == 0 {
		return Result{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n, createdAt, err := r.Store.NextEpoch(ctx, source, "", 0, len(concepts), 0)
	if err != nil {
		return Result{}, fmt.Errorf("upsert next epoch: %w", err)
	}
	for _, c := range concepts {
		c.Epoch = n
		c.EmbeddedAt = createdAt
		c.EmbedModel = r.Emb.Name()
		if err := r.applyConcept(ctx, c, createdAt); err != nil {
			return Result{}, err
		}
	}
	if err := okf.WriteIndexes(r.Bundle); err != nil {
		return Result{}, fmt.Errorf("write indexes: %w", err)
	}
	line := fmt.Sprintf("%s epoch %d (%s): ~%d (agent write-back)",
		createdAt.UTC().Format(time.RFC3339), n, source, len(concepts))
	if err := okf.AppendLog(r.Bundle, line); err != nil {
		return Result{}, fmt.Errorf("append log: %w", err)
	}
	sha, err := r.Git.Commit(ctx, fmt.Sprintf("epoch %d: %s (~%d agent write-back)", n, source, len(concepts)))
	if err != nil {
		return Result{}, fmt.Errorf("git commit: %w", err)
	}
	if err := r.Store.SetEpochCommit(ctx, n, sha); err != nil {
		return Result{}, fmt.Errorf("set epoch commit: %w", err)
	}
	if err := r.Store.PruneEmbeddings(ctx); err != nil {
		return Result{}, fmt.Errorf("prune embeddings: %w", err)
	}
	return Result{Epoch: n, Changed: len(concepts), Commit: sha}, nil
}

// applyConcept writes one concept to the bundle and the derived index.
func (r *Runner) applyConcept(ctx context.Context, c okf.Concept, at time.Time) error {
	if err := okf.WriteConcept(r.Bundle, c); err != nil {
		return fmt.Errorf("write concept %q: %w", c.ID, err)
	}
	if err := r.Store.UpsertConcept(ctx, c); err != nil {
		return fmt.Errorf("upsert concept %q: %w", c.ID, err)
	}
	vec, err := r.embed(ctx, c)
	if err != nil {
		return fmt.Errorf("embed %q: %w", c.ID, err)
	}
	if err := r.Store.UpsertEmbedding(ctx, c.ID, c.Epoch, r.Emb.Name(), vec, at); err != nil {
		return fmt.Errorf("upsert embedding %q: %w", c.ID, err)
	}
	if err := r.Store.ReplaceEdges(ctx, c.ID, c.Links); err != nil {
		return fmt.Errorf("replace edges %q: %w", c.ID, err)
	}
	if err := r.Store.RecordFact(ctx, c, at, at); err != nil {
		return fmt.Errorf("record fact %q: %w", c.ID, err)
	}
	return nil
}

func (r *Runner) embed(ctx context.Context, c okf.Concept) ([]float32, error) {
	// Embed title + intent_terms + body. The hashing embedder is bag-of-words
	// cosine — a lexical-overlap proxy — so folding the agent-generated recall
	// synonyms (intent_terms) into the embedded text lets a paraphrase query that
	// shares that vocabulary (but not the title/body wording) score higher on the
	// vector arm. The vector arm is the fuzzy-recall bottleneck (the FTS arm is
	// AND-bound; see search.go), and it previously ignored intent_terms entirely.
	vs, err := r.Emb.Embed(ctx, []string{c.Title + " " + c.IntentTerms + " " + c.Body})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

// Diff returns the concept-level delta between two epochs using the bitemporal
// AsOf view (true historical snapshots).
func (r *Runner) Diff(ctx context.Context, n, m int) (DiffResult, error) {
	oldSHA, err := r.epochSHAs(ctx, n)
	if err != nil {
		return DiffResult{}, err
	}
	newSHA, err := r.epochSHAs(ctx, m)
	if err != nil {
		return DiffResult{}, err
	}
	return diffSets(oldSHA, newSHA), nil
}

func (r *Runner) epochSHAs(ctx context.Context, epoch int) (map[string]string, error) {
	e := epoch
	cs, err := r.Store.AsOf(ctx, postgres.Filter{AsOfEpoch: &e})
	if err != nil {
		return nil, fmt.Errorf("asof epoch %d: %w", epoch, err)
	}
	m := make(map[string]string, len(cs))
	for _, c := range cs {
		m[c.ID] = c.ContentSHA
	}
	return m, nil
}

// Reindex rebuilds the derived Postgres index from the canonical bundle: it
// truncates the index, replays every concept, and records a fresh epoch. The
// bundle is the source of truth, so this is the no-lock-in recovery path. Past
// epoch history is not reconstructed (it lives in git); the current queryable
// state is fully restored.
func (r *Runner) Reindex(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.Store.Truncate(ctx); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	concepts, err := okf.ReadBundle(r.Bundle)
	if err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}
	n, createdAt, err := r.Store.NextEpoch(ctx, "reindex", "", len(concepts), 0, 0)
	if err != nil {
		return fmt.Errorf("reindex next epoch: %w", err)
	}
	for _, c := range concepts {
		c.Epoch = n
		if err := r.applyConcept(ctx, c, createdAt); err != nil {
			return err
		}
	}
	if err := r.Store.PruneEmbeddings(ctx); err != nil {
		return fmt.Errorf("prune embeddings: %w", err)
	}
	return nil
}
