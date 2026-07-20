package query

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"gopkg.in/yaml.v3"
)

// vocabularyFS embeds every per-domain vocabulary file. Each domain lives in
// its own directory under domains/, so a new domain is added by dropping a
// domains/<domain>/vocabulary.yaml file — no loader change required. The
// directory name is the domain key.
//
//go:embed domains/*/vocabulary.yaml
var vocabularyFS embed.FS

// VocabEntry is one domain-vocabulary mapping: a set of word-stems (folded,
// the same prefix-matching convention ExpandQuery already used for
// entityTriggers) to a canonical subquery, plus an enabled flag and a
// human-readable reason — the audit trail Feature 7 of
// docs/SEARCH-CAPABILITY-SPEC.md requires ("curated, deterministic,
// auditable"). Disabled entries are kept, not deleted, so a maintainer can
// see what was tried and why it isn't live (e.g. a measured eval
// regression) without digging through git history.
type VocabEntry struct {
	Stems    []string `yaml:"stems"`
	Subquery string   `yaml:"subquery"`
	Enabled  bool     `yaml:"enabled"`
	Reason   string   `yaml:"reason"`
}

// vocabFile is a per-domain vocabulary.yaml's top-level shape.
type vocabFile struct {
	Entries []VocabEntry `yaml:"entries"`
}

// parseVocabulary parses the domain-vocabulary YAML format.
func parseVocabulary(data []byte) ([]VocabEntry, error) {
	var f vocabFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse domain vocabulary: %w", err)
	}
	return f.Entries, nil
}

// vocabularies is the per-domain registry, keyed by directory name, loaded
// once from the embedded domains/*/vocabulary.yaml files at package init.
var vocabularies = mustLoadVocabularies(vocabularyFS)

// mustLoadVocabularies walks the embedded domains/ tree and parses each
// domain's vocabulary.yaml, keyed by directory name. It panics on any parse
// failure — the embedded files are committed source, so a failure here is a
// build-time bug, not a runtime condition to recover from.
func mustLoadVocabularies(fsys fs.FS) map[string][]VocabEntry {
	reg := map[string][]VocabEntry{}
	entries, err := fs.ReadDir(fsys, "domains")
	if err != nil {
		panic(err)
	}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		domain := d.Name()
		data, err := fs.ReadFile(fsys, path.Join("domains", domain, "vocabulary.yaml"))
		if err != nil {
			panic(err)
		}
		parsed, err := parseVocabulary(data)
		if err != nil {
			panic(fmt.Errorf("domain %q: %w", domain, err))
		}
		reg[domain] = parsed
	}
	return reg
}

// selectVocabulary merges the registry's entries for the active domain set,
// in deterministic (domain-sorted, then file) order. An empty set merges ALL
// domains — preserving single-domain (pix-only) behavior verbatim. A
// non-empty set merges only those domains' vocabularies; unknown domains
// contribute nothing.
func selectVocabulary(reg map[string][]VocabEntry, domains []string) []VocabEntry {
	var keys []string
	if len(domains) == 0 {
		for k := range reg {
			keys = append(keys, k)
		}
	} else {
		seen := map[string]bool{}
		for _, d := range domains {
			if _, ok := reg[d]; ok && !seen[d] {
				seen[d] = true
				keys = append(keys, d)
			}
		}
	}
	sort.Strings(keys)
	var out []VocabEntry
	for _, k := range keys {
		out = append(out, reg[k]...)
	}
	return out
}

// Vocabulary returns the full domain-vocabulary table across ALL domains
// (enabled AND disabled entries), in deterministic domain-then-file order —
// exported for `pixkb vocab list`'s inspection surface (spec acceptance
// criterion: "Users can inspect... domain expansion when debugging").
func Vocabulary() []VocabEntry {
	return selectVocabulary(vocabularies, nil)
}

// VocabularyFor returns the domain-vocabulary table for the active domain set
// (empty = all domains merged). Pix behavior is identical for nil/empty and
// for exactly ["pix"] while pix is the only real domain.
func VocabularyFor(domains []string) []VocabEntry {
	return selectVocabulary(vocabularies, domains)
}

// activeVocabulary returns only the enabled entries, in file order — what
// ExpandQuery actually matches against.
func activeVocabulary(entries []VocabEntry) []VocabEntry {
	out := make([]VocabEntry, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out
}
