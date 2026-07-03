package okf

import (
	"crypto/sha256"
	"encoding/hex"
)

// ComputeSHA returns the lowercase sha256 hex digest of body.
func ComputeSHA(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
