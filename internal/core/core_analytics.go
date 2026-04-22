//go:build analytics

package core

import (
	"github.com/bluenviron/mediamtx/internal/analytics"
	"github.com/bluenviron/mediamtx/internal/logger"
)

// coreAnalytics carries the singleton Publisher used by every per-path
// analytics Reader. The Core struct embeds a value of this type so the
// shape of Core stays the same across build tags.
type coreAnalytics struct {
	pub *analytics.Publisher
}

// startAnalyticsPublisher attempts to construct the publisher.
//
// Failure (NATS or MinIO unreachable, missing creds) is downgraded to
// a warning and pub stays nil; analytics paths will then refuse to
// start and the rest of mediamtx keeps running. Operators can fix the
// dependency and restart.
func (p *Core) startAnalyticsPublisher() {
	if p.analytics.pub != nil {
		return
	}
	pub, err := analytics.NewPublisher(p)
	if err != nil {
		p.Log(logger.Warn, "analytics publisher disabled: %v", err)
		return
	}
	p.analytics.pub = pub
}

// stopAnalyticsPublisher closes the NATS connection. Called during
// full shutdown only; we do not recreate the publisher across reloads.
func (p *Core) stopAnalyticsPublisher() {
	if p.analytics.pub != nil {
		p.analytics.pub.Close()
		p.analytics.pub = nil
	}
}

// AnalyticsPublisher exposes the publisher to other packages in the
// fork (currently only pathManager uses it). Returns nil until
// startAnalyticsPublisher has succeeded.
func (p *Core) AnalyticsPublisher() *analytics.Publisher {
	return p.analytics.pub
}

// attachAnalyticsToPathManager copies the publisher reference into the
// manager's analytics state so per-path startAnalytics can fetch it
// via pm.analyticsPublisher() without walking back up to *Core.
func (p *Core) attachAnalyticsToPathManager(pm *pathManager) {
	pm.analytics.pub = p.analytics.pub
}
