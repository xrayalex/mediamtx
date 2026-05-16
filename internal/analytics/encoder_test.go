//go:build analytics

package analytics

import (
	"bytes"
	"image/jpeg"
	"testing"
	"time"
)

// helper: build a black BGR24 frame.
func blankBGR(w, h int) []byte { return make([]byte, w*h*3) }

func TestDrawBBoxBGRBasic(t *testing.T) {
	w, h := 10, 8
	data := blankBGR(w, h)
	drawBBoxBGR(data, w, h, [4]int{2, 1, 7, 5}, [3]uint8{255, 0, 0}, 1)

	// (2,1) should be red: BGR bytes = {0, 0, 255}
	i := (1*w + 2) * 3
	if data[i] != 0 || data[i+1] != 0 || data[i+2] != 255 {
		t.Fatalf("top-left corner not red: got %v", data[i:i+3])
	}
	// (7,5) should also be red (bottom-right corner)
	j := (5*w + 7) * 3
	if data[j+2] != 255 {
		t.Fatalf("bottom-right corner not red: got %v", data[j:j+3])
	}
	// (4,3) is INSIDE the bbox, untouched → still black.
	k := (3*w + 4) * 3
	if data[k] != 0 || data[k+1] != 0 || data[k+2] != 0 {
		t.Fatalf("interior pixel was painted: got %v", data[k:k+3])
	}
}

func TestDrawBBoxBGRDefaults(t *testing.T) {
	w, h := 8, 8
	data := blankBGR(w, h)
	// Zero color → defaultBBoxColor (green); thickness 0 → default 2.
	drawBBoxBGR(data, w, h, [4]int{1, 1, 6, 6}, [3]uint8{}, 0)

	// (1,1) top-left, should be green BGR={0,255,0}
	i := (1*w + 1) * 3
	if data[i+1] != 255 {
		t.Fatalf("expected green outline, got %v", data[i:i+3])
	}
	// With thickness 2, (2,1) is also painted.
	j := (1*w + 2) * 3
	if data[j+1] != 255 {
		t.Fatalf("expected thickness>=2, (2,1) not green: %v", data[j:j+3])
	}
}

func TestDrawBBoxBGRClipsOutOfBounds(t *testing.T) {
	w, h := 6, 6
	data := blankBGR(w, h)
	// bbox extends past the frame; must not panic and must clamp.
	// Red in RGB → BGR bytes {0, 0, 255} so data[2] (R channel) is the marker.
	drawBBoxBGR(data, w, h, [4]int{-3, -3, 100, 100}, [3]uint8{255, 0, 0}, 1)

	// At least the (0,0) corner should be painted.
	if data[2] != 255 {
		t.Fatalf("clamped bbox did not paint (0,0): got %v", data[0:3])
	}
}

func TestDrawBBoxBGRDegenerate(t *testing.T) {
	w, h := 6, 6
	data := blankBGR(w, h)
	// Zero-area bbox: no-op.
	drawBBoxBGR(data, w, h, [4]int{3, 3, 3, 3}, [3]uint8{255, 0, 0}, 1)

	for _, b := range data {
		if b != 0 {
			t.Fatalf("degenerate bbox painted pixels: %v", data)
		}
	}
}

func TestCropBGRBasic(t *testing.T) {
	w, h := 4, 3
	src := []byte{
		// row 0
		1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4,
		// row 1
		5, 5, 5, 6, 6, 6, 7, 7, 7, 8, 8, 8,
		// row 2
		9, 9, 9, 10, 10, 10, 11, 11, 11, 12, 12, 12,
	}
	f := &StoredFrame{Width: w, Height: h, Data: src, Timestamp: time.Unix(1, 0), ID: 7}
	out := cropBGR(f, [4]int{1, 1, 3, 3})
	if out == nil {
		t.Fatal("cropBGR returned nil for valid region")
	}
	if out.Width != 2 || out.Height != 2 {
		t.Fatalf("crop dims: want 2x2, got %dx%d", out.Width, out.Height)
	}
	want := []byte{6, 6, 6, 7, 7, 7, 10, 10, 10, 11, 11, 11}
	if !bytes.Equal(out.Data, want) {
		t.Fatalf("crop data mismatch:\n got  %v\n want %v", out.Data, want)
	}
	if out.ID != 7 {
		t.Fatalf("cropBGR should propagate source ID")
	}
}

func TestCropBGRClampsAndRejectsEmpty(t *testing.T) {
	w, h := 4, 3
	f := &StoredFrame{Width: w, Height: h, Data: blankBGR(w, h)}

	// Region entirely outside → nil.
	if cropBGR(f, [4]int{10, 10, 20, 20}) != nil {
		t.Fatal("crop entirely outside should be nil")
	}
	// Zero-area.
	if cropBGR(f, [4]int{2, 2, 2, 2}) != nil {
		t.Fatal("zero-area crop should be nil")
	}
	// Partial clip — should succeed with clamped dims.
	out := cropBGR(f, [4]int{-1, -1, 2, 2})
	if out == nil || out.Width != 2 || out.Height != 2 {
		t.Fatalf("partial clip should give 2x2, got %+v", out)
	}
}

func TestEncodeBGRJPEGRoundTrip(t *testing.T) {
	w, h := 32, 24
	data := make([]byte, w*h*3)
	for i := 0; i < len(data); i += 3 {
		data[i] = 50    // B
		data[i+1] = 100 // G
		data[i+2] = 200 // R
	}
	f := &StoredFrame{Width: w, Height: h, Data: data}
	jpg, err := encodeBGRJPEG(f)
	if err != nil {
		t.Fatalf("encodeBGRJPEG: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatalf("jpeg decode round-trip: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		t.Fatalf("round-trip dims: %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	// JPEG is lossy — check colour roughly.
	r, g, bb, _ := img.At(16, 12).RGBA()
	if r>>8 < 150 || g>>8 > 150 || bb>>8 > 100 {
		t.Fatalf("expected reddish pixel, got R=%d G=%d B=%d", r>>8, g>>8, bb>>8)
	}
}
