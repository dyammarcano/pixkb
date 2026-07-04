package query

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed domain_vocabulary.yaml
var vocabularyYAML []byte

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

// vocabFile is domain_vocabulary.yaml's top-level shape.
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

// vocabulary is the full table (enabled and disabled entries) loaded once
// from the embedded domain_vocabulary.yaml at package init.
var vocabulary = mustParseVocabulary(vocabularyYAML)

// mustParseVocabulary panics on a parse failure — the embedded file is
// committed source, so a parse failure here is a build-time bug, not a
// runtime condition to recover from.
func mustParseVocabulary(data []byte) []VocabEntry {
	entries, err := parseVocabulary(data)
	if err != nil {
		panic(err)
	}
	return entries
}

// Vocabulary returns the full domain-vocabulary table (enabled AND disabled
// entries), in file order — exported for `pixkb vocab list`'s inspection
// surface (spec acceptance criterion: "Users can inspect... domain
// expansion when debugging").
func Vocabulary() []VocabEntry {
	return vocabulary
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
