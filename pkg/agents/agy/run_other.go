//go:build !windows

package agy

import (
	"context"
	"errors"

	"pixkb/pkg/agents"
)

// run is unsupported off Windows: the agy headless workaround needs a ConPTY.
func (d *Driver) run(_ context.Context, _ string) (string, error) {
	return "", errors.New("agy: ConPTY driver is Windows-only")
}

// openSession falls back to a one-shot session off Windows (no ConPTY). It is
// effectively unusable until run is supported there, but keeps the interface
// uniform so the Agency compiles and degrades gracefully.
func (d *Driver) openSession(_ context.Context) (agents.Session, error) {
	return agents.OneShot(d), nil
}
