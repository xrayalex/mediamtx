//go:build !analytics

package core

// coreAnalytics is empty without the analytics build tag so the Core
// struct can declare the field unconditionally and the lifecycle
// hooks compile to no-ops.
type coreAnalytics struct{}

func (*Core) startAnalyticsPublisher()                  {}
func (*Core) stopAnalyticsPublisher()                   {}
func (*Core) attachAnalyticsToPathManager(*pathManager) {}
