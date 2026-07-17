package hql

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse tokenizes and parses input into a Query. Precedence is NOT > AND > OR;
// parentheses override. Keywords and field names are case-insensitive.
func Parse(input string) (Query, error) {
	toks, err := lex(input)
	if err != nil {
		return Query{}, err
	}
	p := &parser{toks: toks}
	if p.peek().kind == tEOF {
		return Query{}, fmt.Errorf("hql: empty query")
	}
	where, err := p.parseOr()
	if err != nil {
		return Query{}, err
	}
	q := Query{Where: where}
	if p.isKeyword("ORDER") {
		p.next()
		if !p.isKeyword("BY") {
			return Query{}, p.errf("expected BY after ORDER")
		}
		p.next()
		ob, err := p.parseOrder()
		if err != nil {
			return Query{}, err
		}
		q.OrderBy = ob
	}
	if p.isKeyword("LIMIT") {
		p.next()
		lim, err := p.parseLimit()
		if err != nil {
			return Query{}, err
		}
		q.Limit = lim
	}
	if p.peek().kind != tEOF {
		return Query{}, p.errf("unexpected token %q", p.peek().val)
	}
	return q, nil
}

// parseLimit reads the non-negative integer after a LIMIT keyword.
func (p *parser) parseLimit() (int, error) {
	t := p.peek()
	if t.kind != tWord {
		return 0, p.errf("expected a number after LIMIT")
	}
	n, err := strconv.Atoi(t.val)
	if err != nil || n < 0 {
		return 0, p.errf("LIMIT must be a non-negative integer, got %q", t.val)
	}
	p.next()
	return n, nil
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

// isKeyword reports whether the current token is the bareword keyword kw
// (case-insensitive).
func (p *parser) isKeyword(kw string) bool {
	t := p.peek()
	return t.kind == tWord && strings.EqualFold(t.val, kw)
}

func (p *parser) errf(format string, a ...any) error {
	return fmt.Errorf("hql: "+format+" (at %d)", append(a, p.peek().pos)...)
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("OR") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Or{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("AND") {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &And{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.isKeyword("NOT") {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Not{X: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	if p.peek().kind == tLParen {
		p.next()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, p.errf("expected )")
		}
		p.next()
		return e, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (Expr, error) {
	if p.peek().kind != tWord {
		return nil, p.errf("expected a field name")
	}
	field := p.next().val

	switch {
	case p.peek().kind == tOp:
		op := p.next().val
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return &Comparison{Field: field, Op: op, Value: &v}, nil
	case p.isKeyword("IN"):
		p.next()
		list, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &Comparison{Field: field, Op: OpIn, List: list}, nil
	case p.isKeyword("NOT"):
		p.next()
		if !p.isKeyword("IN") {
			return nil, p.errf("expected IN after NOT")
		}
		p.next()
		list, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &Comparison{Field: field, Op: OpNotIn, List: list}, nil
	case p.isKeyword("IS"):
		p.next()
		op := OpEmpty
		if p.isKeyword("NOT") {
			p.next()
			op = OpNotEmpty
		}
		if !p.isKeyword("EMPTY") {
			return nil, p.errf("expected EMPTY")
		}
		p.next()
		return &Comparison{Field: field, Op: op}, nil
	default:
		return nil, p.errf("expected an operator after field %q", field)
	}
}

// parseValue reads a scalar value: a quoted string, a bareword, or a function
// call (bareword immediately followed by "(").
func (p *parser) parseValue() (Value, error) {
	t := p.peek()
	switch t.kind {
	case tString:
		p.next()
		return Value{Kind: ValString, Raw: t.val}, nil
	case tWord:
		p.next()
		if p.peek().kind == tLParen { // function call
			p.next()
			var args []Value
			for p.peek().kind != tRParen {
				a, err := p.parseValue()
				if err != nil {
					return Value{}, err
				}
				args = append(args, a)
				if p.peek().kind == tComma {
					p.next()
					continue
				}
				break
			}
			if p.peek().kind != tRParen {
				return Value{}, p.errf("expected ) closing %s(", t.val)
			}
			p.next()
			return Value{Kind: ValFunc, Raw: t.val, Args: args}, nil
		}
		return Value{Kind: ValWord, Raw: t.val}, nil
	default:
		return Value{}, p.errf("expected a value")
	}
}

func (p *parser) parseList() ([]Value, error) {
	if p.peek().kind != tLParen {
		return nil, p.errf("expected ( to start a list")
	}
	p.next()
	var list []Value
	for p.peek().kind != tRParen {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		if p.peek().kind == tComma {
			p.next()
			continue
		}
		break
	}
	if p.peek().kind != tRParen {
		return nil, p.errf("expected ) to close the list")
	}
	p.next()
	if len(list) == 0 {
		return nil, p.errf("empty list")
	}
	return list, nil
}

func (p *parser) parseOrder() ([]OrderField, error) {
	var out []OrderField
	for {
		if p.peek().kind != tWord {
			return nil, p.errf("expected a field name in ORDER BY")
		}
		of := OrderField{Field: p.next().val}
		switch {
		case p.isKeyword("ASC"):
			p.next()
		case p.isKeyword("DESC"):
			p.next()
			of.Desc = true
		}
		out = append(out, of)
		if p.peek().kind == tComma {
			p.next()
			continue
		}
		break
	}
	return out, nil
}
