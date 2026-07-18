# Plan 003: Guard embedder output length before dereferencing (no index-panic)

> **Executor instructions**: Follow step by step; run every verify command. On any STOP
> condition, stop and report. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat b4e7632..HEAD -- internal/query/hybrid.go internal/query/multi.go internal/embed/openai.go`

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `b4e7632`, 2026-07-18

## Why this matters

Two spots dereference an embedder's output by position with no length guard. If an
`Embedder` returns fewer vectors than inputs (the interface permits it — the OpenAI
embedder returns `nil, nil` for empty input), the code panics with index-out-of-range and
takes down the request/MCP server instead of returning an error. Latent today (in-repo
embedders happen to return one vector per one input), but it's a cheap, high-certainty
safety fix on the hot search path.

## Current state

- `internal/query/hybrid.go:184-188` — `hybridCore`:
  ```go
  vecs, err := emb.Embed(ctx, []string{q})
  if err != nil {
      return nil, nil, err
  }
  vecHits, err := s.Vector(ctx, vecs[0], f)   // vecs[0] unguarded
  ```
  Reached from `MultiHybrid` (`internal/query/multi.go:77`) and every RAG retrieval.
- `internal/embed/openai.go:114-120` — after `sort.Slice(er.Data, … Index < Index)`, it
  assigns `out[i] = er.Data[i].Embedding` by **loop position `i`**, not by the declared
  `er.Data[i].Index`; the only guard is the count check at `openai.go:111`. A server that
  returns the right count but a gapped/duplicated `Index` set silently pairs vectors with
  the wrong input.

Repo conventions: errors wrapped with `fmt.Errorf("...: %w", err)`; testify `require` in
tests. See `internal/query/hybrid_test.go` for the fake-embedder/fake-searcher pattern.

## Commands you will need

| Purpose | Command | Expected |
|---------|---------|----------|
| Build | `go build ./...` | exit 0 |
| Test query | `go test ./internal/query/ -count=1` | ok |
| Test embed | `go test ./internal/embed/ -count=1` | ok |
| Lint | `golangci-lint run ./internal/query/ ./internal/embed/` | `0 issues.` |

## Scope

**In scope:**
- `internal/query/hybrid.go` (length guard in `hybridCore`)
- `internal/embed/openai.go` (place by declared `Index`, bounds-checked)
- `internal/query/hybrid_test.go` (add a zero-vector-return test)
- `internal/embed/openai_test.go` (add a gapped-index test; create if absent)

**Out of scope:** the `Embedder` interface signature; the hashing embedder; `s.Vector`.

## Git workflow

- Branch: `advisor/003-embed-length-guard`
- One commit: `fix(query,embed): guard embedder output length and index mapping`. No AI attribution.
- Do NOT push.

## Steps

### Step 1: Guard `vecs[0]` in `hybridCore`

After the `Embed` call, before `s.Vector`, add:
```go
if len(vecs) == 0 {
    return nil, nil, fmt.Errorf("embedder returned no vector for query")
}
```
Confirm `fmt` is imported (it is used elsewhere in the file).

**Verify**: `go build ./...` → exit 0

### Step 2: Place OpenAI embeddings by declared index

In `openai.go`, replace the position-based assignment with an index-based one: allocate
`out := make([][]float32, len(texts))`, and for each `d := range er.Data` write
`out[d.Index] = d.Embedding` **only when `d.Index >= 0 && d.Index < len(texts)`**, else
return an error naming the bad index. Keep the existing count check. This removes the
`sort.Slice` dependency for correctness (you may keep or drop the sort — index placement no
longer needs it; prefer dropping it as dead once placement is by index).

**Verify**: `go build ./...` → exit 0

### Step 3: Tests

Add the two tests from the Test plan and run them.

**Verify**: `go test ./internal/query/ ./internal/embed/ -count=1` → ok

## Test plan

- **hybrid zero-vector guard** (`hybrid_test.go`): a fake embedder returning `(nil, nil)`;
  assert `Hybrid`/`hybridCore` returns a non-nil error and does not panic.
- **openai gapped index** (`openai_test.go`): construct an embedding response with the
  correct count but `Index` values `{0,0}` for two inputs (or `{0,2}`); assert an error is
  returned rather than a mis-paired result. If `openai_test.go` needs an HTTP stub, model
  it after any existing test in `internal/embed/` that stubs the transport; if none exists,
  test the response-mapping helper directly if it is a separate function, otherwise skip
  this sub-test and note it (STOP condition).

**Verification**: `go test ./internal/embed/ ./internal/query/ -count=1` → all pass.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/query/ ./internal/embed/ -count=1` passes with new tests
- [ ] `golangci-lint run ./internal/query/ ./internal/embed/` → `0 issues.`
- [ ] `hybridCore` no longer indexes `vecs[0]` without a length check
- [ ] OpenAI embeddings are placed by declared `Index`, bounds-checked
- [ ] `plans/README.md` status row updated

## STOP conditions

- Excerpts don't match live code (drift).
- The OpenAI mapping is not isolable for a unit test and there is no existing transport-stub
  pattern to copy — implement Step 1 + Step 2 + the hybrid test, then report the openai test
  gap rather than building a bespoke HTTP mock.

## Maintenance notes

- If a future embedder is added, this guard protects the search path generically — no
  per-embedder change needed.
- Reviewer: confirm Step 2 uses `d.Index` for placement and rejects out-of-range indices.
