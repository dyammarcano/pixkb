// Package build carries the release version, overridden via -X ldflags at
// release build time (see .goreleaser.yaml).
package build

var version = "0.0.0-dev"

// Version returns the build version.
func Version() string { return version }
