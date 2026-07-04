package output

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"pixkb/internal/store/postgres"
)

func fixtureHits() []postgres.Hit {
	return []postgres.Hit{
		{ID: "concept-001", Title: "First Concept", Type: "term", Score: 0.9123, Rank: 1},
		{ID: "concept-002", Title: "Second Concept", Type: "rule", Score: 0.5, Rank: 2},
	}
}

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
				hits := fixtureHits()
				want := fmt.Sprintf("%2d  %-34s  %s\n", hits[0].Rank, hits[0].ID, hits[0].Title) +
					fmt.Sprintf("%2d  %-34s  %s\n", hits[1].Rank, hits[1].ID, hits[1].Title)
				if got != want {
					t.Errorf("text output mismatch\ngot:  %q\nwant: %q", got, want)
				}
			},
		},
		{
			name:   "default format (empty string) matches text",
			format: "",
			check: func(t *testing.T, got string) {
				hits := fixtureHits()
				want := fmt.Sprintf("%2d  %-34s  %s\n", hits[0].Rank, hits[0].ID, hits[0].Title) +
					fmt.Sprintf("%2d  %-34s  %s\n", hits[1].Rank, hits[1].ID, hits[1].Title)
				if got != want {
					t.Errorf("default output mismatch\ngot:  %q\nwant: %q", got, want)
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
