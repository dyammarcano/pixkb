package ispb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateISPB(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{name: "valid 8 digits", code: "00000208", wantErr: false},
		{name: "too short", code: "1234", wantErr: true},
		{name: "too long", code: "123456789", wantErr: true},
		{name: "non-digit", code: "0000020A", wantErr: true},
		{name: "empty", code: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateISPB(tt.code)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
