package agy

import "testing"

func TestCleanStripsANSI(t *testing.T) {
	in := "\x1b[2J\x1b[1;1HHello \x1b[31mworld\x1b[0m\r\n"
	if got := clean(in); got != "Hello world" {
		t.Fatalf("clean = %q, want %q", got, "Hello world")
	}
}

func TestWinQuote(t *testing.T) {
	cases := map[string]string{
		"agy":            "agy",
		"hello world":    `"hello world"`,
		`say "hi"`:       `"say \"hi\""`,
		"line\nbreak":    "\"line\nbreak\"",
	}
	for in, want := range cases {
		if got := winQuote(in); got != want {
			t.Fatalf("winQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.Bin != "agy" {
		t.Fatalf("Bin = %q, want agy", c.Bin)
	}
	if c.Timeout <= 0 {
		t.Fatalf("Timeout not defaulted")
	}
}
