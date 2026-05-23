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

	ref := s.Put(2, 1, bgr, ts, 0)
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
	oldRef := s.Put(1, 1, []byte{1, 1, 1}, time.Now(), 0)

	// Put 3 more frames, which overwrites slot 0 (where oldRef lives).
	for i := 0; i < 3; i++ {
		s.Put(1, 1, []byte{byte(i), byte(i), byte(i)}, time.Now(), 0)
	}

	if _, ok := s.Get(oldRef); ok {
		t.Fatalf("expected oldRef to be evicted after capacity+1 Puts, got it back")
	}
}

func TestFrameStoreGetReturnsCopy(t *testing.T) {
	s := NewFrameStore(2)
	bgr := []byte{10, 20, 30}
	ref := s.Put(1, 1, bgr, time.Now(), 0)

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

	s.Put(1, 1, []byte{1, 2, 3}, time.Now(), 0)
	s.Put(1, 1, []byte{4, 5, 6}, time.Now(), 0)

	got, ok := s.Latest()
	if !ok {
		t.Fatalf("Latest must return the second frame")
	}
	if !bytes.Equal(got.Data, []byte{4, 5, 6}) {
		t.Fatalf("Latest data mismatch: %v", got.Data)
	}
}

func TestFrameStoreLookupByTSKey(t *testing.T) {
	s := NewFrameStore(4)

	ref1 := s.Put(1, 1, []byte{1, 1, 1}, time.Now(), 100)
	ref2 := s.Put(1, 1, []byte{2, 2, 2}, time.Now(), 200)

	got := s.LookupByTSKey(100)
	if got != ref1 {
		t.Fatalf("LookupByTSKey(100) = %+v, want %+v", got, ref1)
	}
	got = s.LookupByTSKey(200)
	if got != ref2 {
		t.Fatalf("LookupByTSKey(200) = %+v, want %+v", got, ref2)
	}

	// Unknown key -> zero ref.
	if got := s.LookupByTSKey(999); !got.IsZero() {
		t.Fatalf("LookupByTSKey(unknown) = %+v, want zero", got)
	}

	// tsKey == 0 must not yield a hit even when zero-keyed slots
	// exist, since 0 is reserved for "no key".
	s.Put(1, 1, []byte{3, 3, 3}, time.Now(), 0)
	if got := s.LookupByTSKey(0); !got.IsZero() {
		t.Fatalf("LookupByTSKey(0) = %+v, want zero", got)
	}
}

func TestFrameStoreEvictionClearsTSIndex(t *testing.T) {
	s := NewFrameStore(2)
	s.Put(1, 1, []byte{1}, time.Now(), 11)
	s.Put(1, 1, []byte{2}, time.Now(), 22)

	// Overwrite the slot holding tsKey=11.
	s.Put(1, 1, []byte{3}, time.Now(), 33)

	// tsKey=11's slot has been recycled — the index entry must go too.
	if got := s.LookupByTSKey(11); !got.IsZero() {
		t.Fatalf("expected tsKey=11 to be evicted, got %+v", got)
	}
	// tsKey=22 is still in its slot.
	if got := s.LookupByTSKey(22); got.IsZero() {
		t.Fatalf("expected tsKey=22 to survive, got zero ref")
	}
	// tsKey=33 was just inserted.
	if got := s.LookupByTSKey(33); got.IsZero() {
		t.Fatalf("expected tsKey=33 to resolve, got zero ref")
	}
}

func TestFrameStoreTSKeyReusePreservesLatestSlot(t *testing.T) {
	// Two Puts with the same tsKey in different slots: the older
	// slot's eviction must not delete the map entry that now points
	// at the newer slot.
	s := NewFrameStore(4)
	s.Put(1, 1, []byte{1}, time.Now(), 7) // slot id=1
	// Fill the ring so the next Put with tsKey=7 lands in a new slot.
	s.Put(1, 1, []byte{2}, time.Now(), 8)
	s.Put(1, 1, []byte{3}, time.Now(), 9)
	newRef := s.Put(1, 1, []byte{4}, time.Now(), 7) // slot id=4 (4 % 4 == 0)

	// LookupByTSKey(7) must point at the newer slot.
	got := s.LookupByTSKey(7)
	if got != newRef {
		t.Fatalf("LookupByTSKey(7) = %+v, want newer %+v", got, newRef)
	}

	// Force eviction of the old slot id=1 by filling another full lap.
	s.Put(1, 1, []byte{5}, time.Now(), 10) // slot id=5, evicts slot id=1
	// Map entry for tsKey=7 must still be present — id=1 is gone but
	// id=4 (also tsKey=7) is still live.
	if got := s.LookupByTSKey(7); got != newRef {
		t.Fatalf("LookupByTSKey(7) after old-slot eviction = %+v, want %+v",
			got, newRef)
	}
}

func TestFrameStoreConcurrentReaders(t *testing.T) {
	s := NewFrameStore(8)

	// Pre-populate so readers always see a valid latest.
	s.Put(2, 2, make([]byte, 12), time.Now(), 0)

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
				s.Put(2, 2, make([]byte, 12), time.Now(), 0)
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
