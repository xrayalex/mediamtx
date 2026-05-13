//go:build analytics

package analytics

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	rtspformat "github.com/bluenviron/gortsplib/v5/pkg/format"

	"github.com/bluenviron/mediamtx/internal/logger"
	"github.com/bluenviron/mediamtx/internal/stream"
	"github.com/bluenviron/mediamtx/internal/unit"
)

// frameJob is a single H.264 access unit handed from the stream callback
// to the decoder goroutine.
type frameJob struct {
	au    unit.PayloadH264
	pts   time.Duration
	ntp   time.Time
	isIDR bool
}

// Reader subscribes to a stream.Stream, decodes H.264 frames once per
// channel and fans the BGR result out to a list of modules.
//
// The fan-out is synchronous: the decoder waits for every module's
// Process() to return before reading the next packet, so all modules
// share a stable view of the decoder's reusable BGR buffer.
type Reader struct {
	Stream          *stream.Stream
	CameraID        string
	Modules         []Module
	FPS             int
	Pub             *Publisher
	Parent          logger.Writer
	FrameBufferSize int

	media        *description.Media
	format       *rtspformat.H264
	decoder      *Decoder
	streamReader *stream.Reader
	store        *FrameStore

	jobs chan *frameJob

	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Log implements logger.Writer.
func (r *Reader) Log(level logger.Level, format string, args ...any) {
	if r.Parent != nil {
		r.Parent.Log(level, format, args...)
	}
}

// Start subscribes to the stream and launches the decode/fan-out goroutine.
//
// Returns an error if the stream has no usable H.264 video track or the
// decoder cannot be initialised.
func (r *Reader) Start() error {
	if r.FPS <= 0 {
		return errors.New("analytics: FPS must be > 0")
	}
	if r.Stream == nil {
		return errors.New("analytics: stream is nil")
	}
	if len(r.Modules) == 0 {
		return errors.New("analytics: no modules configured")
	}

	if err := r.findH264Track(); err != nil {
		return err
	}

	sps, pps := r.format.SafeParams()
	r.decoder = &Decoder{Extradata: BuildExtradata(sps, pps)}
	if err := r.decoder.Initialize(); err != nil {
		return fmt.Errorf("init decoder: %w", err)
	}

	bufSize := r.FrameBufferSize
	if bufSize <= 0 {
		bufSize = defaultFrameBufferSize
	}
	r.store = NewFrameStore(bufSize)

	r.jobs = make(chan *frameJob, 10)
	r.closed = make(chan struct{})

	r.streamReader = &stream.Reader{
		SkipBytesSent: true,
		Parent:        r,
	}

	throttle := newFPSThrottle(r.FPS)

	r.streamReader.OnData(r.media, r.format, func(u *unit.Unit) error {
		if u.NilPayload() {
			return nil
		}
		au, ok := u.Payload.(unit.PayloadH264)
		if !ok {
			return nil
		}

		isIDR := false
		for _, nalu := range au {
			if len(nalu) > 0 && nalu[0]&0x1F == h264NALUTypeIDR {
				isIDR = true
				break
			}
		}

		ntp := u.NTP
		if ntp.IsZero() {
			ntp = time.Now()
		}

		// Keyframes always pass; the decoder needs every IDR to keep
		// its reference state valid for subsequent P-frames.
		if !throttle.shouldEmit(ntp, isIDR) {
			return nil
		}

		// Copy the AU: the underlying NAL byte slices are owned by the
		// stream and may be recycled after the callback returns.
		auCopy := make(unit.PayloadH264, len(au))
		for i, nalu := range au {
			cp := make([]byte, len(nalu))
			copy(cp, nalu)
			auCopy[i] = cp
		}

		job := &frameJob{
			au:    auCopy,
			pts:   time.Duration(u.PTS),
			ntp:   ntp,
			isIDR: isIDR,
		}

		r.pushJob(job)
		return nil
	})

	r.Stream.AddReader(r.streamReader)

	r.wg.Add(1)
	go r.runDecode()

	r.Log(logger.Info, "analytics reader started for %s (modules=%d, fps=%d)",
		r.CameraID, len(r.Modules), r.FPS)

	return nil
}

// Close detaches the reader from the stream and releases resources.
func (r *Reader) Close() {
	r.closeOnce.Do(func() {
		close(r.closed)
		if r.streamReader != nil && r.Stream != nil {
			r.Stream.RemoveReader(r.streamReader)
		}
		r.wg.Wait()
		if r.decoder != nil {
			r.decoder.Close()
			r.decoder = nil
		}
		for _, m := range r.Modules {
			if err := m.Close(); err != nil {
				r.Log(logger.Warn, "module %q close: %v", m.Name(), err)
			}
		}
		r.Log(logger.Info, "analytics reader stopped for %s", r.CameraID)
	})
}

// findH264Track locates a video media containing an H.264 format and
// stores both for later subscription.
func (r *Reader) findH264Track() error {
	for _, medi := range r.Stream.Desc.Medias {
		if medi.Type != description.MediaTypeVideo {
			continue
		}
		for _, forma := range medi.Formats {
			if h, ok := forma.(*rtspformat.H264); ok {
				r.media = medi
				r.format = h
				return nil
			}
		}
	}
	return errors.New("analytics: no H.264 video track in stream")
}

// pushJob sends a job to the decoder goroutine, dropping the oldest
// queued job when the channel is full. Single-producer (the stream
// callback runs serialised), so the drop sequence is race-free.
func (r *Reader) pushJob(job *frameJob) {
	select {
	case r.jobs <- job:
		return
	default:
	}
	select {
	case <-r.jobs:
		r.Log(logger.Debug, "analytics dropped a queued frame for %s", r.CameraID)
	default:
	}
	select {
	case r.jobs <- job:
	case <-r.closed:
	}
}

// runDecode is the single goroutine that owns the decoder and fans
// decoded frames out to all modules synchronously.
func (r *Reader) runDecode() {
	defer r.wg.Done()

	for {
		select {
		case <-r.closed:
			return
		case <-r.streamReader.Error():
			return
		case job := <-r.jobs:
			r.handleJob(job)
		}
	}
}

func (r *Reader) handleJob(job *frameJob) {
	bgr, width, height, err := r.decoder.Decode(job.au, job.pts)
	if err != nil {
		r.Log(logger.Debug, "analytics decode error for %s: %v", r.CameraID, err)
		return
	}
	if bgr == nil {
		return
	}

	// Snapshot the frame into the per-camera ring so async publish
	// workers can fetch the BGR bytes after Module.Process has long
	// returned and the decoder has reused its output buffer.
	ref := r.store.Put(width, height, bgr, job.ntp)

	frame := &Frame{
		Data:      bgr,
		Width:     width,
		Height:    height,
		Timestamp: job.ntp,
		CameraID:  r.CameraID,
		Ref:       ref,
		Store:     r.store,
	}

	for _, m := range r.Modules {
		events, err := m.Process(frame)
		if err != nil {
			r.Log(logger.Warn, "analytics module %q error for %s: %v",
				m.Name(), r.CameraID, err)
			continue
		}
		if len(events) == 0 || r.Pub == nil {
			continue
		}
		for i := range events {
			// Auto-attach the store so modules only need to set
			// FrameRef; the publisher resolves both together.
			if !events[i].FrameRef.IsZero() && events[i].FrameStore == nil {
				events[i].FrameStore = r.store
			}
			if !r.Pub.Enqueue(&events[i]) {
				// Enqueue logs its own warning on first/Nth drop; we
				// keep going so that other modules still publish.
				continue
			}
		}
	}
}
