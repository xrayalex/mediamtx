//go:build analytics

package analytics

import "time"

// fpsThrottle decides whether to emit a frame so that the average
// per-second emission stays at most at the configured FPS.
//
// IDR frames always pass — the H.264 decoder needs every keyframe to
// keep its reference state valid for subsequent P-frames. Throttling
// only applies to P/B-frames.
//
// Single-goroutine: the reader's stream callback runs serialised so
// no locking is required.
type fpsThrottle struct {
	interval time.Duration
	lastEmit time.Time
}

// newFPSThrottle returns a throttle that targets the given FPS budget.
// fps must be > 0; the reader is expected to validate this.
func newFPSThrottle(fps int) *fpsThrottle {
	return &fpsThrottle{interval: time.Second / time.Duration(fps)}
}

// shouldEmit reports whether the frame at time now should be emitted,
// updating internal state when it returns true.
func (t *fpsThrottle) shouldEmit(now time.Time, isIDR bool) bool {
	if isIDR {
		t.lastEmit = now
		return true
	}
	if t.lastEmit.IsZero() {
		t.lastEmit = now
		return true
	}
	if now.Sub(t.lastEmit) < t.interval {
		return false
	}
	t.lastEmit = now
	return true
}
