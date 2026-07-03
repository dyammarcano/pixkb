package okf

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteIndexes regenerates progressive-disclosure index.md files: one at the
// bundle root listing top-level dirs/concepts, and one per directory listing
// that directory's child concepts and subdirectories. Indexes are derived
// navigation — ReadBundle skips them, so regenerating is always safe.
func WriteIndexes(bundleDir string) error {
	concepts, err := ReadBundle(bundleDir)
	if err != nil {
		return fmt.Errorf("write indexes: read bundle: %w", err)
	}

	// Group concept IDs by their parent directory (bundle-relative, "" = root).
	children := map[string][]string{}
	subdirs := map[string]map[string]struct{}{}
	for _, c := range concepts {
		parent := pathDir(c.ID)
		children[parent] = append(children[parent], c.ID)
		// Register this directory chain under each ancestor as a subdir.
		for anc := parent; anc != ""; anc = pathDir(anc) {
			grand := pathDir(anc)
			if subdirs[grand] == nil {
				subdirs[grand] = map[string]struct{}{}
			}
			subdirs[grand][anc] = struct{}{}
		}
	}

	dirs := map[string]struct{}{"": {}}
	for d := range children {
		dirs[d] = struct{}{}
	}
	for d := range subdirs {
		dirs[d] = struct{}{}
	}

	for d := range dirs {
		if err := writeOneIndex(bundleDir, d, children[d], subdirs[d]); err != nil {
			return err
		}
	}
	return nil
}

func writeOneIndex(bundleDir, dir string, childIDs []string, subs map[string]struct{}) error {
	var b strings.Builder
	title := dir
	if title == "" {
		title = "pixkb knowledge base"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)

	subList := make([]string, 0, len(subs))
	for s := range subs {
		subList = append(subList, s)
	}
	sort.Strings(subList)
	if len(subList) > 0 {
		b.WriteString("## Sections\n\n")
		for _, s := range subList {
			name := lastSegment(s)
			rel := relFrom(dir, s+"/"+indexFile)
			fmt.Fprintf(&b, "- [%s](%s)\n", name, rel)
		}
		b.WriteString("\n")
	}

	sort.Strings(childIDs)
	if len(childIDs) > 0 {
		b.WriteString("## Concepts\n\n")
		for _, id := range childIDs {
			name := lastSegment(id)
			rel := relFrom(dir, id)
			fmt.Fprintf(&b, "- [%s](%s)\n", name, rel)
		}
	}

	dest := filepath.Join(bundleDir, filepath.FromSlash(dir), indexFile)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("write index %q: mkdir: %w", dir, err)
	}
	if err := os.WriteFile(dest, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write index %q: %w", dir, err)
	}
	return nil
}

// pathDir returns the parent directory of a bundle-relative slash path ("" at
// the root).
func pathDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

// lastSegment returns the final path component of a bundle-relative slash path
// (the portion after the last "/" separator).
func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// relFrom returns target as a path relative to dir, for markdown links in an
// index.md located in dir. PRECONDITION: target must be a direct descendant of
// dir (a child concept or a subdirectory) — callers (writeOneIndex) only ever
// pass such paths. For non-descendants it returns target unchanged.
func relFrom(dir, target string) string {
	if dir == "" {
		return target
	}
	return strings.TrimPrefix(target, dir+"/")
}
