package main

import (
	"fmt"
	"os"
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pixkb:", err)
		os.Exit(1)
	}
}
