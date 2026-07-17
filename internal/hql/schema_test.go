package hql

import (
	"testing"
)

func TestLookupFieldCaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase text", "text", true},
		{"uppercase TEXT", "TEXT", true},
		{"mixed case Text", "Text", true},
		{"lowercase type", "type", true},
		{"uppercase TYPE", "TYPE", true},
		{"lowercase domain", "domain", true},
		{"uppercase DOMAIN", "DOMAIN", true},
		{"unknown field", "unknown", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := lookupField(tt.input)
			if ok != tt.expected {
				t.Errorf("lookupField(%q) returned ok=%v, want %v", tt.input, ok, tt.expected)
			}
		})
	}
}

func TestLookupFieldMapping(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		wantColumn string
		wantPrefix string
		wantKind   fieldKind
		wantOk     bool
	}{
		// Text fields
		{"text field", "text", "body", "", kText, true},
		{"body field", "body", "body", "", kText, true},
		{"title field", "title", "title", "", kText, true},
		{"description field", "description", "description", "", kText, true},
		{"intent_terms field", "intent_terms", "intent_terms", "", kText, true},
		{"source_uri field", "source_uri", "source_uri", "", kText, true},

		// ID fields
		{"type field", "type", "type", "", kID, true},
		{"id field", "id", "id", "", kID, true},
		{"language field", "language", "language", "", kID, true},

		// TagPrefix fields
		{"tag field (no prefix)", "tag", "tags", "", kTagPrefix, true},
		{"domain field", "domain", "tags", "domain:", kTagPrefix, true},
		{"lei field", "lei", "tags", "lei:", kTagPrefix, true},
		{"livro field", "livro", "tags", "livro:", kTagPrefix, true},
		{"titulo field", "titulo", "tags", "titulo:", kTagPrefix, true},
		{"capitulo field", "capitulo", "tags", "capitulo:", kTagPrefix, true},
		{"secao field", "secao", "tags", "secao:", kTagPrefix, true},

		// Int fields
		{"epoch field", "epoch", "last_epoch", "", kInt, true},

		// Date fields
		{"updated field", "updated", "updated_at", "", kDate, true},

		// Unknown field
		{"unknown field", "nonexistent", "", "", fieldKind(0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := lookupField(tt.fieldName)
			if ok != tt.wantOk {
				t.Errorf("lookupField(%q) returned ok=%v, want %v", tt.fieldName, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if f.column != tt.wantColumn {
				t.Errorf("lookupField(%q).column = %q, want %q", tt.fieldName, f.column, tt.wantColumn)
			}
			if f.prefix != tt.wantPrefix {
				t.Errorf("lookupField(%q).prefix = %q, want %q", tt.fieldName, f.prefix, tt.wantPrefix)
			}
			if f.kind != tt.wantKind {
				t.Errorf("lookupField(%q).kind = %v, want %v", tt.fieldName, f.kind, tt.wantKind)
			}
		})
	}
}

func TestLookupFieldSpecificChecks(t *testing.T) {
	// Spot-check specific important fields

	// text → body/kText
	f, ok := lookupField("text")
	if !ok || f.column != "body" || f.kind != kText {
		t.Errorf("text field check failed: ok=%v, column=%q, kind=%v", ok, f.column, f.kind)
	}

	// type → type/kID
	f, ok = lookupField("type")
	if !ok || f.column != "type" || f.kind != kID {
		t.Errorf("type field check failed: ok=%v, column=%q, kind=%v", ok, f.column, f.kind)
	}

	// domain → tags/prefix "domain:"/kTagPrefix
	f, ok = lookupField("domain")
	if !ok || f.column != "tags" || f.prefix != "domain:" || f.kind != kTagPrefix {
		t.Errorf("domain field check failed: ok=%v, column=%q, prefix=%q, kind=%v", ok, f.column, f.prefix, f.kind)
	}

	// lei → tags/"lei:"/kTagPrefix
	f, ok = lookupField("lei")
	if !ok || f.column != "tags" || f.prefix != "lei:" || f.kind != kTagPrefix {
		t.Errorf("lei field check failed: ok=%v, column=%q, prefix=%q, kind=%v", ok, f.column, f.prefix, f.kind)
	}

	// tag → tags/empty-prefix/kTagPrefix
	f, ok = lookupField("tag")
	if !ok || f.column != "tags" || f.prefix != "" || f.kind != kTagPrefix {
		t.Errorf("tag field check failed: ok=%v, column=%q, prefix=%q, kind=%v", ok, f.column, f.prefix, f.kind)
	}

	// epoch → last_epoch/kInt
	f, ok = lookupField("epoch")
	if !ok || f.column != "last_epoch" || f.kind != kInt {
		t.Errorf("epoch field check failed: ok=%v, column=%q, kind=%v", ok, f.column, f.kind)
	}

	// updated → updated_at/kDate
	f, ok = lookupField("updated")
	if !ok || f.column != "updated_at" || f.kind != kDate {
		t.Errorf("updated field check failed: ok=%v, column=%q, kind=%v", ok, f.column, f.kind)
	}
}

func TestLookupFieldUnknown(t *testing.T) {
	_, ok := lookupField("unknown_field_xyz")
	if ok {
		t.Errorf("lookupField(\"unknown_field_xyz\") should return false, but returned true")
	}
}
