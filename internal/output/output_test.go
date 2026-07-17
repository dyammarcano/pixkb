package output

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"pixkb/internal/ispb"
	"pixkb/internal/store/postgres"
)

func fixtureHits() []postgres.Hit {
	return []postgres.Hit{
		{ID: "concept-001", Title: "First Concept", Type: "term", Score: 0.9123, Rank: 1},
		{ID: "concept-002", Title: "Second Concept", Type: "rule", Score: 0.5, Rank: 2},
	}
}

// wantHitsText pins renderText's exact output for fixtureHits() as a literal, so
// a change to renderText's layout fails the test. Deriving `want` from the same
// fmt verbs renderText uses would be tautological — a reviewer could "fix" the
// test by copying the new verbs and never notice the output changed.
const wantHitsText = " 1  concept-001                         First Concept\n" +
	" 2  concept-002                         Second Concept\n"

func TestRender(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
		check   func(t *testing.T, got string)
	}{
		{
			name:   "text",
			format: "text",
			check: func(t *testing.T, got string) {
				if got != wantHitsText {
					t.Errorf("text output mismatch\ngot:  %q\nwant: %q", got, wantHitsText)
				}
			},
		},
		{
			name:   "default format (empty string) matches text",
			format: "",
			check: func(t *testing.T, got string) {
				if got != wantHitsText {
					t.Errorf("default output mismatch\ngot:  %q\nwant: %q", got, wantHitsText)
				}
			},
		},
		{
			name:   "json",
			format: "json",
			check: func(t *testing.T, got string) {
				var roundTrip []postgres.Hit
				if err := json.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureHits()) {
					t.Errorf("json round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureHits())
				}
			},
		},
		{
			name:   "md",
			format: "md",
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "| rank | id | title | type | score |") {
					t.Errorf("md output missing header row:\n%s", got)
				}
				if !strings.Contains(got, "concept-001") {
					t.Errorf("md output missing first hit id:\n%s", got)
				}
				if !strings.Contains(got, "concept-002") {
					t.Errorf("md output missing second hit id:\n%s", got)
				}
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			check: func(t *testing.T, got string) {
				var roundTrip []postgres.Hit
				if err := yaml.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("yaml.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureHits()) {
					t.Errorf("yaml round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureHits())
				}
			},
		},
		{
			name:    "unknown format",
			format:  "xml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.format, fixtureHits())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Render(%q, ...) = nil error, want error", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("Render(%q, ...) unexpected error: %v", tt.format, err)
			}
			tt.check(t, got)
		})
	}
}

func fixtureRelated() []postgres.RelatedConcept {
	return []postgres.RelatedConcept{
		{ID: "concept-001", Title: "First Concept", Type: "term", Direction: "in"},
		{ID: "concept-002", Title: "Second Concept", Type: "rule", Direction: "out"},
	}
}

// wantRelatedText is the byte-for-byte output `pixkb related` produced before
// it gained --format, pinned as a literal (not re-derived from the same format
// string) so a change to renderRelatedText's layout fails the test.
const wantRelatedText = "in  concept-001                         First Concept\n" +
	"out concept-002                         Second Concept\n"

func TestRenderRelated(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
		check   func(t *testing.T, got string)
	}{
		{
			name:   "text",
			format: "text",
			check: func(t *testing.T, got string) {
				if got != wantRelatedText {
					t.Errorf("text output mismatch\ngot:  %q\nwant: %q", got, wantRelatedText)
				}
			},
		},
		{
			name:   "default format (empty string) matches text",
			format: "",
			check: func(t *testing.T, got string) {
				if got != wantRelatedText {
					t.Errorf("default output mismatch\ngot:  %q\nwant: %q", got, wantRelatedText)
				}
			},
		},
		{
			name:   "json",
			format: "json",
			check: func(t *testing.T, got string) {
				var roundTrip []postgres.RelatedConcept
				if err := json.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureRelated()) {
					t.Errorf("json round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureRelated())
				}
			},
		},
		{
			name:   "md",
			format: "md",
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "| direction | id | title | type |") {
					t.Errorf("md output missing header row:\n%s", got)
				}
				for _, want := range []string{"concept-001", "concept-002"} {
					if !strings.Contains(got, want) {
						t.Errorf("md output missing %q:\n%s", want, got)
					}
				}
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			check: func(t *testing.T, got string) {
				var roundTrip []postgres.RelatedConcept
				if err := yaml.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("yaml.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureRelated()) {
					t.Errorf("yaml round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureRelated())
				}
			},
		},
		{
			name:    "unknown format",
			format:  "xml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderRelated(tt.format, fixtureRelated())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("RenderRelated(%q, ...) = nil error, want error", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderRelated(%q, ...) unexpected error: %v", tt.format, err)
			}
			tt.check(t, got)
		})
	}
}

func fixtureStats() postgres.Stats {
	return postgres.Stats{
		Concepts:    12,
		Embeddings:  10,
		Epochs:      3,
		LatestEpoch: 2,
		ByType:      map[string]int{"term": 7, "rule": 5},
		TypeOrder:   []string{"term", "rule"},
	}
}

// wantStatsText pins `pixkb stats`'s pre---format output byte-for-byte.
const wantStatsText = "concepts:    12\n" +
	"embeddings:  10\n" +
	"epochs:      3 (latest: 2)\n" +
	"by type:\n" +
	"  term             7\n" +
	"  rule             5\n"

func TestRenderStats(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		stats   postgres.Stats
		wantErr bool
		check   func(t *testing.T, got string)
	}{
		{
			name:   "text",
			format: "text",
			stats:  fixtureStats(),
			check: func(t *testing.T, got string) {
				if got != wantStatsText {
					t.Errorf("text output mismatch\ngot:  %q\nwant: %q", got, wantStatsText)
				}
			},
		},
		{
			name:   "default format (empty string) matches text",
			format: "",
			stats:  fixtureStats(),
			check: func(t *testing.T, got string) {
				if got != wantStatsText {
					t.Errorf("default output mismatch\ngot:  %q\nwant: %q", got, wantStatsText)
				}
			},
		},
		{
			// The historical command omitted the whole "by type:" block when
			// TypeOrder was empty; that conditional must survive the port.
			name:   "text omits by-type block when TypeOrder is empty",
			format: "text",
			stats:  postgres.Stats{Concepts: 0, Embeddings: 0, Epochs: 0, LatestEpoch: -1},
			check: func(t *testing.T, got string) {
				want := "concepts:    0\nembeddings:  0\nepochs:      0 (latest: -1)\n"
				if got != want {
					t.Errorf("empty-stats output mismatch\ngot:  %q\nwant: %q", got, want)
				}
			},
		},
		{
			name:   "json",
			format: "json",
			stats:  fixtureStats(),
			check: func(t *testing.T, got string) {
				var roundTrip postgres.Stats
				if err := json.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureStats()) {
					t.Errorf("json round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureStats())
				}
			},
		},
		{
			name:   "md",
			format: "md",
			stats:  fixtureStats(),
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "| metric | value |") {
					t.Errorf("md output missing header row:\n%s", got)
				}
				for _, want := range []string{"concepts", "embeddings", "epochs", "term", "rule"} {
					if !strings.Contains(got, want) {
						t.Errorf("md output missing %q:\n%s", want, got)
					}
				}
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			stats:  fixtureStats(),
			check: func(t *testing.T, got string) {
				var roundTrip postgres.Stats
				if err := yaml.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("yaml.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureStats()) {
					t.Errorf("yaml round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureStats())
				}
			},
		},
		{
			name:    "unknown format",
			format:  "xml",
			stats:   fixtureStats(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderStats(tt.format, tt.stats)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("RenderStats(%q, ...) = nil error, want error", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderStats(%q, ...) unexpected error: %v", tt.format, err)
			}
			tt.check(t, got)
		})
	}
}

// fixtureParticipant is a fully-populated participant: both the STR and Pix
// sync timestamps are set, so every conditional block of the text renderer
// fires.
func fixtureParticipant() ispb.Participant {
	ts := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	return ispb.Participant{
		ISPB:              "00000208",
		Name:              "BRB - BANCO DE BRASILIA S.A.",
		LegalName:         "BRB - BANCO DE BRASILIA SA",
		CompeCode:         "070",
		ParticipatesCompe: true,
		AccessType:        "Liquidante",
		STRSyncedAt:       ts,
		CNPJ:              "00000208000100",
		PixAuthorized:     true,
		PixSyncedAt:       ts,
	}
}

// wantISPBText pins `pixkb ispb lookup`'s pre---format output byte-for-byte.
const wantISPBText = "ISPB:          00000208\n" +
	"Name:          BRB - BANCO DE BRASILIA S.A.\n" +
	"Legal name:    BRB - BANCO DE BRASILIA SA\n" +
	"COMPE code:    070\n" +
	"Participates COMPE: true\n" +
	"Access type:   Liquidante\n" +
	"STR synced:    2026-06-01T12:00:00Z\n" +
	"CNPJ:          00000208000100\n" +
	"Pix authorized: true\n" +
	"Pix synced:    2026-06-01T12:00:00Z\n"

func TestRenderISPB(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		p       ispb.Participant
		wantErr bool
		check   func(t *testing.T, got string)
	}{
		{
			name:   "text",
			format: "text",
			p:      fixtureParticipant(),
			check: func(t *testing.T, got string) {
				if got != wantISPBText {
					t.Errorf("text output mismatch\ngot:  %q\nwant: %q", got, wantISPBText)
				}
			},
		},
		{
			name:   "default format (empty string) matches text",
			format: "",
			p:      fixtureParticipant(),
			check: func(t *testing.T, got string) {
				if got != wantISPBText {
					t.Errorf("default output mismatch\ngot:  %q\nwant: %q", got, wantISPBText)
				}
			},
		},
		{
			// A participant known only from the ISPB registry (no STR/Pix
			// sync, no optional names) printed just the two mandatory lines.
			name:   "text omits every unset optional field",
			format: "text",
			p:      ispb.Participant{ISPB: "00000000", Name: "BANCO DO BRASIL"},
			check: func(t *testing.T, got string) {
				want := "ISPB:          00000000\nName:          BANCO DO BRASIL\n"
				if got != want {
					t.Errorf("sparse-participant output mismatch\ngot:  %q\nwant: %q", got, want)
				}
			},
		},
		{
			name:   "json",
			format: "json",
			p:      fixtureParticipant(),
			check: func(t *testing.T, got string) {
				var roundTrip ispb.Participant
				if err := json.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureParticipant()) {
					t.Errorf("json round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureParticipant())
				}
			},
		},
		{
			name:   "md",
			format: "md",
			p:      fixtureParticipant(),
			check: func(t *testing.T, got string) {
				if !strings.Contains(got, "| field | value |") {
					t.Errorf("md output missing header row:\n%s", got)
				}
				for _, want := range []string{"00000208", "BRB - BANCO DE BRASILIA S.A.", "070"} {
					if !strings.Contains(got, want) {
						t.Errorf("md output missing %q:\n%s", want, got)
					}
				}
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			p:      fixtureParticipant(),
			check: func(t *testing.T, got string) {
				var roundTrip ispb.Participant
				if err := yaml.Unmarshal([]byte(got), &roundTrip); err != nil {
					t.Fatalf("yaml.Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(roundTrip, fixtureParticipant()) {
					t.Errorf("yaml round-trip mismatch\ngot:  %+v\nwant: %+v", roundTrip, fixtureParticipant())
				}
			},
		},
		{
			name:    "unknown format",
			format:  "xml",
			p:       fixtureParticipant(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderISPB(tt.format, tt.p)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("RenderISPB(%q, ...) = nil error, want error", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderISPB(%q, ...) unexpected error: %v", tt.format, err)
			}
			tt.check(t, got)
		})
	}
}
