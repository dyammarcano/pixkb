// Package hql implements HQL — a JQL-style structured query DSL for
// filtering pixkb's concept store (legal articles, API endpoints, manual
// sections, and other knowledge concepts).
//
// A query is lexed and parsed (Parse) into a transport-agnostic AST (Query),
// which a later compiler stage (Query.ToSQL) renders into a parameterized
// Postgres WHERE/ORDER BY fragment over the concept table.
//
// Grammar: field comparisons (= != ~ !~ < > <= >=), IN / NOT IN, IS [NOT]
// EMPTY, boolean AND/OR/NOT with parentheses (precedence NOT > AND > OR),
// functions now()/today()/startOfDay()/endOfDay(), relative-date literals
// like -7d, an optional ORDER BY <field> [ASC|DESC], and an optional LIMIT <n>.
//
// Example:
//
//	type = "LegalArticle" AND domain = "tax" AND updated >= -7d ORDER BY updated DESC LIMIT 20
//
// The parser is field-name-agnostic: it does not validate field names or
// operator/kind compatibility — that is the responsibility of a later
// schema-aware compilation stage.
package hql
