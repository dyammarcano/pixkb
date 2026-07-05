package curate_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // registers codex/claude/agy providers
	"pixkb/internal/curate"
	"pixkb/internal/okf"
	_ "pixkb/internal/roster" // registers named roster agents
)

func firstProvider() string {
	for _, p := range []struct{ name, bin string }{{"codex", "codex"}, {"claude", "claude"}} {
		if _, err := exec.LookPath(p.bin); err == nil {
			return p.name
		}
	}
	return ""
}

// TestCurate_RealFixerDryRun drives the Curator with the REAL AgencyFixer (a live
// coding-agent CLI), not the unit-test fake: a junk-titled concept is scanned,
// routed to the hygiene agent, the agent's rewrite is gated by the same
// deterministic detector, and (dry-run) proposed. This proves the production fix
// path end-to-end without touching the database.
//
// Guarded by a provider CLI on PATH and skipped under -short (spends a real
// subscription turn).
func TestCurate_RealFixerDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live curate e2e in -short mode (spends a real subscription turn)")
	}
	prov := firstProvider()
	if prov == "" {
		t.Skip("no codex/claude CLI on PATH")
	}

	// A one-concept bundle with a junk all-caps fragment title (a junk-title
	// hygiene finding the hygiene agent should rewrite).
	bundle := t.TempDir()
	junk := okf.Concept{
		ID: "manuals/m/secao-x.md", Type: "ManualSection",
		Title:     "ONCEITOS GERAIS DE",
		SourceURI: "pdf:manual",
		Body: "# ONCEITOS GERAIS DE\n\nEsta seção define conceitos gerais de iniciação do Pix: " +
			"chave, QR Code, txid e os papéis de PSP pagador e recebedor no arranjo do BACEN.",
		ContentSHA: "sha-x",
	}
	if err := okf.WriteConcept(bundle, junk); err != nil {
		t.Fatalf("write concept: %v", err)
	}

	dir, _ := os.Getwd()
	ag, err := corral.NewAgency(prov, dir)
	if err != nil {
		t.Fatalf("NewAgency: %v", err)
	}
	defer func() { _ = ag.Close() }()

	cur := &curate.Curator{
		Bundle: bundle,
		Fixer:  &curate.AgencyFixer{Agency: ag},
		Apply:  false, // dry-run: run the agent + gate, never write
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()

	out, err := cur.Run(ctx)
	if err != nil {
		t.Fatalf("curate run: %v", err)
	}
	if out.Routed != 1 {
		t.Fatalf("expected 1 routed concept, got %d (%+v)", out.Routed, out.Items)
	}
	if out.Errors != 0 {
		t.Fatalf("agent run errored: %+v", out.Items)
	}
	// The fix must either be proposed (gated clean) or rejected (still tripping a
	// finding) — both are valid loop outcomes; what must NOT happen is an error or
	// a no-route. A clean junk-title rewrite should normally be proposed.
	it := out.Items[len(out.Items)-1]
	if it.Agent != "hygiene" {
		t.Fatalf("junk title should route to hygiene, got %q", it.Agent)
	}
	if it.Status != curate.StatusProposed && it.Status != curate.StatusRejected &&
		it.Status != curate.StatusNoChange {
		t.Fatalf("unexpected status %q (%s)", it.Status, it.Detail)
	}
	t.Logf("real-fixer outcome: %s — %s", it.Status, it.Detail)
}
