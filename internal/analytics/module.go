//go:build analytics

// Package analytics contains the analytics subsystem: a per-path stream
// reader that decodes H.264 frames once and fans them out to one or more
// modules (LPR, motion, etc.) sharing the decoded BGR24 buffer.
package analytics

import (
	"encoding/json"
	"time"
)

// Frame is a decoded video frame handed to modules during fan-out.
//
// Data is BGR24-packed (3 bytes per pixel, row-major, no padding) and is
// owned by the decoder: it is valid only for the duration of Module.Process
// and may be overwritten on the next Decode() call. Modules that need to
// retain the frame must copy it.
type Frame struct {
	Data      []byte
	Width     int
	Height    int
	Timestamp time.Time
	CameraID  string

	// Ref identifies the frame in the per-camera FrameStore. Modules
	// may copy this value into Event.FrameRef so a late publisher
	// goroutine can pull the original BGR bytes and encode a thumbnail
	// off the decode goroutine.
	//
	// The zero value means the reader did not maintain a FrameStore.
	Ref FrameRef
	// Store is the FrameStore that issued Ref. nil iff Ref.IsZero().
	Store *FrameStore
}

// Event is the result of a module recognising something in a Frame.
//
// Payload is module-specific (typically a struct that will be JSON-marshaled
// by the publisher). Thumbnail, when present, is a JPEG-encoded image
// uploaded to object storage; the publisher receives a key and the bytes
// can be discarded after the call returns.
type Event struct {
	ModuleName string
	DetectedAt time.Time
	CameraID   string
	TrackerID  int
	Payload    any

	// Thumbnail is an already-encoded JPEG to upload as-is. Modules
	// only set this when they cannot defer the work to the publisher
	// (e.g. LPR in MODE_LEAVE uses platecore_to_jpeg because the
	// best-frame buffer lives in PlateCore and is gone after the
	// release call). The typical, preferred path leaves Thumbnail
	// nil and lets the publisher worker build the JPEG from a ring-
	// buffered source frame.
	Thumbnail []byte

	// FrameRef, when non-zero, points to a buffered frame from
	// Frame.Store. The async publisher fetches that frame, applies
	// Overlays / CropRegion, and encodes the JPEG off the decode
	// goroutine. Stale refs (frame already evicted) are logged and
	// the event is published without a thumbnail.
	FrameRef FrameRef
	// FrameStore mirrors Frame.Store so the publisher can resolve
	// FrameRef without separately threading the store through. The
	// Reader auto-fills this when a module sets FrameRef.
	FrameStore *FrameStore

	// Overlays are drawn on the fetched frame in source-coordinate
	// space before any cropping. Empty = no overlay. Used by modules
	// to annotate detected regions (LPR plate bbox, motion area,
	// etc.) without doing the JPEG work themselves.
	Overlays []Overlay
	// CropRegion, when non-nil, makes the publisher encode only the
	// rectangular subregion of the source frame (pixel-space). Set
	// from modules like LPR that want a tight crop around the
	// detection instead of the full camera view.
	CropRegion *[4]int
}

// Overlay is a graphic annotation rendered on a source frame before
// JPEG encoding. The zero Color falls back to green; non-positive
// Thickness falls back to 2 pixels.
type Overlay struct {
	BBox      [4]int   // pixel-space [xmin, ymin, xmax, ymax]
	Color     [3]uint8 // RGB
	Thickness int      // outline width, in pixels
}

// Module is a single analytics processor (LPR, motion detector, ...).
//
// Process is synchronous and called from the reader's fan-out goroutine.
// The Frame.Data slice is borrowed from the decoder and must not be
// retained beyond the call; modules that need asynchronous processing
// must copy the bytes they need.
type Module interface {
	Name() string
	Configure(cameraID string, config json.RawMessage) error
	Process(frame *Frame) ([]Event, error)
	Close() error
}
