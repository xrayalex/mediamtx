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

// defaultBBoxColor is the colour used when an Overlay leaves Color
// zero. RGB green is a safe contrast on most camera scenes.
var defaultBBoxColor = [3]uint8{0, 255, 0}

const defaultBBoxThickness = 2

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
// no pre-built Thumbnail; modules that need PlateCore-style overlays
// build their own JPEG (e.g. LPR in MODE_LEAVE via platecore_to_jpeg).
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

// drawBBoxBGR paints a hollow rectangle outline onto a packed BGR24
// buffer in place. Out-of-frame pixels are silently clipped. Color
// values are in RGB and translated to BGR for the packed buffer.
//
// thickness <= 0 falls back to defaultBBoxThickness. A zero color
// (the Go zero value of [3]uint8) falls back to defaultBBoxColor.
func drawBBoxBGR(data []byte, width, height int, bbox [4]int, color [3]uint8, thickness int) {
	if width <= 0 || height <= 0 || len(data) < width*height*3 {
		return
	}
	if thickness <= 0 {
		thickness = defaultBBoxThickness
	}
	if color == ([3]uint8{}) {
		color = defaultBBoxColor
	}

	xmin := clampInt(bbox[0], 0, width-1)
	ymin := clampInt(bbox[1], 0, height-1)
	xmax := clampInt(bbox[2], 0, width-1)
	ymax := clampInt(bbox[3], 0, height-1)
	if xmax <= xmin || ymax <= ymin {
		return
	}

	r, g, b := color[0], color[1], color[2]

	setPx := func(x, y int) {
		if x < 0 || x >= width || y < 0 || y >= height {
			return
		}
		i := (y*width + x) * 3
		data[i+0] = b
		data[i+1] = g
		data[i+2] = r
	}

	// Top and bottom edges.
	for t := 0; t < thickness; t++ {
		yTop := ymin + t
		yBot := ymax - t
		if yTop > ymax || yBot < ymin {
			break
		}
		for x := xmin; x <= xmax; x++ {
			setPx(x, yTop)
			setPx(x, yBot)
		}
	}
	// Left and right edges.
	for t := 0; t < thickness; t++ {
		xLeft := xmin + t
		xRight := xmax - t
		if xLeft > xmax || xRight < xmin {
			break
		}
		for y := ymin; y <= ymax; y++ {
			setPx(xLeft, y)
			setPx(xRight, y)
		}
	}
}

// cropBGR returns a new StoredFrame that is the rectangular
// subregion of f bounded by bbox. Coordinates are pixel-space and
// silently clamped to f's dimensions; returns nil when the resulting
// rectangle is empty (degenerate input).
func cropBGR(f *StoredFrame, bbox [4]int) *StoredFrame {
	if f == nil || f.Width <= 0 || f.Height <= 0 {
		return nil
	}
	xmin := clampInt(bbox[0], 0, f.Width)
	ymin := clampInt(bbox[1], 0, f.Height)
	xmax := clampInt(bbox[2], 0, f.Width)
	ymax := clampInt(bbox[3], 0, f.Height)
	w := xmax - xmin
	h := ymax - ymin
	if w <= 0 || h <= 0 {
		return nil
	}

	out := make([]byte, w*h*3)
	rowBytes := w * 3
	for y := 0; y < h; y++ {
		srcOff := ((ymin+y)*f.Width + xmin) * 3
		dstOff := y * rowBytes
		copy(out[dstOff:dstOff+rowBytes], f.Data[srcOff:srcOff+rowBytes])
	}
	return &StoredFrame{
		ID:        f.ID,
		Timestamp: f.Timestamp,
		Width:     w,
		Height:    h,
		Data:      out,
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
