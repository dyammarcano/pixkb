package agents

import (
	"fmt"
	"sort"
	"sync"
)

// Provider packages (agy, codex, claude) self-register here in init() via a
// lazy factory, so this core package never imports them — mirroring the
// host-registry pattern and keeping the dependency graph acyclic. Import the
// barrel `pkg/agents/all` (or the individual provider packages) to populate it.
var (
	provMu        sync.RWMutex
	provFactories = map[string]func() Provider{}
)

// RegisterProvider registers a backend factory under one or more names/aliases
// (e.g. "codex"; "claude","code"). Call from the provider package's init().
func RegisterProvider(factory func() Provider, names ...string) {
	provMu.Lock()
	defer provMu.Unlock()
	for _, n := range names {
		provFactories[n] = factory
	}
}

// DefaultProvider is used when ProviderByName is given an empty name.
const DefaultProvider = "codex"

// ProviderByName returns a fresh provider for the backend name (or the default
// when empty). It errors if no provider package registered that name — usually
// because the barrel import is missing.
func ProviderByName(name string) (Provider, error) {
	if name == "" {
		name = DefaultProvider
	}
	provMu.RLock()
	f, ok := provFactories[name]
	provMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown agent provider %q (registered: %v) — is the provider package imported?", name, RegisteredProviders())
	}
	return f(), nil
}

// RegisteredProviders lists the registered backend names, sorted.
func RegisteredProviders() []string {
	provMu.RLock()
	defer provMu.RUnlock()
	out := make([]string, 0, len(provFactories))
	for n := range provFactories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
