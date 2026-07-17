package hql

// Query is a parsed HQL statement: a boolean filter plus optional ordering and
// row limit.
type Query struct {
	Where   Expr
	OrderBy []OrderField
	// Limit caps the result count; 0 means no explicit limit (callers apply
	// their own default).
	Limit int
}

// OrderField is one ORDER BY term.
type OrderField struct {
	Field string
	Desc  bool
}

// Expr is a boolean expression node (And/Or/Not/Comparison).
type Expr interface{ isExpr() }

// And / Or / Not are the logical combinators.
type And struct{ L, R Expr }
type Or struct{ L, R Expr }
type Not struct{ X Expr }

// Comparison is a single `field OP value` (or list / empty) test.
type Comparison struct {
	Field string
	Op    string  // one of the Op* constants
	Value *Value  // set for scalar ops; nil for IN/EMPTY
	List  []Value // set for IN / NOT IN
}

func (*And) isExpr()        {}
func (*Or) isExpr()         {}
func (*Not) isExpr()        {}
func (*Comparison) isExpr() {}

// Operator constants. Symbol ops keep their source spelling; word ops are lower.
const (
	OpEq          = "="
	OpNe          = "!="
	OpContains    = "~"
	OpNotContains = "!~"
	OpGt          = ">"
	OpGe          = ">="
	OpLt          = "<"
	OpLe          = "<="
	OpIn          = "in"
	OpNotIn       = "notin"
	OpEmpty       = "empty"
	OpNotEmpty    = "notempty"
)

// ValueKind classifies a literal value.
type ValueKind int

const (
	ValString ValueKind = iota // quoted literal
	ValWord                    // bareword: id, number, date, duration, enum
	ValFunc                    // function call: now(), today(), startOfDay(), …
)

// Value is a literal or function call in a comparison.
type Value struct {
	Kind ValueKind
	Raw  string  // string/word text, or function name for ValFunc
	Args []Value // function arguments (ValFunc only)
}
