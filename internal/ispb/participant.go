// Package ispb maps BACEN ISPB codes to SPB/Pix participant institutions.
package ispb

import (
	"fmt"
	"regexp"
	"time"
)

var ispbPattern = regexp.MustCompile(`^\d{8}$`)

// ValidateISPB checks that code is exactly 8 digits, zero-padded.
func ValidateISPB(code string) error {
	if !ispbPattern.MatchString(code) {
		return fmt.Errorf("invalid ISPB code %q: must be 8 digits", code)
	}
	return nil
}

// Participant is the merged, store-level view of an ISPB record: STR fields
// (canonical) plus Pix-specific fields. A source's fields are left at their
// zero value when that source has never been synced for this code.
type Participant struct {
	ISPB              string
	Name              string
	LegalName         string
	CompeCode         string
	ParticipatesCompe bool
	AccessType        string
	OperationStart    time.Time
	STRSyncedAt       time.Time
	CNPJ              string
	PixAuthorized     bool
	PixSyncedAt       time.Time
}
