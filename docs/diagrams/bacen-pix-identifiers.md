# BACEN Pix Identifiers (E2EID & RtrId)

Canonical BACEN definition of the two identifiers that flow through every Pix
transaction. Specification-level only — this is what BACEN defines, independent
of how any participant stores or routes it.

## end_to_end_id (E2EID) — original Pix transaction

The canonical BACEN identifier for a single Pix transaction. **Generated once by
the originating PSP** and travels with the transaction through every PSP and SPI
hop, end-to-end.

**Format: 32 characters, fixed structure.**

```mermaid
flowchart LR
    E["E"] --- I["ISPB (8) — originating PSP"] --- T["YYYYMMDDHHMM (12) — issuance, BRT"] --- S["suffix (11) — unique alphanumeric"]
```

```
E 53891234 202606231335 XYZ1A2B3CDE
^ ^^^^^^^^ ^^^^^^^^^^^^ ^^^^^^^^^^^
│ │        │            └ 11-char unique suffix (alphanumeric)
│ │        └ YYYYMMDDHHMM — timestamp of issuance (12 digits, BRT)
│ └ ISPB of the originating PSP (8 digits)
└ Literal prefix: 'E'    Total = 1 + 8 + 12 + 11 = 32
```

In ISO 20022 it is the `EndToEndId` element of the pacs.008 message.

## rtr_id (RtrId) — return transaction (devolução)

The BACEN identifier for a **return transaction (devolução)**, issued when a
refund is initiated. Same 32-char structure as E2EID, stamped by the PSP that
originates the **return** (not the original Pix). **One Pix can have multiple
`RtrId`s** (partial refunds). Prefix is **`D`** (devolução). In ISO 20022 it is
the `RtrId` of the pacs.004 message.

```
D 00315557 202606231336 69be1e8e6f1   ← D + ISPB of refunding PSP + return timestamp + suffix
```

## Disambiguation

| Starts with | Identifier | Issued by | When |
|---|---|---|---|
| `E` + 31 | end_to_end_id (E2EID) — original Pix | originating PSP | at transaction time |
| `D` + 31 | rtr_id (RtrId) — devolução/refund | refunding PSP | at refund time (≥ original) |

## Notes (BACEN-normative)

- The **ISPB encoded in the prefix is the ISSUER** of that leg, not the account
  holder. For the underlying transaction's originating PSP, use the E2EID's ISPB.
- The **timestamp is BRT** (`America/Sao_Paulo`); convert to UTC before comparing
  with UTC timestamps.
- The `E`/`D` prefix is the early signal of which BACEN flow applies: original
  Pix (pacs.008) vs devolução (pacs.004 / camt.056).
