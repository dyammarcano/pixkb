package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWatch_FiresOnDrop(t *testing.T) {
	dir := t.TempDir()
	got := make(chan []string, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = Watch(ctx, dir, 40*time.Millisecond, func(_ context.Context, files []string) error {
			got <- files
			return nil
		})
	}()

	time.Sleep(80 * time.Millisecond) // let the watcher register
	require := assert.New(t)
	require.NoError(os.WriteFile(filepath.Join(dir, "drop.txt"), []byte("hi"), 0o644))

	select {
	case files := <-got:
		assert.NotEmpty(t, files)
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not fire on file drop")
	}
}

func TestWatch_ContextCancels(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, dir, 10*time.Millisecond, func(context.Context, []string) error { return nil }) }()
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not return on cancel")
	}
}
