package conf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathAnalyticsConfValidate(t *testing.T) {
	for _, ca := range []struct {
		name    string
		conf    PathAnalyticsConf
		wantErr bool
		wantFPS int // expected FPS after validate (default-fill check)
	}{
		{
			name: "disabled",
			conf: PathAnalyticsConf{Enabled: false},
		},
		{
			name: "enabled, no modules",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     10,
			},
			wantErr: true,
		},
		{
			name: "enabled, valid",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     15,
				Modules: []PathAnalyticsModule{
					{Name: "lpr"},
				},
			},
			wantFPS: 15,
		},
		{
			name: "fps default fills in 10 when zero",
			conf: PathAnalyticsConf{
				Enabled: true,
				Modules: []PathAnalyticsModule{
					{Name: "lpr"},
				},
			},
			wantFPS: 10,
		},
		{
			name: "fps too low",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     0,
				Modules: []PathAnalyticsModule{{Name: "lpr"}},
			},
			wantFPS: 10, // default-filled, then valid
		},
		{
			name: "fps too high",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     61,
				Modules: []PathAnalyticsModule{{Name: "lpr"}},
			},
			wantErr: true,
		},
		{
			name: "duplicate module",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     10,
				Modules: []PathAnalyticsModule{
					{Name: "lpr"},
					{Name: "lpr"},
				},
			},
			wantErr: true,
		},
		{
			name: "empty module name",
			conf: PathAnalyticsConf{
				Enabled: true,
				FPS:     10,
				Modules: []PathAnalyticsModule{
					{Name: ""},
				},
			},
			wantErr: true,
		},
	} {
		t.Run(ca.name, func(t *testing.T) {
			err := ca.conf.validate()
			if ca.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if ca.wantFPS != 0 {
				require.Equal(t, ca.wantFPS, ca.conf.FPS)
			}
		})
	}
}

func TestPathAnalyticsConfUnmarshalEnv(t *testing.T) {
	t.Run("empty value is no-op", func(t *testing.T) {
		var c PathAnalyticsConf
		err := c.UnmarshalEnv("PREFIX", "")
		require.NoError(t, err)
		require.False(t, c.Enabled)
	})

	t.Run("json value populates struct", func(t *testing.T) {
		var c PathAnalyticsConf
		err := c.UnmarshalEnv("PREFIX", `{"enabled":true,"fps":12,"modules":[{"name":"lpr","config":{"plate_type":"auto"}}]}`)
		require.NoError(t, err)
		require.True(t, c.Enabled)
		require.Equal(t, 12, c.FPS)
		require.Len(t, c.Modules, 1)
		require.Equal(t, "lpr", c.Modules[0].Name)
		require.Equal(t, json.RawMessage(`{"plate_type":"auto"}`), c.Modules[0].Config)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		var c PathAnalyticsConf
		err := c.UnmarshalEnv("PREFIX", `{"enabled":}`)
		require.Error(t, err)
	})
}
