//go:build analytics

package analytics

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestFrameStorePutGet(t *testing.T) {
	s := NewFrameStore(4)
	bgr := []byte{1, 2, 3, 4, 5, 6}
	ts := time.Now()

	ref := s.Put(2, 1, bgr, ts)
	if ref.IsZero() {
		t.Fatalf("Put returned zero ref")
	}

	got, ok := s.Get(ref)
	if !ok {
		t.Fatalf("Get(ref) reported eviction immediately after Put")
	}
	if !bytes.Equal(got.Data, bgr) {
		t.Fatalf("Get data mismatch: got %v want %v", got.Data, bgr)
	}
	if got.Width != 2 || got.Height != 1 {
		t.Fatalf("dims mismatch: %dx%d", got.Width, got.Height)
	}
	if !got.Timestamp.Equal(ts) {
		t.Fatalf("timestamp mismatch")
	}
}

func TestFrameStoreEviction(t *testing.T) {
	s := NewFrameStore(3)
	oldRef := s.Put(1, 1, []byte{1, 1, 1}, time.Now())

	// Put 3 more frames, which overwrites slot 0 (where oldRef lives).
	for i := 0; i < 3; i++ {
		s.Put(1, 1, []byte{byte(i), byte(i), byte(i)}, time.Now())
	}

	if _, ok := s.Get(oldRef); ok {
		t.Fatalf("expected oldRef to be evicted after capacity+1 Puts, got it back")
	}
}

func TestFrameStoreGetReturnsCopy(t *testing.T) {
	s := NewFrameStore(2)
	bgr := []byte{10, 20, 30}
	ref := s.Put(1, 1, bgr, time.Now())

	got, ok := s.Get(ref)
	if !ok {
		t.Fatalf("Get failed unexpectedly")
	}

	// Mutate caller's copy — must NOT affect the slot.
	got.Data[0] = 99

	again, ok := s.Get(ref)
	if !ok {
		t.Fatalf("Get failed on second call")
	}
	if again.Data[0] != 10 {
		t.Fatalf("Get returned shared buffer (got %d, want 10)", again.Data[0])
	}
}

func TestFrameStoreZeroRef(t *testing.T) {
	s := NewFrameStore(2)
	if _, ok := s.Get(FrameRef{}); ok {
		t.Fatalf("Get on zero ref must return ok=false")
	}
}

func TestFrameStoreLatest(t *testing.T) {
	s := NewFrameStore(4)
	if _, ok := s.Latest(); ok {
		t.Fatalf("Latest on empty store must return ok=false")
	}

	s.Put(1, 1, []byte{1, 2, 3}, time.Now())
	s.Put(1, 1, []byte{4, 5, 6}, time.Now())

	got, ok := s.Latest()
	if !ok {
		t.Fatalf("Latest must return the second frame")
	}
	if !bytes.Equal(got.Data, []byte{4, 5, 6}) {
		t.Fatalf("Latest data mismatch: %v", got.Data)
	}
}

func TestFrameStoreConcurrentReaders(t *testing.T) {
	s := NewFrameStore(8)

	// Pre-populate so readers always see a valid latest.
	s.Put(2, 2, make([]byte, 12), time.Now())

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.Put(2, 2, make([]byte, 12), time.Now())
			}
		}
	}()

	// Multiple reader goroutines hammering Latest.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if got, ok := s.Latest(); ok {
					if len(got.Data) != 12 {
						t.Errorf("unexpected data length %d", len(got.Data))
						return
					}
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
