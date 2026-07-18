package kbmcp

import "testing"

func TestClampInt(t *testing.T) {
	cases := []struct{ v, max, want int }{
		{0, 50, 0},    // unset stays 0 (rag applies its default)
		{10, 50, 10},  // under cap unchanged
		{50, 50, 50},  // at cap unchanged
		{999, 50, 50}, // over cap clamped
		{-1, 50, -1},  // negative left as-is (rag treats <=0 as default)
	}
	for _, c := range cases {
		if got := clampInt(c.v, c.max); got != c.want {
			t.Errorf("clampInt(%d,%d)=%d want %d", c.v, c.max, got, c.want)
		}
	}
}
