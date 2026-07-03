package okf

import "time"

// Concept is the central type representing a single OKF knowledge-base entry.
// ID is the file path relative to the repository root (e.g. "messages/pacs.008.md").
// Body holds the raw markdown text that follows the closing frontmatter fence.
// Links holds [[wikilink]] targets parsed from Body (populated by ParseLinks).
type Concept struct {
	ID          string
	Type        string
	Title       string
	Description string
	Resource    string
	Tags        []string
	Language    string
	Timestamp   time.Time
	Epoch       int
	ContentSHA  string
	SourceURI   string
	EmbeddedAt  time.Time
	EmbedModel  string
	Body        string
	Links       []string
	// IntentTerms holds agent-generated recall terms (synonyms, alternate
	// phrasings) woven into the FTS index but kept OUT of the rendered body, so
	// the canonical BACEN content stays clean (see ADR 0001).
	IntentTerms string
}
