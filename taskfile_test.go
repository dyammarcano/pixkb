package pixkb_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTaskfileTargets(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("Taskfile.yml")
	require.NoError(t, err)

	var tf struct {
		Version string                 `yaml:"version"`
		Tasks   map[string]interface{} `yaml:"tasks"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &tf))
	assert.Equal(t, "3", tf.Version)

	for _, target := range []string{"build", "test", "test:full", "lint", "migrate"} {
		_, ok := tf.Tasks[target]
		assert.Truef(t, ok, "Taskfile missing target %q", target)
	}
}
