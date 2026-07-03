package okf

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeSHA(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "ascii", body: "pacs.008 message body"},
		{name: "utf8 portuguese", body: "Devolução de Pix com remoção de duplicidade"},
		{name: "multiline", body: "line one\nline two\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(tt.body))
			want := hex.EncodeToString(sum[:])
			got := ComputeSHA(tt.body)
			assert.Equal(t, want, got)
			assert.Len(t, got, 64)
		})
	}
}

func TestComputeSHADeterministic(t *testing.T) {
	body := "stable content"
	assert.Equal(t, ComputeSHA(body), ComputeSHA(body))
}
