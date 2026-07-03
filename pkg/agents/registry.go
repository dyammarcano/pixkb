package agents

import (
	"sort"
	"sync"
)

var (
	regMu     sync.RWMutex
	factories []func() Agent
)

// Register adds an agent factory to the global roster. Call it from an init()
// function so the roster is fully assembled before main runs. The lazy-factory
// pattern (a func returning the Agent, not the Agent value) mirrors lensr
// aihost and keeps registration free of import cycles.
func Register(f func() Agent) {
	regMu.Lock()
	defer regMu.Unlock()
	factories = append(factories, f)
}

// All returns every registered agent, materialized and sorted by name so the
// roster is deterministic across runs.
func All() []Agent {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Agent, 0, len(factories))
	for _, f := range factories {
		out = append(out, f())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByName returns the registered agent with the given name.
func ByName(name string) (Agent, bool) {
	for _, a := range All() {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}
