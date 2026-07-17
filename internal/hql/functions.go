package hql

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnsupported marks a grammar construct that parses but the scaffold cannot
// yet lower to SQL or evaluate (joins, FTS `match`, some functions). v1 removes
// these.
var ErrUnsupported = errors.New("hql: unsupported (not yet implemented)")

// EvalContext supplies the values HQL functions resolve against. Now is an
// injectable clock so now()/today()/relative durations are deterministic in
// tests. pixkb has no signed-in user, so unlike herald there is no Self field
// and no me()/currentUser() function.
type EvalContext struct {
	Now time.Time
}

// now returns the context clock, or the zero-arg system time as a fallback when
// unset (callers in tests always set it).
func (c EvalContext) now() time.Time {
	if c.Now.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return c.Now
}

var relDurRe = regexp.MustCompile(`^([+-]?\d+)([smhdw])$`)

// parseRelative parses a relative-duration literal (e.g. "-7d", "24h", "+1w")
// into a time.Duration. d and w are expanded to hours. ok is false if s is not
// a relative duration.
func parseRelative(s string) (time.Duration, bool) {
	m := relDurRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	switch m[2] {
	case "s":
		return time.Duration(n) * time.Second, true
	case "m":
		return time.Duration(n) * time.Minute, true
	case "h":
		return time.Duration(n) * time.Hour, true
	case "d":
		return time.Duration(n) * 24 * time.Hour, true
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, true
	}
	return 0, false
}

// resolveScalar coerces a value literal, using ctx for functions. pixkb's
// schema (Task 3) has no boolean field kind, so unlike herald this does not
// switch on a fieldKind: date fields call resolveDate directly, and this
// returns the raw string for everything else.
func resolveScalar(v Value, ctx EvalContext) (any, error) {
	if v.Kind == ValFunc {
		return resolveFunc(v, ctx)
	}
	return v.Raw, nil
}

// resolveDate turns a date literal into a time.Time: a relative duration is
// added to the clock; otherwise RFC3339 (and the date-only YYYY-MM-DD form) are
// accepted.
func resolveDate(raw string, ctx EvalContext) (time.Time, error) {
	if d, ok := parseRelative(raw); ok {
		return ctx.now().Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("hql: %q is not a date (want RFC3339, YYYY-MM-DD, or a relative duration like -7d)", raw)
}

// funcArity maps each supported HQL function (lowercased) to its exact argument
// count. Every current function is niladic; a call with the wrong number of
// arguments is a query error rather than being silently ignored.
var funcArity = map[string]int{
	"now": 0, "today": 0, "startofday": 0, "endofday": 0,
}

// resolveFunc evaluates a function call. now()/today()/startOfDay()/endOfDay()
// derive from the clock. Unknown functions are ErrUnsupported; a known
// function called with the wrong arity is a plain error.
func resolveFunc(v Value, ctx EvalContext) (any, error) {
	name := strings.ToLower(v.Raw)
	arity, known := funcArity[name]
	if !known {
		return nil, fmt.Errorf("%w: function %s()", ErrUnsupported, v.Raw)
	}
	if len(v.Args) != arity {
		return nil, fmt.Errorf("hql: %s() takes %d argument(s), got %d", v.Raw, arity, len(v.Args))
	}
	switch name {
	case "now":
		return ctx.now(), nil
	case "today", "startofday":
		t := ctx.now()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()), nil
	case "endofday":
		t := ctx.now()
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location()), nil
	default:
		return nil, fmt.Errorf("%w: function %s()", ErrUnsupported, v.Raw)
	}
}

// escapeLike escapes SQL LIKE/ILIKE metacharacters so a value is matched
// literally under `ILIKE ... ESCAPE '\'`.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
