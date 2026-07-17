package hql

import "testing"

func TestParseBasicComparison(t *testing.T) {
	q, err := Parse(`type = "LegalArticle" AND domain ~ "tax"`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	and, ok := q.Where.(*And)
	if !ok {
		t.Fatalf("Where = %T, want *And", q.Where)
	}
	l, ok := and.L.(*Comparison)
	if !ok || l.Field != "type" || l.Op != OpEq || l.Value == nil || l.Value.Raw != "LegalArticle" || l.Value.Kind != ValString {
		t.Fatalf("left = %#v", and.L)
	}
	r, ok := and.R.(*Comparison)
	if !ok || r.Field != "domain" || r.Op != OpContains || r.Value == nil || r.Value.Raw != "tax" {
		t.Fatalf("right = %#v", and.R)
	}
}

func TestParseOrderByAndLimit(t *testing.T) {
	q, err := Parse(`id = "abc" ORDER BY id DESC LIMIT 5`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(q.OrderBy) != 1 || q.OrderBy[0].Field != "id" || !q.OrderBy[0].Desc {
		t.Fatalf("OrderBy = %#v", q.OrderBy)
	}
	if q.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", q.Limit)
	}
}

func TestParseInAndFunc(t *testing.T) {
	q, err := Parse(`tag IN ("a", "b") OR type = now()`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	or, ok := q.Where.(*Or)
	if !ok {
		t.Fatalf("Where = %T, want *Or", q.Where)
	}
	l, ok := or.L.(*Comparison)
	if !ok || l.Field != "tag" || l.Op != OpIn || len(l.List) != 2 || l.List[0].Raw != "a" || l.List[1].Raw != "b" {
		t.Fatalf("left = %#v", or.L)
	}
	r, ok := or.R.(*Comparison)
	if !ok || r.Field != "type" || r.Op != OpEq || r.Value == nil || r.Value.Kind != ValFunc || r.Value.Raw != "now" {
		t.Fatalf("right = %#v", or.R)
	}
}

func TestParseNotAndParens(t *testing.T) {
	q, err := Parse(`NOT (type = "X" OR type = "Y") AND domain = "tax"`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	and, ok := q.Where.(*And)
	if !ok {
		t.Fatalf("Where = %T, want *And", q.Where)
	}
	not, ok := and.L.(*Not)
	if !ok {
		t.Fatalf("left = %T, want *Not", and.L)
	}
	if _, ok := not.X.(*Or); !ok {
		t.Fatalf("not.X = %T, want *Or", not.X)
	}
	if r, ok := and.R.(*Comparison); !ok || r.Field != "domain" {
		t.Fatalf("right = %#v", and.R)
	}
}

func TestParseIsEmpty(t *testing.T) {
	q, err := Parse(`tag IS EMPTY`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, ok := q.Where.(*Comparison)
	if !ok || c.Field != "tag" || c.Op != OpEmpty {
		t.Fatalf("Where = %#v", q.Where)
	}

	q2, err := Parse(`tag IS NOT EMPTY`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c2, ok := q2.Where.(*Comparison)
	if !ok || c2.Field != "tag" || c2.Op != OpNotEmpty {
		t.Fatalf("Where = %#v", q2.Where)
	}
}

func TestParseRelativeDuration(t *testing.T) {
	q, err := Parse(`updated >= -7d`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, ok := q.Where.(*Comparison)
	if !ok || c.Field != "updated" || c.Op != OpGe || c.Value == nil || c.Value.Raw != "-7d" || c.Value.Kind != ValWord {
		t.Fatalf("Where = %#v", q.Where)
	}
}

func TestParseLimit(t *testing.T) {
	cases := []struct {
		q    string
		want int
	}{
		{`type = "x"`, 0},
		{`type = "x" LIMIT 10`, 10},
		{`type = "x" ORDER BY id DESC LIMIT 5`, 5},
		{`domain ~ "x" LIMIT 0`, 0},
	}
	for _, c := range cases {
		q, err := Parse(c.q)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.q, err)
		}
		if q.Limit != c.want {
			t.Errorf("Parse(%q).Limit = %d, want %d", c.q, q.Limit, c.want)
		}
	}
}

func TestParseLimitErrors(t *testing.T) {
	for _, q := range []string{
		`type = "x" LIMIT`,
		`type = "x" LIMIT abc`,
		`type = "x" LIMIT -3`,
		`type = "x" LIMIT 5 bogus`,
	} {
		if _, err := Parse(q); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", q)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, q := range []string{
		``,
		`type`,
		`type =`,
		`(type = "x"`,
		`type = "x" bogus`,
		`type IN ()`,
	} {
		if _, err := Parse(q); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", q)
		}
	}
}

func TestFunctionArityParses(t *testing.T) {
	// The parser accepts any argument count for a function call; arity
	// validation happens later (functions.go, Task 2). Here we only assert
	// these all parse successfully and capture the right arg count.
	cases := []struct {
		q        string
		wantArgs int
	}{
		{`type = now("x")`, 1},
		{`updated >= now()`, 0},
		{`updated >= today(1)`, 1},
	}
	for _, c := range cases {
		q, err := Parse(c.q)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.q, err)
		}
		cmp, ok := q.Where.(*Comparison)
		if !ok || cmp.Value == nil || cmp.Value.Kind != ValFunc {
			t.Fatalf("Where = %#v", q.Where)
		}
		if len(cmp.Value.Args) != c.wantArgs {
			t.Errorf("Parse(%q) args = %d, want %d", c.q, len(cmp.Value.Args), c.wantArgs)
		}
	}
}

func mustParse(t *testing.T, s string) Query {
	t.Helper()
	q, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return q
}

func TestParseUnknownFunctionOK(t *testing.T) {
	// The parser doesn't know about function semantics; any bareword followed
	// by "(...)" parses as a ValFunc. Resolution/validation is Task 2.
	q := mustParse(t, `type = bogusfn()`)
	c, ok := q.Where.(*Comparison)
	if !ok || c.Value == nil || c.Value.Kind != ValFunc || c.Value.Raw != "bogusfn" {
		t.Fatalf("Where = %#v", q.Where)
	}
}

// FuzzParse asserts the hand-rolled lexer/parser never panics on arbitrary
// input. Errors are an acceptable outcome; panics are not.
func FuzzParse(f *testing.F) {
	seeds := []string{
		``, `type = "x"`, `domain ~ "tax" AND updated >= -7d`,
		`tag IN ("a","b") OR NOT id = "1"`,
		`(type = now()) ORDER BY updated DESC LIMIT 5`,
		`type = "group"`, `type = now("x")`, `x`, `= =`, `((((`, `"unterminated`,
		`IS NOT EMPTY`, `subject IS EMPTY`, `LIMIT`, `updated > 2026-01-02`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		_, _ = Parse(in) // parse errors are acceptable; panics are not
	})
}
