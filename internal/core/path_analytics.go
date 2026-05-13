//go:build analytics

package core

import (
	"github.com/bluenviron/mediamtx/internal/analytics"
	"github.com/bluenviron/mediamtx/internal/logger"
)

// analyticsPublisherProvider is implemented by *pathManager (under the
// analytics tag) so the path can fetch the singleton without making
// pathParent itself build-tag dependent.
type analyticsPublisherProvider interface {
	analyticsPublisher() *analytics.Publisher
}

// pathAnalytics carries the per-path Reader handle. Lives on the path
// struct so we don't sprinkle "if analytics-tag" branches across
// path.go.
type pathAnalytics struct {
	reader *analytics.Reader
}

// startAnalytics builds modules from PathConf.Analytics and starts a
// Reader subscribed to the path's stream.
//
// Best-effort: any failure (no publisher, unknown module, module
// Configure fails — typically PlateCore license issues) is logged at
// warn level and the path stays alive without analytics. This matches
// the brief's requirement that LPR degrades gracefully when the
// license proxy is missing.
func (pa *path) startAnalytics() {
	if !pa.conf.Analytics.Enabled {
		return
	}
	if pa.stream == nil {
		return
	}
	prov, ok := pa.parent.(analyticsPublisherProvider)
	if !ok {
		return
	}
	pub := prov.analyticsPublisher()
	if pub == nil {
		pa.Log(logger.Warn, "analytics enabled but publisher is unavailable; skipping")
		return
	}

	modules := make([]analytics.Module, 0, len(pa.conf.Analytics.Modules))
	for _, mc := range pa.conf.Analytics.Modules {
		m, err := analytics.NewModule(mc.Name)
		if err != nil {
			pa.Log(logger.Warn, "analytics module %q: %v", mc.Name, err)
			continue
		}
		if err := m.Configure(pa.name, mc.Config); err != nil {
			pa.Log(logger.Warn, "analytics module %q configure: %v", mc.Name, err)
			_ = m.Close()
			continue
		}
		modules = append(modules, m)
	}
	if len(modules) == 0 {
		return
	}

	fps := pa.conf.Analytics.FPS
	if fps == 0 {
		fps = 10
	}

	reader := &analytics.Reader{
		Stream:          pa.stream,
		CameraID:        pa.name,
		Modules:         modules,
		FPS:             fps,
		Pub:             pub,
		Parent:          pa,
		FrameBufferSize: pa.conf.Analytics.FrameBufferSize,
	}
	if err := reader.Start(); err != nil {
		pa.Log(logger.Warn, "analytics reader start: %v", err)
		for _, m := range modules {
			_ = m.Close()
		}
		return
	}
	pa.analytics.reader = reader
}

// stopAnalytics tears down the Reader if one is running. Idempotent.
func (pa *path) stopAnalytics() {
	if pa.analytics.reader != nil {
		pa.analytics.reader.Close()
		pa.analytics.reader = nil
	}
}
