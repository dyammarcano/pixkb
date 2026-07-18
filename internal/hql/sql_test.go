package hql

import (
	"strings"
	"testing"
	"time"
)

func sqlFixedCtx() EvalContext {
	return EvalContext{Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func sqlMustParse(t *testing.T, src string) Query {
	t.Helper()
	q, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return q
}

func TestToSQL_EqID(t *testing.T) {
	q := sqlMustParse(t, `type = LegalArticle`)
	where, args, order, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `type = $1` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "LegalArticle" {
		t.Errorf("args = %v", args)
	}
	if order != "" {
		t.Errorf("order = %q", order)
	}
}

func TestToSQL_TagPrefixEq(t *testing.T) {
	q := sqlMustParse(t, `domain = tax`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `tags @> ARRAY[$1]::text[]` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "domain:tax" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TagPrefixIn(t *testing.T) {
	q := sqlMustParse(t, `lei IN (lc-214-2025, lc-999)`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `(tags @> ARRAY[$1]::text[] OR tags @> ARRAY[$2]::text[])` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 2 || args[0] != "lei:lc-214-2025" || args[1] != "lei:lc-999" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TagPrefixIsEmpty(t *testing.T) {
	q := sqlMustParse(t, `livro IS EMPTY`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `NOT EXISTS (SELECT 1 FROM unnest(tags) t WHERE t LIKE $1)` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "livro:%" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_BareTagIsEmpty(t *testing.T) {
	q := sqlMustParse(t, `tag IS EMPTY`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `(tags IS NULL OR cardinality(tags) = 0)` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 0 {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TextContainsEscaped(t *testing.T) {
	q := sqlMustParse(t, `text ~ "50%_x"`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `body ILIKE $1 ESCAPE '\'` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != `%50\%\_x%` {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TextContainsSubstring(t *testing.T) {
	q := sqlMustParse(t, `title ~ "pix"`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `title ILIKE $1 ESCAPE '\'` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "%pix%" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_IntGe(t *testing.T) {
	q := sqlMustParse(t, `epoch >= 2`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `last_epoch >= $1` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "2" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_DateGt(t *testing.T) {
	q := sqlMustParse(t, `updated > 2026-01-01`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `updated_at > $1` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
	if _, ok := args[0].(time.Time); !ok {
		t.Errorf("args[0] = %T, want time.Time", args[0])
	}
}

func TestToSQL_NotOrAnd(t *testing.T) {
	q := sqlMustParse(t, `NOT (type = X OR type = Y) AND domain = tax`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	want := `((NOT (type = $1 OR type = $2)) AND tags @> ARRAY[$3]::text[])`
	if where != want {
		t.Errorf("where = %q, want %q", where, want)
	}
	if len(args) != 3 || args[0] != "X" || args[1] != "Y" || args[2] != "domain:tax" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_OrderAndLimit(t *testing.T) {
	q := sqlMustParse(t, `type = X ORDER BY id DESC LIMIT 5`)
	_, _, order, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if order != "id DESC" {
		t.Errorf("order = %q", order)
	}
	if q.Limit != 5 {
		t.Errorf("limit = %d", q.Limit)
	}
}

func TestToSQL_IllegalOrderByTagPrefix(t *testing.T) {
	q := sqlMustParse(t, `type = X ORDER BY domain`)
	_, _, _, err := q.ToSQL(sqlFixedCtx())
	if err == nil {
		t.Fatal("expected error for ORDER BY on tagPrefix field")
	}
}

func TestToSQL_IllegalOperator_DateContains(t *testing.T) {
	q := sqlMustParse(t, `updated ~ "x"`)
	_, _, _, err := q.ToSQL(sqlFixedCtx())
	if err == nil {
		t.Fatal("expected error for ~ on date field")
	}
}

func TestToSQL_IllegalOperator_IDGreaterThan(t *testing.T) {
	q := sqlMustParse(t, `type > 3`)
	_, _, _, err := q.ToSQL(sqlFixedCtx())
	if err == nil {
		t.Fatal("expected error for > on kID field")
	}
}

func TestToSQL_UnknownField(t *testing.T) {
	q := sqlMustParse(t, `nope = X`)
	_, _, _, err := q.ToSQL(sqlFixedCtx())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestToSQL_IDContainsAllowed(t *testing.T) {
	q := sqlMustParse(t, `id ~ "abc"`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `id ILIKE $1 ESCAPE '\'` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "%abc%" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TagPrefixNotIn(t *testing.T) {
	q := sqlMustParse(t, `domain NOT IN (pix, tax)`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `NOT (tags @> ARRAY[$1]::text[] OR tags @> ARRAY[$2]::text[])` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 2 || args[0] != "domain:pix" || args[1] != "domain:tax" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TagPrefixIsNotEmpty(t *testing.T) {
	q := sqlMustParse(t, `livro IS NOT EMPTY`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `NOT (NOT EXISTS (SELECT 1 FROM unnest(tags) t WHERE t LIKE $1))` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "livro:%" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TextIn(t *testing.T) {
	q := sqlMustParse(t, `title IN (a, b)`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `title = ANY($1)` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
	list, ok := args[0].([]any)
	if !ok || len(list) != 2 || list[0] != "a" || list[1] != "b" {
		t.Errorf("args[0] = %v", args[0])
	}
}

func TestToSQL_TextEq(t *testing.T) {
	q := sqlMustParse(t, `title = foo`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `title = $1` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "foo" {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_TextIsEmpty(t *testing.T) {
	q := sqlMustParse(t, `description IS EMPTY`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `(description IS NULL OR description = '')` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 0 {
		t.Errorf("args = %v", args)
	}
}

func TestToSQL_IDNotEqual(t *testing.T) {
	q := sqlMustParse(t, `id != x`)
	where, args, _, err := q.ToSQL(sqlFixedCtx())
	if err != nil {
		t.Fatalf("ToSQL error: %v", err)
	}
	if where != `id != $1` {
		t.Errorf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "x" {
		t.Errorf("args = %v", args)
	}
}
