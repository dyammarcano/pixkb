package okf

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// logFile is the bundle-level epoch log, excluded from concept enumeration.
const logFile = "log.md"

// indexFile is the progressive-disclosure index basename written per directory
// and at the bundle root; it is generated navigation, never a concept.
const indexFile = "index.md"

// isNonConcept reports whether a bundle-relative basename is generated
// navigation/log (index.md, log.md) rather than a concept.
func isNonConcept(base string) bool {
	return base == logFile || base == indexFile
}

// ReadBundle walks bundleDir and reads every .md file as a Concept, SKIPPING
// generated files at any depth: log.md and index.md (root and per-directory
// progressive-disclosure indexes). Results are sorted by ID for determinism.
func ReadBundle(bundleDir string) ([]Concept, error) {
	var concepts []Concept
	err := filepath.WalkDir(bundleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(bundleDir, path)
		if relErr != nil {
			return relErr
		}
		// Skip generated navigation/log files at any depth (basename match).
		if isNonConcept(strings.ToLower(filepath.Base(rel))) {
			return nil
		}
		c, readErr := ReadConcept(path, bundleDir)
		if readErr != nil {
			return readErr
		}
		concepts = append(concepts, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read bundle %q: %w", bundleDir, err)
	}
	sort.Slice(concepts, func(i, j int) bool {
		return concepts[i].ID < concepts[j].ID
	})
	return concepts, nil
}
