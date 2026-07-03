// Package watch debounces fsnotify events on the ingest drop-directory so an
// offline daemon can re-ingest when new artifacts are staged (sneakernet/diode).
package watch

import (
	"context"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch observes dir for create/write events, debounces bursts by the given
// duration, and calls run with the set of changed file paths. It blocks until
// ctx is cancelled. Errors from run do not stop the watcher — a bad drop must
// not kill the daemon; run is expected to log its own failures.
func Watch(ctx context.Context, dir string, debounce time.Duration, run func(ctx context.Context, files []string) error) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	if err := w.Add(dir); err != nil {
		return err
	}

	pending := make(map[string]struct{})
	var timerC <-chan time.Time
	var timer *time.Timer

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				pending[ev.Name] = struct{}{}
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(debounce)
				timerC = timer.C
			}

		case <-timerC:
			timerC = nil
			if len(pending) == 0 {
				continue
			}
			files := make([]string, 0, len(pending))
			for f := range pending {
				files = append(files, f)
			}
			pending = make(map[string]struct{})
			_ = run(ctx, files)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}
