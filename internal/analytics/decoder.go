//go:build analytics

package analytics

import (
	"errors"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"

	"github.com/bluenviron/mediamtx/internal/unit"
)

// H.264 NAL unit types we care about for extradata and IDR detection.
const (
	h264NALUTypeIDR = 5
	h264NALUTypeSPS = 7
	h264NALUTypePPS = 8
)

var annexBStartCode = []byte{0x00, 0x00, 0x00, 0x01}

// BuildExtradata returns an Annex-B-prefixed SPS+PPS byte slice, suitable as
// the prefix that the decoder needs before the first IDR. Returns nil if
// either parameter set is empty.
func BuildExtradata(sps, pps []byte) []byte {
	if len(sps) == 0 || len(pps) == 0 {
		return nil
	}
	out := make([]byte, 0, 2*len(annexBStartCode)+len(sps)+len(pps))
	out = append(out, annexBStartCode...)
	out = append(out, sps...)
	out = append(out, annexBStartCode...)
	out = append(out, pps...)
	return out
}

// Decoder turns Annex-B H.264 access units into BGR24 frames using libavcodec.
//
// Single-goroutine: SendPacket / ReceiveFrame state and the reusable BGR
// buffer are not protected by a lock.
type Decoder struct {
	// Extradata is the SPS+PPS Annex-B prefix prepended once to the first
	// IDR access unit so the decoder can initialise parameter sets even
	// when the camera does not embed them in every IDR.
	Extradata []byte

	codecCtx *astiav.CodecContext
	pkt      *astiav.Packet
	yuvFrame *astiav.Frame
	bgrFrame *astiav.Frame
	swsCtx   *astiav.SoftwareScaleContext

	extradataPrepended bool
}

// Initialize allocates the codec context and opens the H.264 decoder.
func (d *Decoder) Initialize() error {
	codec := astiav.FindDecoder(astiav.CodecIDH264)
	if codec == nil {
		return errors.New("h264 decoder not found in libavcodec")
	}

	d.codecCtx = astiav.AllocCodecContext(codec)
	if d.codecCtx == nil {
		return errors.New("failed to allocate H.264 codec context")
	}

	if err := d.codecCtx.Open(codec, nil); err != nil {
		d.codecCtx.Free()
		d.codecCtx = nil
		return fmt.Errorf("open H.264 codec: %w", err)
	}

	d.pkt = astiav.AllocPacket()
	d.yuvFrame = astiav.AllocFrame()
	d.bgrFrame = astiav.AllocFrame()
	return nil
}

// Close releases all FFmpeg resources.
func (d *Decoder) Close() {
	if d.swsCtx != nil {
		d.swsCtx.Free()
		d.swsCtx = nil
	}
	if d.bgrFrame != nil {
		d.bgrFrame.Free()
		d.bgrFrame = nil
	}
	if d.yuvFrame != nil {
		d.yuvFrame.Free()
		d.yuvFrame = nil
	}
	if d.pkt != nil {
		d.pkt.Free()
		d.pkt = nil
	}
	if d.codecCtx != nil {
		d.codecCtx.Free()
		d.codecCtx = nil
	}
}

// Decode submits one H.264 access unit (a slice of bare NAL units, without
// start codes) and returns a decoded BGR24 frame: contiguous bytes, 3 per
// pixel, row-major, no stride padding.
//
// Returns (nil, 0, 0, nil) when the decoder has consumed the input but is
// still waiting for more packets to produce a frame (libavcodec EAGAIN/EOF).
//
// The returned slice is owned by the decoder and remains valid only until
// the next Decode() call.
func (d *Decoder) Decode(au unit.PayloadH264, pts time.Duration) ([]byte, int, int, error) {
	annexB, isIDR := auToAnnexB(au)

	if isIDR && !d.extradataPrepended && len(d.Extradata) > 0 {
		merged := make([]byte, 0, len(d.Extradata)+len(annexB))
		merged = append(merged, d.Extradata...)
		merged = append(merged, annexB...)
		annexB = merged
		d.extradataPrepended = true
	}

	if err := d.pkt.FromData(annexB); err != nil {
		return nil, 0, 0, fmt.Errorf("packet from data: %w", err)
	}
	d.pkt.SetPts(int64(pts))

	if err := d.codecCtx.SendPacket(d.pkt); err != nil {
		return nil, 0, 0, fmt.Errorf("send packet: %w", err)
	}

	if err := d.codecCtx.ReceiveFrame(d.yuvFrame); err != nil {
		if errors.Is(err, astiav.ErrEagain) || errors.Is(err, astiav.ErrEof) {
			return nil, 0, 0, nil
		}
		return nil, 0, 0, fmt.Errorf("receive frame: %w", err)
	}
	defer d.yuvFrame.Unref()

	width := d.yuvFrame.Width()
	height := d.yuvFrame.Height()

	if d.swsCtx == nil {
		ctx, err := astiav.CreateSoftwareScaleContext(
			width, height, d.yuvFrame.PixelFormat(),
			width, height, astiav.PixelFormatBgr24,
			astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
		)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("create swscale: %w", err)
		}
		d.swsCtx = ctx

		d.bgrFrame.SetWidth(width)
		d.bgrFrame.SetHeight(height)
		d.bgrFrame.SetPixelFormat(astiav.PixelFormatBgr24)
		if err := d.bgrFrame.AllocBuffer(1); err != nil {
			return nil, 0, 0, fmt.Errorf("alloc BGR buffer: %w", err)
		}
	}

	if err := d.swsCtx.ScaleFrame(d.yuvFrame, d.bgrFrame); err != nil {
		return nil, 0, 0, fmt.Errorf("scale: %w", err)
	}

	// Bytes(1) returns a contiguous BGR24 image with no stride padding,
	// allocated each call by the library; from the decoder's perspective
	// this is the per-call output buffer the caller may use until the
	// next Decode().
	bgr, err := d.bgrFrame.Data().Bytes(1)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read BGR bytes: %w", err)
	}

	return bgr, width, height, nil
}

// auToAnnexB rewrites a slice of NAL units (without start codes) into a
// single Annex-B byte slice and reports whether the AU contains an IDR.
func auToAnnexB(au unit.PayloadH264) ([]byte, bool) {
	size := 0
	for _, nalu := range au {
		size += len(annexBStartCode) + len(nalu)
	}
	out := make([]byte, 0, size)
	isIDR := false
	for _, nalu := range au {
		if len(nalu) > 0 && nalu[0]&0x1F == h264NALUTypeIDR {
			isIDR = true
		}
		out = append(out, annexBStartCode...)
		out = append(out, nalu...)
	}
	return out, isIDR
}
