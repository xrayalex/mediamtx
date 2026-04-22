package conf

import (
	"encoding/json"
	"fmt"

	"github.com/bluenviron/mediamtx/internal/conf/jsonwrapper"
)

// PathAnalyticsConf is the per-path analytics configuration block.
//
// The type is defined unconditionally (no build tag) so that the YAML/JSON
// parser accepts an "analytics:" stanza in every build. The runtime code
// that actually uses the values lives behind the "analytics" build tag;
// without it, paths just hold the parsed value and ignore it.
type PathAnalyticsConf struct {
	Enabled bool                  `json:"enabled"`
	FPS     int                   `json:"fps"`
	Modules []PathAnalyticsModule `json:"modules"`
}

// PathAnalyticsModule selects one analytics module by name and carries its
// opaque configuration. Config is delivered to Module.Configure() as-is so
// each module owns its own parsing.
type PathAnalyticsModule struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config,omitempty"`
}

// UnmarshalEnv lets the env loader treat the analytics block as a single
// JSON-encoded environment variable (e.g.
// MTX_PATHS_CAM1_ANALYTICS='{"enabled":true,"fps":10,"modules":[...]}').
//
// Per-field env overrides for nested slices of structs are not supported;
// when the env loader encounters child variables under this prefix
// (v == ""), we simply do nothing. This also prevents the loader from
// trying to recurse through a nil-pointer view of the struct, which it
// produces internally via OptionalPath and which would otherwise panic.
func (c *PathAnalyticsConf) UnmarshalEnv(_ string, v string) error {
	if v == "" {
		return nil
	}
	return jsonwrapper.Unmarshal([]byte(v), c)
}

// validate enforces the structural invariants of a PathAnalyticsConf.
// Module-name validity against the registry is left to runtime, since the
// registry only exists under the analytics build tag and importing it
// here would create a build-time dependency.
func (c *PathAnalyticsConf) validate() error {
	if !c.Enabled {
		return nil
	}

	if c.FPS == 0 {
		c.FPS = 10
	}
	if c.FPS < 1 || c.FPS > 60 {
		return fmt.Errorf("fps must be in [1, 60], got %d", c.FPS)
	}

	if len(c.Modules) == 0 {
		return fmt.Errorf("at least one module must be listed when enabled")
	}

	seen := make(map[string]struct{}, len(c.Modules))
	for i, m := range c.Modules {
		if m.Name == "" {
			return fmt.Errorf("modules[%d]: name is empty", i)
		}
		if _, dup := seen[m.Name]; dup {
			return fmt.Errorf("modules[%d]: duplicate module %q", i, m.Name)
		}
		seen[m.Name] = struct{}{}
	}

	return nil
}
