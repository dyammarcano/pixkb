package hql

import (
	"fmt"
	"strings"
)

// ToSQL renders the query as a parameterized Postgres fragment over the
// concept table: a WHERE expression, its ordered $N argument list, and an
// ORDER BY clause. Neither the "WHERE" nor "ORDER BY" keywords are included —
// the caller (Store.QueryConcepts) adds them. Every value is a placeholder;
// no user text is ever interpolated into SQL.
func (q Query) ToSQL(ctx EvalContext) (where string, args []any, order string, err error) {
	return q.ToSQLAt(ctx, 0)
}

// ToSQLAt renders the query the same way ToSQL does, but numbers its $N
// placeholders starting from startArg+1 instead of 1. This lets a caller
// splice the returned WHERE fragment and args into an already-numbered
// argument list (e.g. appending an HQL predicate after other parameterized
// predicates) without any string renumbering. ToSQLAt(ctx, 0) is byte-
// identical to ToSQL(ctx).
func (q Query) ToSQLAt(ctx EvalContext, startArg int) (where string, args []any, order string, err error) {
	b := &sqlBuilder{ctx: ctx, base: startArg}
	where, err = b.expr(q.Where)
	if err != nil {
		return "", nil, "", err
	}
	order, err = b.order(q.OrderBy)
	if err != nil {
		return "", nil, "", err
	}
	return where, b.args, order, nil
}

type sqlBuilder struct {
	ctx  EvalContext
	base int
	args []any
}

// ph appends v to args and returns its positional placeholder ($1, $2, ...
// offset by base).
func (b *sqlBuilder) ph(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", b.base+len(b.args))
}

func (b *sqlBuilder) expr(e Expr) (string, error) {
	switch n := e.(type) {
	case *And:
		l, err := b.expr(n.L)
		if err != nil {
			return "", err
		}
		r, err := b.expr(n.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " AND " + r + ")", nil
	case *Or:
		l, err := b.expr(n.L)
		if err != nil {
			return "", err
		}
		r, err := b.expr(n.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " OR " + r + ")", nil
	case *Not:
		x, err := b.expr(n.X)
		if err != nil {
			return "", err
		}
		return "(NOT " + x + ")", nil
	case *Comparison:
		return b.cmp(n)
	default:
		return "", fmt.Errorf("hql: unknown expression node %T", e)
	}
}

func kindName(k fieldKind) string {
	switch k {
	case kText:
		return "text"
	case kID:
		return "id"
	case kInt:
		return "int"
	case kDate:
		return "date"
	case kTagPrefix:
		return "tagPrefix"
	default:
		return "unknown"
	}
}

// allowedOps lists the operators legal for f's kind on fieldName (id itself
// additionally allows ~ / !~ on top of the base kID set).
func allowedOps(f field, fieldName string) []string {
	switch f.kind {
	case kText:
		return []string{OpContains, OpNotContains, OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty}
	case kID:
		ops := []string{OpEq, OpNe, OpIn, OpNotIn}
		if strings.EqualFold(fieldName, "id") {
			ops = append(ops, OpContains, OpNotContains)
		}
		return ops
	case kInt, kDate:
		return []string{OpEq, OpNe, OpGt, OpGe, OpLt, OpLe}
	case kTagPrefix:
		return []string{OpEq, OpNe, OpIn, OpNotIn, OpEmpty, OpNotEmpty}
	default:
		return nil
	}
}

func opAllowed(f field, fieldName, op string) bool {
	for _, o := range allowedOps(f, fieldName) {
		if o == op {
			return true
		}
	}
	return false
}

func (b *sqlBuilder) cmp(c *Comparison) (string, error) {
	f, ok := lookupField(c.Field)
	if !ok {
		return "", fmt.Errorf("hql: unknown field %q", c.Field)
	}
	if !opAllowed(f, c.Field, c.Op) {
		return "", fmt.Errorf("hql: field %q (kind %s) does not support operator %q; allowed: %s",
			c.Field, kindName(f.kind), c.Op, strings.Join(allowedOps(f, c.Field), " "))
	}
	switch f.kind {
	case kText, kID:
		return b.textIDCmp(c, f)
	case kInt, kDate:
		return b.scalarCmp(c, f)
	case kTagPrefix:
		return b.tagPrefixCmp(c, f)
	default:
		return "", fmt.Errorf("hql: field %q has unknown kind", c.Field)
	}
}

func (b *sqlBuilder) textIDCmp(c *Comparison, f field) (string, error) {
	col := f.column
	switch c.Op {
	case OpContains, OpNotContains:
		if c.Value == nil {
			return "", fmt.Errorf("hql: field %q needs a value", c.Field)
		}
		pat := "%" + escapeLike(c.Value.Raw) + "%"
		neg := ""
		if c.Op == OpNotContains {
			neg = "NOT "
		}
		return col + " " + neg + "ILIKE " + b.ph(pat) + ` ESCAPE '\'`, nil
	case OpEq, OpNe:
		if c.Value == nil {
			return "", fmt.Errorf("hql: field %q needs a value", c.Field)
		}
		v, err := resolveScalar(*c.Value, b.ctx)
		if err != nil {
			return "", err
		}
		op := "="
		if c.Op == OpNe {
			op = "!="
		}
		return col + " " + op + " " + b.ph(v), nil
	case OpIn, OpNotIn:
		vals := make([]any, 0, len(c.List))
		for _, item := range c.List {
			v, err := resolveScalar(item, b.ctx)
			if err != nil {
				return "", err
			}
			vals = append(vals, v)
		}
		if c.Op == OpIn {
			return col + " = ANY(" + b.ph(vals) + ")", nil
		}
		return col + " != ALL(" + b.ph(vals) + ")", nil
	case OpEmpty, OpNotEmpty:
		base := "(" + col + " IS NULL OR " + col + " = '')"
		if c.Op == OpNotEmpty {
			return "NOT " + base, nil
		}
		return base, nil
	default:
		return "", fmt.Errorf("hql: unknown operator %q", c.Op)
	}
}

func (b *sqlBuilder) scalarCmp(c *Comparison, f field) (string, error) {
	if c.Value == nil {
		return "", fmt.Errorf("hql: field %q needs a value", c.Field)
	}
	col := f.column
	var v any
	var err error
	if f.kind == kDate {
		v, err = resolveDate(c.Value.Raw, b.ctx)
	} else {
		v, err = resolveScalar(*c.Value, b.ctx)
	}
	if err != nil {
		return "", err
	}
	return col + " " + c.Op + " " + b.ph(v), nil
}

// tagValue resolves a comparison value to the string appended after the
// field's prefix; tag values are never functions/dates so a non-string
// resolution is an error.
func tagValue(v Value, prefix string, ctx EvalContext) (string, error) {
	resolved, err := resolveScalar(v, ctx)
	if err != nil {
		return "", err
	}
	s, ok := resolved.(string)
	if !ok {
		return "", fmt.Errorf("hql: tag value must be a string, got %T", resolved)
	}
	return prefix + s, nil
}

func (b *sqlBuilder) tagPrefixCmp(c *Comparison, f field) (string, error) {
	switch c.Op {
	case OpEq, OpNe:
		if c.Value == nil {
			return "", fmt.Errorf("hql: field %q needs a value", c.Field)
		}
		v, err := tagValue(*c.Value, f.prefix, b.ctx)
		if err != nil {
			return "", err
		}
		base := "tags @> ARRAY[" + b.ph(v) + "]::text[]"
		if c.Op == OpNe {
			return "NOT (" + base + ")", nil
		}
		return base, nil
	case OpIn, OpNotIn:
		parts := make([]string, 0, len(c.List))
		for _, item := range c.List {
			v, err := tagValue(item, f.prefix, b.ctx)
			if err != nil {
				return "", err
			}
			parts = append(parts, "tags @> ARRAY["+b.ph(v)+"]::text[]")
		}
		joined := "(" + strings.Join(parts, " OR ") + ")"
		if c.Op == OpNotIn {
			return "NOT " + joined, nil
		}
		return joined, nil
	case OpEmpty, OpNotEmpty:
		return b.tagPrefixEmpty(f, c.Op == OpNotEmpty)
	default:
		return "", fmt.Errorf("hql: unknown operator %q", c.Op)
	}
}

func (b *sqlBuilder) tagPrefixEmpty(f field, negate bool) (string, error) {
	var base string
	if f.prefix == "" {
		base = "(tags IS NULL OR cardinality(tags) = 0)"
	} else {
		base = "NOT EXISTS (SELECT 1 FROM unnest(tags) t WHERE t LIKE " + b.ph(f.prefix+"%") + ")"
	}
	if negate {
		return "NOT (" + base + ")", nil
	}
	return base, nil
}

func (b *sqlBuilder) order(fs []OrderField) (string, error) {
	if len(fs) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(fs))
	for _, of := range fs {
		f, ok := lookupField(of.Field)
		if !ok {
			return "", fmt.Errorf("hql: unknown ORDER BY field %q", of.Field)
		}
		if f.kind == kTagPrefix {
			return "", fmt.Errorf("hql: ORDER BY %q (tag field) is not supported", of.Field)
		}
		dir := "ASC"
		if of.Desc {
			dir = "DESC"
		}
		parts = append(parts, f.column+" "+dir)
	}
	return strings.Join(parts, ", "), nil
}
