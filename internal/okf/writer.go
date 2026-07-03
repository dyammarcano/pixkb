package okf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// reservedNames are generated, non-concept files ReconcileBundle must preserve.
var reservedNames = map[string]bool{"index.md": true, "log.md": true, "README.md": true}

// ReconcileBundle makes the on-disk bundle an exact materialization of the
// current concept set: it walks the whole bundle and deletes any *.md file
// whose bundle-relative path is not in keep. Generated index/log files are
// preserved. Without this, concepts dropped by a source — whether individual
// junk PDF sections the segmenter no longer emits, or an entire source PDF
// removed from config — orphan on disk and a subsequent Reindex (which reads
// the whole bundle) resurrects them. Empty directories left behind are removed.
func ReconcileBundle(bundleDir string, keep map[string]struct{}) error {
	if _, err := os.Stat(bundleDir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	err := filepath.WalkDir(bundleDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || reservedNames[e.Name()] {
			return nil
		}
		rel, err := filepath.Rel(bundleDir, p)
		if err != nil {
			return err
		}
		id := path.Clean(filepath.ToSlash(rel))
		if _, ok := keep[id]; ok {
			return nil
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("reconcile remove %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile walk: %w", err)
	}
	return pruneEmptyDirs(bundleDir)
}

// pruneEmptyDirs removes now-empty subdirectories left after orphan deletion
// (e.g. a fully-dropped source's directory), bottom-up. The bundle root itself
// is never removed.
func pruneEmptyDirs(bundleDir string) error {
	var dirs []string
	err := filepath.WalkDir(bundleDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() && p != bundleDir {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("prune dirs walk: %w", err)
	}
	// Deepest-first so parents empty out after their children are gone. A dir is
	// prunable when it is empty OR holds only generated reserved files (a stale
	// index.md left behind when a whole source was dropped) — those are
	// meaningless without concepts, so delete them and the dir.
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			return fmt.Errorf("prune dirs read %q: %w", dirs[i], err)
		}
		if !onlyReserved(entries) {
			continue
		}
		for _, e := range entries {
			if err := os.Remove(filepath.Join(dirs[i], e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("prune reserved %q: %w", e.Name(), err)
			}
		}
		if err := os.Remove(dirs[i]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prune dir %q: %w", dirs[i], err)
		}
	}
	return nil
}

// onlyReserved reports whether a directory's entries are all generated reserved
// files (index.md/log.md/README.md) and no concepts or subdirs — i.e. an orphan
// left after its source was dropped. An empty dir also qualifies.
func onlyReserved(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() || !reservedNames[e.Name()] {
			return false
		}
	}
	return true
}

// WriteConcept writes the concept to <bundleDir>/<ID> as a markdown file with
// YAML frontmatter followed by the body. Parent directories are created. The
// concept ID must be a non-empty, forward-slash, bundle-relative path.
func WriteConcept(bundleDir string, c Concept) error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("write concept: empty id")
	}
	front, err := marshalFrontmatter(c)
	if err != nil {
		return fmt.Errorf("write concept %q: %w", c.ID, err)
	}
	rel := filepath.FromSlash(c.ID)
	dest := filepath.Join(bundleDir, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("write concept %q: mkdir: %w", c.ID, err)
	}

	var buf strings.Builder
	buf.WriteString(fence)
	buf.WriteByte('\n')
	buf.Write(front)
	buf.WriteString(fence)
	buf.WriteByte('\n')
	buf.WriteString(c.Body)

	if err := os.WriteFile(dest, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("write concept %q: %w", c.ID, err)
	}
	return nil
}
