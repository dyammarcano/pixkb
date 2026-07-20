-- Make citation-edge idempotency a hard guard rather than a best-effort
-- INSERT ... WHERE NOT EXISTS: a partial unique index on the `cites` kind so
-- concurrent `pixkb link` writers can never duplicate a (src, dst) citation.
-- Scoped to kind='cites' because the `link` kind legitimately carries multiple
-- rows per src (edge sets replaced wholesale by ReplaceEdges).
CREATE UNIQUE INDEX IF NOT EXISTS edge_cites_uniq ON edge(src, dst, kind) WHERE kind = 'cites';
