//go:build analytics

package analytics

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluenviron/mediamtx/internal/unit"
)

func TestBuildExtradata(t *testing.T) {
	t.Run("nil when missing", func(t *testing.T) {
		require.Nil(t, BuildExtradata(nil, []byte{1}))
		require.Nil(t, BuildExtradata([]byte{1}, nil))
	})

	t.Run("annex-b prefixed sps and pps", func(t *testing.T) {
		sps := []byte{0x67, 0x42}
		pps := []byte{0x68, 0xCE}
		got := BuildExtradata(sps, pps)
		require.Equal(t, []byte{
			0, 0, 0, 1, 0x67, 0x42,
			0, 0, 0, 1, 0x68, 0xCE,
		}, got)
	})
}

func TestAuToAnnexB(t *testing.T) {
	t.Run("non-IDR access unit", func(t *testing.T) {
		au := unit.PayloadH264{
			{0x61, 0xAA}, // NALU type 1, P-frame slice
			{0x06, 0xBB}, // NALU type 6, SEI
		}
		bytes, isIDR := auToAnnexB(au)
		require.False(t, isIDR)
		require.Equal(t, []byte{
			0, 0, 0, 1, 0x61, 0xAA,
			0, 0, 0, 1, 0x06, 0xBB,
		}, bytes)
	})

	t.Run("IDR access unit", func(t *testing.T) {
		au := unit.PayloadH264{
			{0x65, 0xCC}, // NALU type 5, IDR
			{0x06, 0xDD},
		}
		bytes, isIDR := auToAnnexB(au)
		require.True(t, isIDR)
		require.Equal(t, []byte{
			0, 0, 0, 1, 0x65, 0xCC,
			0, 0, 0, 1, 0x06, 0xDD,
		}, bytes)
	})

	t.Run("empty access unit produces empty output", func(t *testing.T) {
		bytes, isIDR := auToAnnexB(unit.PayloadH264{})
		require.False(t, isIDR)
		require.Empty(t, bytes)
	})

	t.Run("skip empty NALU when checking IDR", func(t *testing.T) {
		au := unit.PayloadH264{
			{}, // empty NALU should not crash
			{0x65},
		}
		_, isIDR := auToAnnexB(au)
		require.True(t, isIDR)
	})
}
