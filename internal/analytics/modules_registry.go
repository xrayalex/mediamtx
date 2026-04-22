//go:build analytics

package analytics

import (
	"fmt"
	"sort"
	"sync"
)

// Factory constructs a fresh Module instance.
type Factory func() Module

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// RegisterModule registers a Factory under name. Intended to be called from
// init() in the module subpackage. Panics on duplicate registration.
func RegisterModule(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("analytics: module already registered: " + name)
	}
	registry[name] = factory
}

// NewModule returns a fresh Module instance for the given name.
func NewModule(name string) (Module, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("analytics: module %q not registered", name)
	}
	return factory(), nil
}

// ModuleNames returns the sorted list of registered module names.
func ModuleNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
