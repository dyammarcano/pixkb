# ISPB Mapper — Design Spec

**Date:** 2026-07-03
**Project:** `pixkb` (`C:\weaver-sync\development\personal\projects\bacen`)
**Status:** Draft — awaiting user approval

## Goal

Add an **ISPB mapper** to `pixkb`: a lookup from an 8-digit ISPB code to its
SPB/Pix participant institution. Two independent BACEN sources feed the same
table:

1. **STR participants CSV** (canonical, broader) — the full registry of every
   STR (Sistema de Transferência de Reservas) participant: banks, DTVMs,
   credit unions, and special entities (Selic, Tesouro). Static URL, clean
   UTF-8 CSV.
2. **Pix participants CSV** (supplementary) — Pix adherents only, with a
   BCB-authorization flag STR doesn't have. Ported from `shield_ispb`
   (`C:\weaver-sync\dell\projects\eligibility\apps\shield_ispb`), dropping all
   `fx`/DI/`entities` coupling.

Confirmed live during design (fetched via sandboxed HTTP, not WebFetch, per
this project's context-mode routing rules):

- **STR**: `GET https://www.bcb.gov.br/content/estabilidadefinanceira/str1/ParticipantesSTR.csv`
  — static filename, no date probing. UTF-8 with BOM, `,`-delimited, RFC4180
  quoting, clean header row (no title-row skip, no fuzzy column match needed).
  Header: `ISPB,Nome_Reduzido,Número_Código,Participa_da_Compe,Acesso_Principal,Nome_Extenso,Início_da_Operação`.
  Verified 47KB / ~1400+ rows.
- **Pix**: `GET https://www.bcb.gov.br/content/estabilidadefinanceira/participantes_pix/lista-participantes-instituicoes-em-adesao-pix-YYYYMMDD.csv`
  — dated filename; BACEN publishes daily but not every date has a file, so
  the client probes dates backward from today (`shield_ispb`'s approach).
  Windows-1252, `;`-delimited, title rows above the real header, fuzzy column
  match by name.

Both are unauthenticated GETs.

## Architecture

```
internal/ispb/                 # pure: no DB import
  participant.go   Participant (merged read model) + ISPB validation (^\d{8}$)
  str.go           STRConfig, DefaultSTRConfig, STRRecord,
                    DownloadSTR(ctx, cfg, log) ([]byte, error),
                    ParseSTR(data, syncedAt) ([]STRRecord, error)
  pix.go           PixConfig, DefaultPixConfig, PixRecord,
                    DownloadPix(ctx, cfg, log) ([]byte, url string, error),
                    ParsePix(data, cfg, syncedAt) ([]PixRecord, error)

internal/store/postgres/
  ispb.go          methods on *Store: UpsertSTR / UpsertPix / GetISPB / ListISPB / CountISPB
  schema/0004_ispb_participant.up.sql
  schema/0004_ispb_participant.down.sql

cmd/pixkb/
  ispb.go          newISPBCmd(): `ispb str {fetch,load,sync}`, `ispb pix {fetch,load,sync}`,
                    `ispb sync` (both), `ispb lookup <code>`
```

**Rationale:** STR and Pix are structurally unrelated CSVs (different
encoding, delimiter, header shape, download strategy) — one file per source
keeps each independently readable and testable, matching "files that change
together live together." `postgres.Store` holds an unexported `pool`; the
established pattern is per-feature method files on `*Store` (`fact.go`,
`vector.go`, `stats.go`), so DB access for ISPB lives there and `internal/ispb`
stays a pure, DB-free library.

## Data model

Two source-specific parse-time DTOs, plus one merged read model returned by
the store:

```go
// internal/ispb/str.go
type STRRecord struct {
    ISPB              string    // 8 digits, zero-padded
    Name              string    // Nome_Reduzido
    LegalName         string    // Nome_Extenso
    CompeCode         string    // Número_Código; "" when source is "n/a" (e.g. Selic, Bacen)
    ParticipatesCompe bool      // Participa_da_Compe: Sim/Não
    AccessType        string    // Acesso_Principal: RSFN | Internet
    OperationStart    time.Time // Início_da_Operação, parsed DD/MM/YYYY
    SyncedAt          time.Time
}

// internal/ispb/pix.go
type PixRecord struct {
    ISPB       string    // 8 digits, zero-padded
    Name       string    // Nome Reduzido
    CNPJ       string    // may be ""
    Authorized bool      // Autorizada pelo BCB
    SyncedAt   time.Time
}

// internal/ispb/participant.go — merged view returned by GetISPB/ListISPB
type Participant struct {
    ISPB               string
    Name               string    // STR Nome_Reduzido if synced, else Pix Nome Reduzido
    LegalName          string    // "" if never STR-synced
    CompeCode          string
    ParticipatesCompe  bool
    AccessType         string
    OperationStart     time.Time // zero if never STR-synced
    STRSyncedAt        time.Time // zero if never STR-synced
    CNPJ               string    // "" if never Pix-synced
    PixAuthorized      bool
    PixSyncedAt        time.Time // zero if never Pix-synced
}
```

`STRRecord.Validate()` / `PixRecord.Validate()` enforce `^\d{8}$` on `ISPB`.

## Config

- `DefaultSTRConfig()`: `URL` (the static URL above), `HTTPTimeout=30s`. No
  date probing — a single GET.
- `DefaultPixConfig()`: ported verbatim from `shield_ispb` — `BaseURL`
  (`%s`-templated), `HTTPTimeout=30s`, `MaxDaysBack=60`, `CSVDelimiter=';'`,
  column names (`ISPB`, `Nome Reduzido`, `CNPJ`, `Autorizada pelo BCB`),
  `AuthorizedValues={sim,yes,s,1}`.

No new `pixkb.yaml` keys in v1. The **DSN is NOT part of either config** — it
is resolved by the CLI layer via the existing `loadConfig()` / `openStore()`.

## Migration — `0004_ispb_participant`

One table, columns partitioned by source; either source can populate a row
independently (`ON CONFLICT` upserts touch only their own columns):

```sql
-- up
CREATE TABLE IF NOT EXISTS ispb_participant (
    ispb_code           VARCHAR(8)  PRIMARY KEY,
    institution_name    TEXT        NOT NULL,
    legal_name          TEXT        NOT NULL DEFAULT '',
    compe_code          TEXT        NOT NULL DEFAULT '',
    participates_compe  BOOLEAN     NOT NULL DEFAULT FALSE,
    access_type         TEXT        NOT NULL DEFAULT '',
    operation_start     DATE,
    str_synced_at       TIMESTAMPTZ,
    cnpj                VARCHAR(14) NOT NULL DEFAULT '',
    pix_authorized      BOOLEAN     NOT NULL DEFAULT FALSE,
    pix_synced_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- down
DROP TABLE IF EXISTS ispb_participant;
```

`UpsertSTR` — STR is canonical, always overwrites the shared `institution_name`:

```sql
INSERT INTO ispb_participant
  (ispb_code, institution_name, legal_name, compe_code, participates_compe,
   access_type, operation_start, str_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name   = EXCLUDED.institution_name,
  legal_name          = EXCLUDED.legal_name,
  compe_code          = EXCLUDED.compe_code,
  participates_compe  = EXCLUDED.participates_compe,
  access_type         = EXCLUDED.access_type,
  operation_start     = EXCLUDED.operation_start,
  str_synced_at       = EXCLUDED.str_synced_at,
  updated_at          = now()
```

`UpsertPix` — only sets `institution_name` on first insert (`COALESCE`), so a
Pix-only sync doesn't clobber a canonical STR name if the row already exists:

```sql
INSERT INTO ispb_participant
  (ispb_code, institution_name, cnpj, pix_authorized, pix_synced_at, updated_at)
VALUES ($1,$2,$3,$4,$5, now())
ON CONFLICT (ispb_code) DO UPDATE SET
  institution_name = COALESCE(ispb_participant.institution_name, EXCLUDED.institution_name),
  cnpj              = EXCLUDED.cnpj,
  pix_authorized    = EXCLUDED.pix_authorized,
  pix_synced_at     = EXCLUDED.pix_synced_at,
  updated_at        = now()
```

Auto-picked up by the embedded `schema/*.sql` FS; applied with the existing
`pixkb db up`.

## CLI — `pixkb ispb`

Cobra parent `newISPBCmd()` with two nested source groups plus merged
top-level commands, appended to `attachCommands(root)` in
`cmd/pixkb/commands.go`. **No `--dsn` flag anywhere** — DB-touching
subcommands do:

```go
cfg := loadConfig()
st, err := openStore(cmd.Context(), cfg)   // errors if DSN unset in config/env
if err != nil { return err }
defer st.Close()
```

Air-gap honored via online-gather → local file → offline-ingest:

| Command | Network | DB | Action |
|---|---|---|---|
| `ispb str fetch --out <path>` | ✅ | — | `DownloadSTR` → write CSV bytes to file |
| `ispb str load --file <path>` | — | ✅ | read file → `ParseSTR` → `UpsertSTR` per row |
| `ispb str sync` | ✅ | ✅ | `DownloadSTR` → `ParseSTR` → `UpsertSTR` |
| `ispb pix fetch --out <path>` | ✅ | — | `DownloadPix` → write CSV bytes to file |
| `ispb pix load --file <path>` | — | ✅ | read file → `ParsePix` → `UpsertPix` per row |
| `ispb pix sync` | ✅ | ✅ | `DownloadPix` → `ParsePix` → `UpsertPix` |
| `ispb sync` | ✅ | ✅ | `str sync` then `pix sync`, stop on first error |
| `ispb lookup <ispb-code>` | — | ✅ | `GetISPB` → print merged record to `cmd.OutOrStdout()` |

Default staged paths: `mirrors/bacen-ispb/str-participants.csv` and
`mirrors/bacen-ispb/pix-participants.csv` (fits the existing pre-staged
`mirrors/` convention). `pix fetch`/`pix sync` accept optional
`--max-days-back int` (default from `DefaultPixConfig`). These are
operational I/O flags, not connection config — the no-flag rule applies
specifically to DSN.

`DownloadSTR`/`DownloadPix` receive a `*slog.Logger` (constructed in the
command, writing to `os.Stderr` per the `mcp.go` pattern).

## Dependencies

Adds **`golang.org/x/text`** for the Pix path only (charmap/transform for
Windows-1252) — already present as an indirect dependency (`go.mod` shows
`golang.org/x/text v0.36.0 // indirect`) and just needs promoting to direct.
The STR path is stdlib-only (`net/http`, `encoding/csv`, `bytes`, `time`,
`strings`, `fmt`). Pure Go — safe under `CGO_ENABLED=0`.

## Testing (testify)

- `internal/ispb/str_test.go` — CSV fixture (BOM + header + data rows,
  including `n/a` compe-code and `Não`/`Sim` rows) → assert parsed
  `[]STRRecord`; `httptest.Server` for `DownloadSTR`.
- `internal/ispb/pix_test.go` — Windows-1252 fixture bytes (title rows +
  header + data + blank/short rows) → assert parsed `[]PixRecord`, padding,
  authorized mapping, column detection; `httptest.Server` for `DownloadPix`
  date-probe behavior (injected `BaseURL`, no real network).
- `internal/store/postgres/ispb_test.go` — `UpsertSTR` alone, `UpsertPix`
  alone, then both against the same ISPB (STR name wins, Pix fields present) →
  `GetISPB`/`ListISPB`/`CountISPB` roundtrip; guarded by `PIXKB_TEST_DSN`,
  skipped under `-short` (matches existing integration tests).

## Out of scope (YAGNI)

- No fx scheduler / daily auto-sync (shield had one; not needed here).
- No sqlc — hand-written SQL via pgx, matching pixkb.
- No MCP tool / concept-store integration in v1 (can be a follow-up).
- No reconciliation/conflict reporting when STR and Pix disagree on a name —
  STR wins deterministically, no logging of the discrepancy.
