package hql

import (
	"errors"
	"testing"
	"time"
)

// fixedNow anchors all clock-derived function tests to a deterministic instant.
var fixedNow = time.Date(2026, 1, 1, 12, 30, 45, 0, time.UTC)

func fixedCtx() EvalContext {
	return EvalContext{Now: fixedNow}
}

func TestResolveFuncNow(t *testing.T) {
	got, err := resolveFunc(Value{Kind: ValFunc, Raw: "now"}, fixedCtx())
	if err != nil {
		t.Fatalf("now(): %v", err)
	}
	tm, ok := got.(time.Time)
	if !ok || !tm.Equal(fixedNow) {
		t.Errorf("now() = %v, want %v", got, fixedNow)
	}
}

func TestResolveFuncTodayAndStartOfDay(t *testing.T) {
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"today", "startOfDay", "STARTOFDAY"} {
		got, err := resolveFunc(Value{Kind: ValFunc, Raw: name}, fixedCtx())
		if err != nil {
			t.Fatalf("%s(): %v", name, err)
		}
		tm, ok := got.(time.Time)
		if !ok || !tm.Equal(want) {
			t.Errorf("%s() = %v, want %v", name, got, want)
		}
	}
}

func TestResolveFuncEndOfDay(t *testing.T) {
	want := time.Date(2026, 1, 1, 23, 59, 59, 0, time.UTC)
	got, err := resolveFunc(Value{Kind: ValFunc, Raw: "endOfDay"}, fixedCtx())
	if err != nil {
		t.Fatalf("endOfDay(): %v", err)
	}
	tm, ok := got.(time.Time)
	if !ok || !tm.Equal(want) {
		t.Errorf("endOfDay() = %v, want %v", got, want)
	}
}

func TestResolveFuncUnknownIsErrUnsupported(t *testing.T) {
	_, err := resolveFunc(Value{Kind: ValFunc, Raw: "bogusfn"}, fixedCtx())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("bogusfn() error = %v, want ErrUnsupported", err)
	}
}

func TestFuncArityMismatchIsError(t *testing.T) {
	cases := []Value{
		{Kind: ValFunc, Raw: "now", Args: []Value{{Kind: ValWord, Raw: "x"}}},
		{Kind: ValFunc, Raw: "today", Args: []Value{{Kind: ValWord, Raw: "1"}}},
		{Kind: ValFunc, Raw: "startOfDay", Args: []Value{{Kind: ValWord, Raw: "1"}}},
		{Kind: ValFunc, Raw: "endOfDay", Args: []Value{{Kind: ValWord, Raw: "1"}}},
	}
	for _, v := range cases {
		if _, err := resolveFunc(v, fixedCtx()); err == nil {
			t.Errorf("%s(...with args) = nil error, want arity error", v.Raw)
		}
	}
	// Zero-arg forms still resolve.
	for name := range funcArity {
		if _, err := resolveFunc(Value{Kind: ValFunc, Raw: name}, fixedCtx()); err != nil {
			t.Errorf("%s() with no args should resolve: %v", name, err)
		}
	}
}

func TestResolveDateRelative(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
	}{
		{"-7d", fixedNow.Add(-7 * 24 * time.Hour)},
		{"+1w", fixedNow.Add(7 * 24 * time.Hour)},
		{"24h", fixedNow.Add(24 * time.Hour)},
		{"30m", fixedNow.Add(30 * time.Minute)},
		{"10s", fixedNow.Add(10 * time.Second)},
	}
	for _, c := range cases {
		got, err := resolveDate(c.raw, fixedCtx())
		if err != nil {
			t.Fatalf("resolveDate(%q): %v", c.raw, err)
		}
		if !got.Equal(c.want) {
			t.Errorf("resolveDate(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestResolveDateAbsolute(t *testing.T) {
	got, err := resolveDate("2026-06-22T10:00:00Z", fixedCtx())
	if err != nil {
		t.Fatalf("resolveDate RFC3339: %v", err)
	}
	want := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("resolveDate RFC3339 = %v, want %v", got, want)
	}

	got, err = resolveDate("2026-06-22", fixedCtx())
	if err != nil {
		t.Fatalf("resolveDate date-only: %v", err)
	}
	want = time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("resolveDate date-only = %v, want %v", got, want)
	}
}

func TestResolveDateInvalid(t *testing.T) {
	if _, err := resolveDate("not-a-date", fixedCtx()); err == nil {
		t.Fatal("resolveDate(invalid) = nil error, want error")
	}
}

func TestResolveScalarWord(t *testing.T) {
	got, err := resolveScalar(Value{Kind: ValWord, Raw: "hello"}, fixedCtx())
	if err != nil {
		t.Fatalf("resolveScalar: %v", err)
	}
	if got != "hello" {
		t.Errorf("resolveScalar(word) = %v, want %q", got, "hello")
	}
}

func TestResolveScalarFunc(t *testing.T) {
	got, err := resolveScalar(Value{Kind: ValFunc, Raw: "now"}, fixedCtx())
	if err != nil {
		t.Fatalf("resolveScalar(func): %v", err)
	}
	tm, ok := got.(time.Time)
	if !ok || !tm.Equal(fixedNow) {
		t.Errorf("resolveScalar(now()) = %v, want %v", got, fixedNow)
	}
}

func TestEscapeLike(t *testing.T) {
	cases := []struct{ in, want string }{
		{`50%_x`, `50\%\_x`},
		{`a\b`, `a\\b`},
		{`plain`, `plain`},
		{`%%__\\`, `\%\%\_\_\\\\`},
	}
	for _, c := range cases {
		if got := escapeLike(c.in); got != c.want {
			t.Errorf("escapeLike(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
