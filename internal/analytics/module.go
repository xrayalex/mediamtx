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
	// that overlay bbox/text (LPR with draw=1) build it during
	// Process; modules that just want a plain frame snapshot leave
	// it nil and set FrameRef instead.
	Thumbnail []byte

	// FrameRef, when non-zero, points to a buffered frame from
	// Frame.Store. The async publisher uses it to encode a default
	// JPEG (without overlays) off the decode goroutine if Thumbnail
	// is empty. Stale refs (frame already evicted) are logged and
	// the event is published without a thumbnail.
	FrameRef FrameRef
	// FrameStore mirrors Frame.Store so the publisher can resolve
	// FrameRef without separately threading the store through.
	FrameStore *FrameStore
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
