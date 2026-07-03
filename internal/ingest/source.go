// Package ingest holds source adapters (git, pdf, iso-spec, api-doc) that
// normalize external knowledge into OKF concepts for the canonical bundle.
package ingest

import (
	"context"

	"pixkb/internal/okf"
)

// Source is one provider of OKF concepts (a repo, a PDF, an ISO-20022 spec
// set, an API doc tree). Fetch is the only operation that may touch the
// network/disk; it returns concepts ready to be written into the bundle.
type Source interface {
	Fetch(ctx context.Context) ([]okf.Concept, error)
	Name() string
}

// RepoSpec identifies a git repository to ingest.
type RepoSpec struct {
	Owner string
	Name  string
	Ref   string // branch, tag, or commit; empty = default branch
}

// MsgDef is a curated ISO-20022 message definition (PACS/CAMT) used to build a
// message concept without parsing the official XSDs (which are not freely
// redistributable). Links are related message IDs (e.g. "pacs.002").
type MsgDef struct {
	ID      string   // e.g. "pacs.008"
	Title   string   // human title
	Family  string   // "pacs" | "camt"
	Summary string   // one-paragraph description
	Fields  []string // notable fields, "Name — description"
	Links   []string // related message ids, e.g. ["pacs.002","pacs.004"]
	Intent  string   // bilingual search phrases (pt+en) bridging the vocabulary gap
}
