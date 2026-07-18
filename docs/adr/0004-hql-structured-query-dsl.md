# ADR 0004 — HQL structured-query DSL for `search --where`

- **Status:** Accepted
- **Date:** 2026-07-17
- **Context item:** BACKLOG P2 "structured query DSL"; specs/plans 2026-07-17 (HQL v1/v2)

## Context

Ranked hybrid search (FTS + vector via RRF) answers "what is most relevant to
this text", but there was no way to ask precise structured questions —
"LegalArticles in livro I with domain:tax", "concepts of type ApiEndpoint tagged
X". A port of herald's `internal/hql` (lexer / parser / AST / SQL codegen)
already existed and fit the need. The sensitive question was how to compose the
DSL's generated SQL into the existing parameterized search queries without
opening an injection hole.

## Options

1. **No DSL — extend the CLI with more typed flags** (`--type`, `--tag`, …).
   Simple, but every new predicate is a new flag; no boolean composition, no
   range/relational operators.
2. **Port HQL as a standalone filter** that compiles to a SQL `WHERE` fragment
   plus positional args, composed into the FTS/Vector queries.

## Decision

**Option 2 — port HQL, compose it injection-safely.** Key decisions:

- **Whitelisted surface:** columns resolve through a fixed field map, operators
  through an allow-list; only column-name constants are ever interpolated —
  every value goes through a positional `$N` placeholder.
- **Offset-numbered codegen:** `Query.ToSQLAt(startArg)` renders placeholders
  starting at a caller-supplied offset, so the fragment slots into an existing
  arg list without ever renumbering by string rewriting.
- **Store-agnostic composition:** a `postgres.Filter.HQLWhere func(startArg int)`
  closure carries the fragment into the FTS and Vector query builders.
- **v1** shipped as a standalone `--where` filter; **v2** folded it into ranked
  hybrid search (`search --where`) and added the MCP `query` verb.

## Consequences

- **Positive:** precise structured retrieval alongside ranked search; zero
  injection surface (whitelist + positional args, confirmed by review and the
  maturity Security audit); unit-testable codegen (asserts generated SQL + args
  without a DB).
- **Negative:** a new DSL to learn and maintain (lexer/parser/codegen); the
  `~`/`!~` operators needed a substring-wrapping fix caught in review.
- **Neutral:** ranking math is unchanged — HQL only filters the candidate set.

## Follow-ups

- The `Match` operator and a bitemporal field were deferred past v2 (BACKLOG).
