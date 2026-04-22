//go:build !analytics

package core

// pathAnalytics is empty without the analytics build tag; the path
// struct still declares the field and the lifecycle hooks compile to
// no-ops.
type pathAnalytics struct{}

func (*path) startAnalytics() {}
func (*path) stopAnalytics()  {}
