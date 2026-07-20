# BACEN Pix Flows & pixkb Pipeline

Canonical BACEN view of the Pix/SPB flows — the normative arrangement, not any
single participant's implementation. Actors are the BACEN roles only: **Pagador
PSP**, **Recebedor PSP**, **SPI** (settlement), **DICT** (key directory). Editable
diagrams (`bacen-flows.drawio`, `pixkb-pipeline.drawio`) export to SVG/PNG.

## Pix payment flow (pix-in)

```mermaid
sequenceDiagram
    participant P as Pagador PSP
    participant SPI as SPI (BACEN)
    participant R as Recebedor PSP
    P->>SPI: pacs.008 — EndToEndId (E2EID, prefix E)
    SPI->>R: pacs.008 (liquidação)
    R-->>SPI: pacs.002 — status (ACSC/RJCT)
    SPI-->>P: pacs.002
```

An original Pix is carried as ISO 20022 **pacs.008** (FI to FI Customer Credit
Transfer). The Pagador PSP instructs the **SPI** (Sistema de Pagamentos
Instantâneos, settling over reserves accounts), which relays it to the Recebedor
PSP. Settlement status returns as **pacs.002** (`ACSC` settled, `RJCT` rejected).
The transaction carries an **EndToEndId (E2EID)** — 32 chars, prefix `E` — issued
once by the originating PSP.

## Devolução / refund flow

```mermaid
sequenceDiagram
    participant R as Recebedor PSP
    participant SPI as SPI (BACEN)
    participant P as Pagador PSP
    R->>SPI: pacs.004 / camt.056 — RtrId (prefix D)
    SPI->>P: pacs.004 (devolução)
    P-->>SPI: pacs.002 — status
```

A refund (devolução) is carried as **pacs.004** (Payment Return) and/or
**camt.056** (Payment Cancellation Request), settled back through the SPI. It
carries an **RtrId** — 32 chars, prefix `D` — issued by the refunding PSP. One Pix
can have multiple `RtrId`s (partial refunds).

## MED — Mecanismo Especial de Devolução

```mermaid
sequenceDiagram
    participant U as Usuário pagador
    participant P as Pagador PSP
    participant DICT as DICT (BACEN)
    participant R as Recebedor PSP
    U->>P: relata fraude / falha operacional
    P->>DICT: notificação de infração (infraction report)
    R->>R: análise + bloqueio cautelar do saldo
    R->>SPI: pacs.004 — devolução (RtrId prefix D)
    R->>R: monitora conta até 90 dias (devolução parcial)
```

The **MED (Mecanismo Especial de Devolução)** is BACEN's set of rules and
operational procedures that let a Pix be returned **starting from the receiving
participant**, in cases of founded suspicion of fraud or of an operational/system
failure in the Pix. It was established/expanded by **Resolução BCB nº 147 (16 Nov
2021)**.

The paying user reports the incident to their **Pagador PSP**, which opens a
**notificação de infração (infraction report)** against the receiving account
through **DICT**. The **Recebedor PSP** analyses the report, may place a
**cautelary block** on the funds, and returns them via the ordinary **devolução**
flow (**pacs.004**, `RtrId` prefix `D`, settled through the SPI). Since Resolução
147, infraction reports may also be opened for transactions **settled in the
participant's own books**, but a **refund request** can only be opened for
transactions **settled in the SPI**. When a refund is partial or is rejected for
insufficient balance in the receiving account, the Recebedor PSP must **monitor the
account and refund up to the requested amount for 90 days**, counted from the
original transaction. **MED 2.0** (2025) extends these procedures.

## DICT key resolution

```mermaid
flowchart LR
    P[Pagador PSP] -->|resolve chave CPF/CNPJ/email/tel/EVP| D[DICT]
    D -->|conta + ISPB do recebedor| P
```

Before initiating a Pix by key, the Pagador PSP resolves it against **DICT**
(Diretório de Identificadores de Contas Transacionais) via `GET /entries/{Key}`.
Key ownership transfers go through claims / reivindicação flows.

## Cobrança lifecycle

```mermaid
flowchart LR
    A[POST /cob · /cobv] --> Q[QR Code / Pix Copia e Cola]
    Q --> Pay[Pagador escaneia] --> In[pix-in flow]
    In --> W[webhook ao recebedor]
```

Charges are created through the Pix cobrança API (`POST /cob` immediate, `/cobv`
due-dated), producing a dynamic QR Code / Pix Copia e Cola payload that triggers
the pix-in flow; status is delivered to the merchant via a registered webhook.

## pixkb pipeline

![pixkb pipeline](pixkb-pipeline.svg)

pixkb ingests heterogeneous sources through GatherAll + CrossLink into the Epoch
Runner, which writes the canonical OKF bundle (`kb-data/`, git source of truth)
and the derived Postgres + pgvector index. Query is hybrid (title-weighted FTS +
vector KNN, RRF). An eval loop (Codex-as-judge) scores relevance/precision and
feeds fixes back. Agents reach the KB only through pixkb's MCP verbs.
