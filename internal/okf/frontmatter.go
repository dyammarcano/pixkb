package okf

import (
	"bytes"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// frontmatter mirrors the OKF-required + pixkb-extension fields of Concept
// as they are serialized in YAML frontmatter. Body, ID, and Links are not
// stored here: ID is the file path, Body is the markdown after the fences,
// and Links are derived from the body on read.
type frontmatter struct {
	Type        string    `yaml:"type"`
	Title       string    `yaml:"title,omitempty"`
	Description string    `yaml:"description,omitempty"`
	Resource    string    `yaml:"resource,omitempty"`
	Tags        []string  `yaml:"tags,omitempty"`
	Language    string    `yaml:"language,omitempty"`
	Timestamp   time.Time `yaml:"timestamp"`
	Epoch       int       `yaml:"epoch"`
	ContentSHA  string    `yaml:"content_sha"`
	SourceURI   string    `yaml:"source_uri,omitempty"`
	Domain      string    `yaml:"domain,omitempty"`
	NormRef     string    `yaml:"norm_ref,omitempty"`
	IntentTerms string    `yaml:"intent_terms,omitempty"`
	EmbeddedAt  time.Time `yaml:"embedded_at"`
	EmbedModel  string    `yaml:"embed_model,omitempty"`
}

// fence is the YAML frontmatter delimiter line.
const fence = "---"

// marshalFrontmatter renders the concept's metadata as YAML bytes (no fences,
// no body).
func marshalFrontmatter(c Concept) ([]byte, error) {
	fm := frontmatter{
		Type:        c.Type,
		Title:       c.Title,
		Description: c.Description,
		Resource:    c.Resource,
		Tags:        c.Tags,
		Language:    c.Language,
		Timestamp:   c.Timestamp,
		Epoch:       c.Epoch,
		ContentSHA:  c.ContentSHA,
		SourceURI:   c.SourceURI,
		Domain:      c.Domain,
		NormRef:     c.NormRef,
		IntentTerms: c.IntentTerms,
		EmbeddedAt:  c.EmbeddedAt,
		EmbedModel:  c.EmbedModel,
	}
	out, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("marshal frontmatter: %w", err)
	}
	return out, nil
}

// unmarshalFrontmatter parses YAML frontmatter bytes into a frontmatter value.
func unmarshalFrontmatter(front []byte) (frontmatter, error) {
	var fm frontmatter
	if err := yaml.Unmarshal(front, &fm); err != nil {
		return frontmatter{}, fmt.Errorf("unmarshal frontmatter: %w", err)
	}
	return fm, nil
}

// splitDocument separates a "--- yaml --- body" document into the raw YAML
// frontmatter bytes and the markdown body (everything after the closing fence,
// with the single separating newline consumed).
func splitDocument(raw []byte) ([]byte, string, error) {
	norm := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(norm, []byte(fence+"\n")) {
		return nil, "", fmt.Errorf("document missing opening frontmatter fence")
	}
	rest := norm[len(fence)+1:]
	front, after, ok := bytes.Cut(rest, []byte("\n"+fence+"\n"))
	if !ok {
		return nil, "", fmt.Errorf("document missing closing frontmatter fence")
	}
	body := string(after)
	return front, body, nil
}
