package agy

import "testing"

func TestSyncBuffer(t *testing.T) {
	b := newSyncBuffer()
	if b.Len() != 0 {
		t.Fatal("new buffer not empty")
	}
	_, _ = b.Write([]byte("hello "))
	off := b.Len()
	_, _ = b.Write([]byte("world"))
	if b.Len() != 11 {
		t.Fatalf("len = %d, want 11", b.Len())
	}
	if got := b.From(off); got != "world" {
		t.Fatalf("From(%d) = %q, want world", off, got)
	}
	if got := b.From(100); got != "" {
		t.Fatalf("From past end = %q, want empty", got)
	}
	b.Reset()
	if b.Len() != 0 {
		t.Fatal("Reset did not clear")
	}
}

func TestStripEcho(t *testing.T) {
	// The TUI echoes the typed prompt before the answer; stripEcho drops it.
	out := "criar cobrança imediata\nThe POST /cob endpoint creates an immediate charge."
	got := stripEcho(out, "criar cobrança imediata")
	if want := "\nThe POST /cob endpoint creates an immediate charge."; got != want {
		t.Fatalf("stripEcho = %q, want %q", got, want)
	}
	// No echo present: output returned unchanged.
	if got := stripEcho("just an answer", "some prompt"); got != "just an answer" {
		t.Fatalf("stripEcho unchanged = %q", got)
	}
}
