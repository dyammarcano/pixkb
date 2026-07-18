# Plan 001: RAG answer cache never serves un-redacted PII across the PII-filter flag

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If any
> "STOP conditions" item occurs, stop and report — do not improvise. When done,
> update this plan's status row in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat b4e7632..HEAD -- internal/rag/cache.go internal/rag/adapters.go internal/kbmcp/ask.go`
> If any in-scope file changed since this plan was written, compare the "Current
> state" excerpts against the live code before proceeding; on a mismatch, treat
> it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug (security/compliance-adjacent — LGPD PII)
- **Planned at**: commit `b4e7632`, 2026-07-18

## Why this matters

The RAG answer cache (`internal/rag`) is keyed only by `(normalized question, epoch)`.
The `kb_ask` MCP tool exposes a `no_pii_filter` flag that, when true, returns and
**caches un-redacted** answer text. Because the cache key ignores that flag (and every
retrieval-scoping option), a call with `no_pii_filter=true` stores raw PII/LGPD text
under `key(q, epoch)`, and a *subsequent normal* call for the same question and epoch
gets that un-redacted text straight from the cache — the `RedactPII` pass is silently
bypassed. The same collision also serves a `type`/`top_k`/`multi`-scoped answer to a
differently-scoped query. `internal/rag/adapters.go:117-118` even documents the opposite
("redacted before being cached, so a cache hit and a cache miss return
identically-redacted text") — the code contradicts its own contract. This is a
compliance leak on the long-running MCP server, where the cache actually accumulates
hits.

## Current state

- `internal/rag/cache.go:32` — the key function, question+epoch only:
  ```go
  func CacheKey(question string, epoch int) string {
      norm := strings.ToLower(strings.Join(strings.Fields(question), " "))
      sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", epoch, norm)))
      return hex.EncodeToString(sum[:])
  }
  ```
- `internal/rag/adapters.go:121-146` — `Ask`: computes `key = CacheKey(q, opts.Epoch)`,
  returns a cache hit *before* synth/redact, and on a miss caches `a` after redacting
  only when `!opts.NoPIIFilter`:
  ```go
  var key string
  if opts.Cache != nil {
      key = CacheKey(q, opts.Epoch)
      if a, ok := opts.Cache.Get(key); ok {
          return a, g, nil
      }
  }
  a, err := Synthesize(ctx, gen, g)
  ...
  if !opts.NoPIIFilter {
      a.Text = RedactPII(a.Text)
  }
  if opts.Cache != nil {
      opts.Cache.Put(key, a)
  }
  ```
- `internal/kbmcp/ask.go` — the `kb_ask` tool passes `NoPIIFilter`, `Type`, `TopK`,
  `Multi`, `Diversify`, `Expand`, `ExpandSeeds`, `ExpandSimilar`, `MinScore` into
  `rag.Options`; none reach the key.
- `Options` is defined in `internal/rag/rag.go` (search for `type Options struct`).

Repo conventions: table-driven tests with `github.com/stretchr/testify/require`; see
`internal/rag/cache_test.go` for the existing cache tests to extend. No AI attribution
in commits; conventional-commit messages.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `go build ./...` | exit 0 |
| Test (rag) | `go test ./internal/rag/ -count=1` | ok, all pass |
| Vet | `go vet ./internal/rag/` | exit 0 |
| Lint | `golangci-lint run ./internal/rag/` | `0 issues.` |

## Scope

**In scope:**
- `internal/rag/cache.go` (extend `CacheKey`)
- `internal/rag/adapters.go` (call site + fix the stale doc comment)
- `internal/rag/cache_test.go` (add tests)

**Out of scope (do NOT touch):**
- `RedactPII` and the PII patterns — the redactor itself is correct; the bug is the cache.
- The `LRUCache` internals (`cache.go:38-97`) — unchanged.
- `internal/kbmcp/ask.go` — it already passes the flags into `Options`; no change needed.

## Git workflow

- Branch: `advisor/001-rag-cache-pii-key`
- One commit; conventional-commit style, e.g. `fix(rag): key answer cache by PII-filter + retrieval scope`. No AI attribution.
- Do NOT push or open a PR.

## Steps

### Step 1: Make the key cover the PII flag and retrieval scope

Add a variant that folds the answer-affecting options into the key. Prefer the smallest
correct change: a new `CacheKeyFor(q string, opts Options) string` that hashes the
normalized question, epoch, `NoPIIFilter`, `Type`, `TopK`, `Multi`, `Diversify`,
`Expand`, `ExpandSeeds`, `ExpandSimilar`, and `MinScore`. Keep the existing `CacheKey`
(used by tests) delegating to the new function with default options, or inline — but the
production call site in `Ask` must use the scope-aware key.

Then in `Ask` (`adapters.go`), **additionally do not cache when `opts.NoPIIFilter` is
set** — an un-redacted answer should never persist in a shared process cache. Concretely:
guard both the `Get` and the `Put` so a `NoPIIFilter` request neither reads nor writes the
cache. (Belt-and-suspenders with the key change: even a hash collision cannot leak.)

**Verify**: `go build ./...` → exit 0

### Step 2: Fix the stale doc comment

Update the `Ask` doc comment (`adapters.go:112-120`) so it states the real contract: the
cache is scoped by the PII flag and retrieval options, and `no_pii_filter` responses are
never cached.

**Verify**: `go vet ./internal/rag/` → exit 0

### Step 3: Add regression tests

In `cache_test.go`, add tests (see Test plan) and run them.

**Verify**: `go test ./internal/rag/ -count=1` → ok, all pass

## Test plan

Model after the existing `cache_test.go` tests. Add:

1. **PII flag not leaked via cache** — with a fake `AnswerCache`, call `Ask` once with
   `NoPIIFilter=true` for question Q/epoch E, then again with `NoPIIFilter=false` for the
   same Q/E; assert the second call's returned `Answer.Text` is the redacted form (i.e. it
   went through `Synthesize`+`RedactPII`, not served from the first call's raw text). The
   simplest construction: a fake `Generator` that returns text containing a PII token the
   redactor scrubs, and assert the token is absent on the second call.
2. **`NoPIIFilter` responses are not cached** — after a `NoPIIFilter=true` call, assert the
   fake cache received no `Put` (or that a following identical `NoPIIFilter=true` call still
   invokes the generator).
3. **Distinct scopes get distinct keys** — `CacheKeyFor(q, opts{TopK:5})` !=
   `CacheKeyFor(q, opts{TopK:10})`, and same for `Type` and `NoPIIFilter`.

**Verification**: `go test ./internal/rag/ -count=1` → all pass, including the 3 new tests.

## Done criteria

- [ ] `go build ./...` exits 0
- [ ] `go test ./internal/rag/ -count=1` passes with the 3 new tests
- [ ] `golangci-lint run ./internal/rag/` → `0 issues.`
- [ ] A `NoPIIFilter=true` answer is never returned to a `NoPIIFilter=false` caller (test 1)
- [ ] The `Ask` doc comment no longer claims cache hits are always redacted
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

- The excerpts in "Current state" don't match the live code (drift).
- `Options` does not contain the fields named in Step 1 (the API changed) — report the
  actual field set instead of guessing.
- Fixing this appears to require touching `internal/kbmcp/ask.go` or `RedactPII`.

## Maintenance notes

- Any new answer-affecting `Options` field must also be folded into `CacheKeyFor` — add a
  comment on the `Options` struct pointing here.
- Reviewer: confirm both the `Get` and `Put` are guarded for `NoPIIFilter`, and that the
  new key is used at the production call site (not just defined).
