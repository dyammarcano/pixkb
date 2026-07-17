package hql

import (
	"fmt"
	"strings"
)

// tokKind classifies a lexed token.
type tokKind int

const (
	tEOF    tokKind = iota
	tWord           // bareword: field name, keyword (AND/OR/…), or unquoted value
	tString         // quoted string literal
	tOp             // = != ~ !~ > >= < <=
	tLParen
	tRParen
	tComma
)

// token is one lexical unit with its source offset (for error messages).
type token struct {
	kind tokKind
	val  string
	pos  int
}

// isWordByte reports whether b can appear in a bareword. Barewords carry field
// names, keywords, and unquoted values including relative durations ("-7d"),
// dates ("2026-07-01"), ids/URLs, and emails. Operator characters (= ! < > ~)
// and structural characters ( ) , are excluded, and '-' is only ever a word
// byte (HQL has no arithmetic), so "-7d" lexes as a single word.
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_' || b == '.' || b == ':' || b == '@' || b == '%' || b == '/' || b == '-' || b == '+' || b == '*':
		return true
	}
	return false
}

// lex tokenizes input. Quoted strings accept ' or " and support a doubled quote
// as an escape ('it”s'). It returns an error on an unterminated string or a
// stray operator character.
func lex(input string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(input) {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "(", i})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")", i})
			i++
		case c == ',':
			toks = append(toks, token{tComma, ",", i})
			i++
		case c == '\'' || c == '"':
			s, next, err := lexString(input, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tString, s, i})
			i = next
		case c == '=' || c == '~':
			toks = append(toks, token{tOp, string(c), i})
			i++
		case c == '!' || c == '<' || c == '>':
			op, next := lexOp(input, i)
			toks = append(toks, token{tOp, op, i})
			i = next
		case isWordByte(c):
			start := i
			for i < len(input) && isWordByte(input[i]) {
				i++
			}
			toks = append(toks, token{tWord, input[start:i], start})
		default:
			return nil, fmt.Errorf("hql: unexpected character %q at %d", string(c), i)
		}
	}
	toks = append(toks, token{tEOF, "", len(input)})
	return toks, nil
}

// lexOp reads a one- or two-character operator starting at i ('!' → "!=" or
// "!~"; '<'/'>' → "<="/">=" or bare). It never fails; a lone '!' becomes "!".
func lexOp(input string, i int) (string, int) {
	c := input[i]
	if i+1 < len(input) {
		if n := input[i+1]; (c == '!' && (n == '=' || n == '~')) || ((c == '<' || c == '>') && n == '=') {
			return input[i : i+2], i + 2
		}
	}
	return string(c), i + 1
}

// lexString reads a quoted literal beginning at the quote at i, returning the
// unquoted value and the index just past the closing quote. A doubled quote
// inside the literal is an escaped quote.
func lexString(input string, i int) (string, int, error) {
	q := input[i]
	var b strings.Builder
	j := i + 1
	for j < len(input) {
		if input[j] == q {
			if j+1 < len(input) && input[j+1] == q { // doubled quote escape
				b.WriteByte(q)
				j += 2
				continue
			}
			return b.String(), j + 1, nil
		}
		b.WriteByte(input[j])
		j++
	}
	return "", 0, fmt.Errorf("hql: unterminated string starting at %d", i)
}
