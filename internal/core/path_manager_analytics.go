//go:build analytics

package core

import "github.com/bluenviron/mediamtx/internal/analytics"

// pathManagerAnalytics holds the analytics Publisher reference passed
// down from Core when the manager is constructed. Stored here so
// per-path startAnalytics() does not have to walk back up to *Core
// (which would force a tag-conditional method on pathManagerParent).
type pathManagerAnalytics struct {
	pub *analytics.Publisher
}

// analyticsPublisher implements analyticsPublisherProvider so the
// path's startAnalytics can fetch the singleton via type assertion on
// pa.parent without making pathParent build-tag dependent.
func (pm *pathManager) analyticsPublisher() *analytics.Publisher {
	return pm.analytics.pub
}
