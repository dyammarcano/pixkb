package hql

import "strings"

type fieldKind int

const (
	kText      fieldKind = iota // ILIKE substring + = != IN NOTIN IS[NOT]EMPTY
	kID                         // exact = != IN NOTIN (+ ~ !~ for id)
	kInt                        // = != > >= < <=
	kDate                       // = != > >= < <= against a timestamptz column
	kTagPrefix                  // tags @> ARRAY[prefix||value]; = != IN NOTIN IS[NOT]EMPTY
)

// field maps a DSL field to its concept-table source. For kTagPrefix, `column`
// is "tags" and `prefix` is prepended to the value ("domain:", "lei:", …; empty
// for the bare `tag` field). For all other kinds `prefix` is unused.
type field struct {
	column string
	prefix string
	kind   fieldKind
}

var fields = map[string]field{
	"text":         {column: "body", kind: kText},
	"body":         {column: "body", kind: kText},
	"title":        {column: "title", kind: kText},
	"description":  {column: "description", kind: kText},
	"intent_terms": {column: "intent_terms", kind: kText},
	"source_uri":   {column: "source_uri", kind: kText},
	"type":         {column: "type", kind: kID},
	"id":           {column: "id", kind: kID},
	"language":     {column: "language", kind: kID},
	"tag":          {column: "tags", prefix: "", kind: kTagPrefix},
	"domain":       {column: "tags", prefix: "domain:", kind: kTagPrefix},
	"lei":          {column: "tags", prefix: "lei:", kind: kTagPrefix},
	"livro":        {column: "tags", prefix: "livro:", kind: kTagPrefix},
	"titulo":       {column: "tags", prefix: "titulo:", kind: kTagPrefix},
	"capitulo":     {column: "tags", prefix: "capitulo:", kind: kTagPrefix},
	"secao":        {column: "tags", prefix: "secao:", kind: kTagPrefix},
	"epoch":        {column: "last_epoch", kind: kInt},
	"updated":      {column: "updated_at", kind: kDate},
}

func lookupField(name string) (field, bool) {
	f, ok := fields[strings.ToLower(name)]
	return f, ok
}
