# Plan 002: Reproducible lint — committed `.golangci.yml` + pinned CI version

> **Executor instructions**: Follow step by step; run every verification command and
> confirm the expected result. On any "STOP conditions" item, stop and report. Update
> this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**: `git diff --stat b4e7632..HEAD -- .github/workflows/ci.yml Taskfile.yml`

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `b4e7632`, 2026-07-18

## Why this matters

`Taskfile.yml` and CI both run `golangci-lint`, but there is **no committed
`.golangci.yml`** and CI pins the linter to `version: latest`. Lint is therefore
non-reproducible: a new golangci-lint release can turn CI red (or silently drop a
linter) with no code change, and local `task lint` uses whatever binary the developer
happens to have — diverging from CI. The intended strict-linter set is captured nowhere.
This repo has repeatedly hit "the IDE flags it but golangci config doesn't" ambiguity;
a pinned config ends that.

## Current state

- `.github/workflows/ci.yml` — uses `golangci/golangci-lint-action@v6` with
  `version: latest` (search for `golangci-lint-action`).
- `Taskfile.yml` — a `lint` task runs `golangci-lint run` (search for `golangci-lint`).
- Repo-wide: no `.golangci.yml` / `.golangci.yaml` / `.golangci.toml` (confirm with the
  first verify command).
- Go version: read the `go` directive in `go.mod` (use it for the config's `go:` field).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Confirm no config exists | `Get-ChildItem -Force .golangci.* -ErrorAction SilentlyContinue` (PowerShell) | no output |
| Installed linter version | `golangci-lint version` | prints a version — record it |
| Run lint | `golangci-lint run ./...` | `0 issues.` (or a triaged list) |
| Build | `go build ./...` | exit 0 |

## Scope

**In scope:**
- `.golangci.yml` (create)
- `.github/workflows/ci.yml` (pin the version)

**Out of scope (do NOT touch):**
- Source files — this plan is config only. If the pinned config surfaces NEW lint
  findings, do NOT fix them here; record them and STOP (see STOP conditions).
- `Taskfile.yml` lint task command — it already calls `golangci-lint run`; leave it.

## Git workflow

- Branch: `advisor/002-golangci-config-pin`
- One commit, conventional style: `build(ci): pin golangci-lint + commit .golangci.yml`. No AI attribution.
- Do NOT push.

## Steps

### Step 1: Determine the installed golangci-lint version and schema

Run `golangci-lint version` and record the exact version (e.g. `1.64.x` or `2.x`). The
config schema differs between v1 and v2 — match the installed major version. Read
`go.mod`'s `go` directive for the language version.

**Verify**: you have a concrete version string and the go version.

### Step 2: Write `.golangci.yml`

Create a config that (a) sets the `go` version, (b) enables the linter set the repo
already relies on. Base the enabled set on what the code has clearly been written to
satisfy — at minimum keep the default linters (`errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`) plus the ones whose findings appear in this repo's history:
`gofmt`/`gofumpt` if used, `revive` or `stylecheck`, `misspell`, and `usestdlibvars`
(the `strings.SplitSeq`/stdlib-vars findings referenced in past commits). Do NOT invent
an aggressive set that fails the current tree — the goal is to codify today's passing
state, then tighten separately.

Match the installed schema version exactly (v1 uses `linters.enable:`; v2 uses the newer
`version: "2"` layout). If unsure which, run `golangci-lint run` after writing and let a
schema error tell you.

**Verify**: `golangci-lint run ./...` → `0 issues.` (the config must parse AND the tree
must still pass — see STOP if new issues appear).

### Step 3: Pin the CI version

In `.github/workflows/ci.yml`, change the golangci-lint-action `version: latest` to the
exact version from Step 1 (e.g. `version: v1.64.8`). Keep the action major (`@v6`).

**Verify**: `git diff .github/workflows/ci.yml` shows only the version line changed.

## Test plan

No unit tests. The verification is that `golangci-lint run ./...` parses the new config
and reports `0 issues.` on the current tree, and the build stays green.

## Done criteria

- [ ] `.golangci.yml` exists and `golangci-lint run ./...` prints `0 issues.`
- [ ] `go build ./...` exits 0
- [ ] CI no longer contains `version: latest` for golangci-lint (`grep -n "version: latest" .github/workflows/ci.yml` → no match)
- [ ] No source files modified (`git status` shows only `.golangci.yml` and `ci.yml`)
- [ ] `plans/README.md` status row updated

## STOP conditions

- The pinned config surfaces lint findings on the current tree (tree was green before).
  Do NOT fix source to satisfy a newly-enabled linter in this plan — instead, narrow the
  config to today's passing set, note the extra findings as a follow-up, and report.
- `golangci-lint` is not installed in the executor environment (`golangci-lint version`
  errors) — STOP; this plan needs it.
- The installed major version is ambiguous or differs from CI's resolved `latest` — report
  the mismatch rather than guessing a schema.

## Maintenance notes

- Tightening the linter set later is a separate, source-touching change — do it behind its
  own plan so the "codify current state" and "raise the bar" steps stay reviewable apart.
- Reviewer: confirm the pinned CI version equals the version the config was validated
  against, so CI and local agree.
