package okf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadConcept reads a single bundle file at path and reconstructs the Concept.
// The ID is computed as the forward-slash path of file relative to bundleDir.
// Links are parsed from the body.
func ReadConcept(path, bundleDir string) (Concept, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Concept{}, fmt.Errorf("read concept %q: %w", path, err)
	}
	front, body, err := splitDocument(raw)
	if err != nil {
		return Concept{}, fmt.Errorf("read concept %q: %w", path, err)
	}
	fm, err := unmarshalFrontmatter(front)
	if err != nil {
		return Concept{}, fmt.Errorf("read concept %q: %w", path, err)
	}

	rel, err := filepath.Rel(bundleDir, path)
	if err != nil {
		return Concept{}, fmt.Errorf("read concept %q: rel to bundle: %w", path, err)
	}
	id := filepath.ToSlash(rel)

	c := Concept{
		ID:          id,
		Type:        fm.Type,
		Title:       fm.Title,
		Description: fm.Description,
		Resource:    fm.Resource,
		Tags:        fm.Tags,
		Language:    fm.Language,
		Timestamp:   fm.Timestamp,
		Epoch:       fm.Epoch,
		ContentSHA:  fm.ContentSHA,
		SourceURI:   fm.SourceURI,
		IntentTerms: fm.IntentTerms,
		EmbeddedAt:  fm.EmbeddedAt,
		EmbedModel:  fm.EmbedModel,
		Body:        body,
		Links:       resolveLinks(id, body),
	}
	return c, nil
}

// resolveLinks parses body links and joins relative-but-sibling targets to the
// owning concept's directory so each link is a full bundle-relative ID.
func resolveLinks(ownerID, body string) []string {
	dir := strings.TrimSuffix(ownerID, filepath.Base(ownerID))
	dir = strings.TrimSuffix(dir, "/")
	raw := ParseLinks(body)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{})
	for _, l := range raw {
		full := l
		if dir != "" && !strings.Contains(l, "/") {
			full = dir + "/" + l
		}
		if _, ok := seen[full]; ok {
			continue
		}
		seen[full] = struct{}{}
		out = append(out, full)
	}
	return out
}
