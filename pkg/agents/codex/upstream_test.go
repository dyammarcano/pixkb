package codex

import (
	"strings"
	"testing"
)

func TestCheckDriftDetectsChange(t *testing.T) {
	if len(StatusRefs) == 0 {
		t.Fatal("no refs pinned")
	}
	// All current == pinned => no drift.
	cur := map[string]string{}
	for _, r := range StatusRefs {
		cur[r.Path] = r.BlobSHA
	}
	for _, d := range CheckDrift(cur) {
		if d.Changed {
			t.Errorf("unexpected drift for %s", d.Path)
		}
	}

	// Mutate one => drift on exactly that file; a missing one => absent.
	cur[StatusRefs[0].Path] = "deadbeef"
	delete(cur, StatusRefs[1].Path)
	got := CheckDrift(cur)
	changed := 0
	for _, d := range got {
		if d.Changed {
			changed++
		}
	}
	if changed != 2 {
		t.Errorf("changed = %d, want 2 (mutated + missing)", changed)
	}
}

func TestUpstreamMirrorsEmbedded(t *testing.T) {
	names, err := UpstreamMirrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("want >=2 mirror files, got %v", names)
	}
	b, err := UpstreamMirror("SESSION-SHAPE.md")
	if err != nil {
		t.Fatalf("read mirror: %v", err)
	}
	if !strings.Contains(string(b), "window_minutes") {
		t.Error("SESSION-SHAPE.md missing the parsed field reference")
	}
}
