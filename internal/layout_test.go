package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPackageDirsExist(t *testing.T) {
	t.Parallel()

	dirs := []string{
		"okf",
		"embed",
		filepath.Join("store", "postgres"),
		"ingest",
		"epoch",
		"query",
		"watch",
	}
	for _, d := range dirs {
		info, err := os.Stat(d)
		assert.NoErrorf(t, err, "missing internal dir %s", d)
		if err == nil {
			assert.Truef(t, info.IsDir(), "%s is not a directory", d)
		}
	}

	info, err := os.Stat(filepath.Join("..", "pkg", "okf"))
	assert.NoError(t, err)
	if err == nil {
		assert.True(t, info.IsDir())
	}
}
