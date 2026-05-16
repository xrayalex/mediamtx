//go:build analytics

package analytics

import (
	"sync"
	"time"
)

// defaultFrameBufferSize is the per-camera ring depth used when the
// path config does not override it. Sized for ~6.4s of buffered
// frames at the default 10 fps analytics tap.
//
// The window has to cover the PlateCore tracker's accumulation
// horizon for non-LEAVE modes: a result's bbox/timestamp can refer
// to a source frame that is min_hits frames old for ENTER_ONCE, or
// up to repeat_event seconds old for ENTER_INTERVAL. Defaults
// (min_hits=3, repeat_event=5s) fit comfortably inside 6.4s; raise
// frame_buffer_size in the path config when running with larger
// intervals.
const defaultFrameBufferSize = 64

// FrameRef is a stable, copyable handle to a frame that lived in a
// FrameStore at one point. The zero value means "no reference".
//
// IDs are monotonically issued per-store starting at 1, so an ID of 0
// reliably indicates an unset ref.
type FrameRef struct {
	ID uint64
}

// IsZero reports whether ref carries no frame.
func (r FrameRef) IsZero() bool { return r.ID == 0 }

// StoredFrame is a snapshot copy of a buffered frame.
//
// Data is owned by the caller — the FrameStore never holds the slice
// returned from Get() after the call returns. This isolates readers
// (publish workers) from the writer (decode goroutine), which is free
// to overwrite the underlying slot at any time.
type StoredFrame struct {
	ID        uint64
	Timestamp time.Time
	Width     int
	Height    int
	Data      []byte
}

// FrameStore is a per-camera ring buffer of decoded BGR24 frames.
//
// Single writer (the reader's decode goroutine) calls Put on every
// successful decode; any number of readers may call Get from any
// goroutine. Slot data is overwritten in place after capacity frames
// have been written; consumers detect this by comparing the FrameRef
// ID with the slot's current ID.
type FrameStore struct {
	mu       sync.RWMutex
	slots    []frameSlot
	capacity int
	nextID   uint64
}

type frameSlot struct {
	id        uint64 // 0 = never written
	timestamp time.Time
	width     int
	height    int
	data      []byte // pre-allocated when first Put hits, reused thereafter
}

// NewFrameStore returns a FrameStore that retains the most recent
// capacity frames. capacity must be > 0; callers should validate.
func NewFrameStore(capacity int) *FrameStore {
	if capacity <= 0 {
		capacity = 1
	}
	return &FrameStore{
		slots:    make([]frameSlot, capacity),
		capacity: capacity,
	}
}

// Capacity returns the configured slot count.
func (s *FrameStore) Capacity() int { return s.capacity }

// Put copies bgr into the next slot (round-robin) and returns a
// FrameRef that callers can use to retrieve the same frame later via
// Get. The returned ref is always non-zero on success.
//
// The bgr slice must outlive only this call: Put copies the bytes.
// Subsequent Decode() calls in the caller's goroutine may overwrite
// the source slice without affecting buffered frames.
func (s *FrameStore) Put(width, height int, bgr []byte, ts time.Time) FrameRef {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	slot := &s.slots[id%uint64(s.capacity)]

	if cap(slot.data) < len(bgr) {
		slot.data = make([]byte, len(bgr))
	} else {
		slot.data = slot.data[:len(bgr)]
	}
	copy(slot.data, bgr)

	slot.id = id
	slot.timestamp = ts
	slot.width = width
	slot.height = height

	return FrameRef{ID: id}
}

// Get returns a snapshot copy of the frame named by ref, or
// (nil, false) if the slot has already been overwritten or the ref is
// the zero value.
//
// The returned StoredFrame.Data is freshly allocated; callers may
// retain it as long as they like.
func (s *FrameStore) Get(ref FrameRef) (*StoredFrame, bool) {
	if ref.IsZero() {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	slot := &s.slots[ref.ID%uint64(s.capacity)]
	if slot.id != ref.ID {
		return nil, false
	}

	data := make([]byte, len(slot.data))
	copy(data, slot.data)

	return &StoredFrame{
		ID:        slot.id,
		Timestamp: slot.timestamp,
		Width:     slot.width,
		Height:    slot.height,
		Data:      data,
	}, true
}

// Latest returns the most recently buffered frame, or (nil, false) if
// no frames have been Put yet.
func (s *FrameStore) Latest() (*StoredFrame, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.nextID == 0 {
		return nil, false
	}
	slot := &s.slots[s.nextID%uint64(s.capacity)]
	data := make([]byte, len(slot.data))
	copy(data, slot.data)
	return &StoredFrame{
		ID:        slot.id,
		Timestamp: slot.timestamp,
		Width:     slot.width,
		Height:    slot.height,
		Data:      data,
	}, true
}
