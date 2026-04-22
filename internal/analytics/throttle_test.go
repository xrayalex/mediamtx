//go:build analytics

package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFPSThrottle(t *testing.T) {
	t.Run("interval derived from fps", func(t *testing.T) {
		require.Equal(t, 100*time.Millisecond, newFPSThrottle(10).interval)
		require.Equal(t, time.Second, newFPSThrottle(1).interval)
	})

	t.Run("first frame always passes", func(t *testing.T) {
		thr := newFPSThrottle(10)
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		require.True(t, thr.shouldEmit(now, false))
	})

	t.Run("idr always passes and resets the timer", func(t *testing.T) {
		thr := newFPSThrottle(10)
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		require.True(t, thr.shouldEmit(t0, false))
		// 50ms later — under the 100ms budget — but it's an IDR.
		require.True(t, thr.shouldEmit(t0.Add(50*time.Millisecond), true))
		// 50ms after the IDR — should now be dropped because IDR reset timer.
		require.False(t, thr.shouldEmit(t0.Add(100*time.Millisecond), false))
	})

	t.Run("p-frame within interval is dropped", func(t *testing.T) {
		thr := newFPSThrottle(10) // 100ms interval
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		require.True(t, thr.shouldEmit(t0, false))
		require.False(t, thr.shouldEmit(t0.Add(50*time.Millisecond), false))
		require.False(t, thr.shouldEmit(t0.Add(99*time.Millisecond), false))
	})

	t.Run("p-frame past interval passes", func(t *testing.T) {
		thr := newFPSThrottle(10)
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		require.True(t, thr.shouldEmit(t0, false))
		require.True(t, thr.shouldEmit(t0.Add(100*time.Millisecond), false))
	})
}
