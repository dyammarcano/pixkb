package okf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendLog appends a single line to <bundleDir>/log.md, creating the file (and
// parent directory) if needed. Any trailing newline on line is normalized so
// exactly one newline terminates the entry.
func AppendLog(bundleDir, line string) error {
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return fmt.Errorf("append log: mkdir %q: %w", bundleDir, err)
	}
	dest := filepath.Join(bundleDir, logFile)
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("append log: open %q: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	entry := strings.TrimRight(line, "\n") + "\n"
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append log: write: %w", err)
	}
	return nil
}
