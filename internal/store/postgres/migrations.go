package postgres

import "embed"

// SchemaFS embeds the golang-migrate SQL migration set. The bundle is
// canonical; this schema is the derived, rebuildable pgvector index.
//
//go:embed schema/*.sql
var SchemaFS embed.FS
