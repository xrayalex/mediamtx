//go:build analytics

package analytics

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

// defaultJPEGQuality matches what consumers expect for analytics
// thumbnails: visibly clean plates / object crops, but small enough
// that publishing many per second does not saturate the upload path.
const defaultJPEGQuality = 80

// bgrImage adapts a packed BGR24 byte slice to image.Image without
// allocating an intermediate RGBA buffer. JPEG encoding only needs
// per-pixel reads through At(), so this is enough.
type bgrImage struct {
	data   []byte
	width  int
	height int
}

func (b *bgrImage) ColorModel() color.Model { return color.RGBAModel }

func (b *bgrImage) Bounds() image.Rectangle {
	return image.Rect(0, 0, b.width, b.height)
}

func (b *bgrImage) At(x, y int) color.Color {
	i := (y*b.width + x) * 3
	return color.RGBA{
		R: b.data[i+2],
		G: b.data[i+1],
		B: b.data[i],
		A: 255,
	}
}

// encodeBGRJPEG produces a JPEG byte slice from a StoredFrame.
//
// Used by the publisher worker when an Event carries a FrameRef but
// no pre-built Thumbnail; modules that overlay bbox/text on top of
// the image must build their own JPEG (e.g. LPR with draw=1 via
// platecore_to_jpeg).
func encodeBGRJPEG(f *StoredFrame) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("encode jpeg: nil frame")
	}
	if f.Width <= 0 || f.Height <= 0 {
		return nil, fmt.Errorf("encode jpeg: invalid dimensions %dx%d", f.Width, f.Height)
	}
	if len(f.Data) < f.Width*f.Height*3 {
		return nil, fmt.Errorf("encode jpeg: data too short (%d, need %d)",
			len(f.Data), f.Width*f.Height*3)
	}

	img := &bgrImage{data: f.Data, width: f.Width, height: f.Height}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: defaultJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
