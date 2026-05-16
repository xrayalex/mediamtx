//go:build analytics

package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/nats-io/nats.go"

	"github.com/bluenviron/mediamtx/internal/logger"
)

// Publisher delivers analytics Events to NATS JetStream and uploads
// thumbnails to MinIO.
//
// One Publisher is shared by every camera path. Initialize() is
// best-effort: if NATS or MinIO are unreachable the call returns an
// error and the caller is expected to leave Pub == nil so analytics
// stays disabled until the operator restarts the process.
//
// All publishing is asynchronous: callers hand events to Enqueue
// (non-blocking) and a small pool of worker goroutines does the
// MinIO upload + NATS publish. This keeps the decode goroutine free
// from the multi-millisecond network round-trips of each event.
type Publisher struct {
	Parent logger.Writer

	natsURL     string
	natsUser    string
	natsPass    string
	minioEnd    string
	minioKey    string
	minioSecret string
	minioBucket string
	minioSSL    bool

	queue   chan publishJob
	workers sync.WaitGroup
	closed  chan struct{}

	mu sync.Mutex
	nc *nats.Conn
	js nats.JetStreamContext
	mc *minio.Client

	droppedEnqueue atomic.Uint64
	droppedEvicted atomic.Uint64
}

// thumbnailUploadTimeout caps how long we wait on a MinIO PutObject
// before giving up: thumbnails are best-effort and must not stall the
// fan-out goroutine.
const thumbnailUploadTimeout = 3 * time.Second

// defaultPublishQueueSize / defaultPublishWorkers tune the async
// pipeline. Operators can override via env vars when high event
// rates demand it.
const (
	defaultPublishQueueSize = 256
	defaultPublishWorkers   = 4
)

// publishJob is the unit handed from Enqueue to a worker.
type publishJob struct {
	event *Event
}

// eventEnvelope is the JSON shape published to JetStream.
type eventEnvelope struct {
	EventID      string `json:"event_id"`
	Module       string `json:"module"`
	DetectedAt   string `json:"detected_at"`
	CameraPath   string `json:"camera_path"`
	TrackerID    int    `json:"tracker_id"`
	Payload      any    `json:"payload"`
	ThumbnailKey string `json:"thumbnail_key,omitempty"`
}

// NewPublisher reads ANALYTICS_* env vars, connects to NATS + MinIO,
// and starts the worker pool. Returns an error if either backend is
// unreachable; callers should log and treat analytics as disabled.
func NewPublisher(parent logger.Writer) (*Publisher, error) {
	useSSL, _ := strconv.ParseBool(envOr("ANALYTICS_MINIO_USE_SSL", "false"))

	queueSize := envInt("ANALYTICS_PUBLISH_QUEUE_SIZE", defaultPublishQueueSize)
	workerCount := envInt("ANALYTICS_PUBLISH_WORKERS", defaultPublishWorkers)

	p := &Publisher{
		Parent:      parent,
		natsURL:     envOr("ANALYTICS_NATS_URL", "nats://nats:4222"),
		natsUser:    envOr("ANALYTICS_NATS_USER", ""),
		natsPass:    envOr("ANALYTICS_NATS_PASS", ""),
		minioEnd:    envOr("ANALYTICS_MINIO_ENDPOINT", "minio:9000"),
		minioKey:    envOr("ANALYTICS_MINIO_ACCESS_KEY", ""),
		minioSecret: envOr("ANALYTICS_MINIO_SECRET_KEY", ""),
		minioBucket: envOr("ANALYTICS_MINIO_BUCKET", "lpr-thumbnails"),
		minioSSL:    useSSL,
		queue:       make(chan publishJob, queueSize),
		closed:      make(chan struct{}),
	}

	if err := p.dialNATS(); err != nil {
		return nil, fmt.Errorf("connect NATS: %w", err)
	}

	if err := p.dialMinIO(); err != nil {
		p.nc.Close()
		return nil, fmt.Errorf("connect MinIO: %w", err)
	}

	for i := 0; i < workerCount; i++ {
		p.workers.Add(1)
		go p.runWorker()
	}

	p.log(logger.Info,
		"publisher connected (nats=%s, minio=%s, bucket=%s, workers=%d, queue=%d)",
		p.natsURL, p.minioEnd, p.minioBucket, workerCount, queueSize)

	return p, nil
}

func (p *Publisher) dialNATS() error {
	opts := []nats.Option{
		nats.Name("mediamtx-analytics"),
		nats.Timeout(5 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}
	if p.natsUser != "" {
		opts = append(opts, nats.UserInfo(p.natsUser, p.natsPass))
	}

	nc, err := nats.Connect(p.natsURL, opts...)
	if err != nil {
		return err
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return err
	}

	p.nc = nc
	p.js = js
	return nil
}

func (p *Publisher) dialMinIO() error {
	if p.minioKey == "" || p.minioSecret == "" {
		return errors.New("missing ANALYTICS_MINIO_ACCESS_KEY or ANALYTICS_MINIO_SECRET_KEY")
	}

	mc, err := minio.New(p.minioEnd, &minio.Options{
		Creds:  credentials.NewStaticV4(p.minioKey, p.minioSecret, ""),
		Secure: p.minioSSL,
	})
	if err != nil {
		return err
	}

	// Bucket existence check doubles as a connectivity probe.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := mc.BucketExists(ctx, p.minioBucket); err != nil {
		return fmt.Errorf("bucket %q probe: %w", p.minioBucket, err)
	}

	p.mc = mc
	return nil
}

// Close stops the worker pool, drains the queue best-effort, and
// closes the NATS connection. Safe to call multiple times; subsequent
// calls are no-ops.
//
// We deliberately do NOT close p.queue: workers exit on p.closed and
// drain any in-flight jobs in their own select, which avoids racing
// with a concurrent Enqueue (sending on a closed channel panics).
func (p *Publisher) Close() {
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return
	default:
	}
	close(p.closed)
	p.mu.Unlock()

	p.workers.Wait()

	p.mu.Lock()
	if p.nc != nil {
		p.nc.Close()
		p.nc = nil
	}
	p.mu.Unlock()
}

// Enqueue hands an event to the async worker pool. Non-blocking:
// when the queue is full the event is dropped and a counter is
// incremented so the operator can spot saturation.
//
// Returns false if the publisher is closed or the queue was full.
func (p *Publisher) Enqueue(event *Event) bool {
	if event == nil {
		return false
	}
	select {
	case <-p.closed:
		return false
	default:
	}

	job := publishJob{event: event}
	select {
	case p.queue <- job:
		return true
	case <-p.closed:
		return false
	default:
		n := p.droppedEnqueue.Add(1)
		// Log only the first few drops so we do not flood at very
		// high event rates; operators can read the counter via
		// DroppedCounters when triaging saturation.
		if n <= 5 || n%100 == 0 {
			p.log(logger.Warn,
				"publish queue full, dropping event (total dropped=%d)", n)
		}
		return false
	}
}

// DroppedCounters returns the running totals of events lost because
// the queue was full or because the referenced frame had already
// been evicted from the FrameStore. Exposed mainly for tests and
// future /metrics integration.
func (p *Publisher) DroppedCounters() (queueFull, frameEvicted uint64) {
	return p.droppedEnqueue.Load(), p.droppedEvicted.Load()
}

func (p *Publisher) runWorker() {
	defer p.workers.Done()
	for {
		select {
		case <-p.closed:
			// Drain anything queued before Close. We do this best-
			// effort: NATS is still open until Close returns from
			// the wait, so handle() can finish publishing.
			for {
				select {
				case job := <-p.queue:
					p.handle(job)
				default:
					return
				}
			}
		case job := <-p.queue:
			p.handle(job)
		}
	}
}

func (p *Publisher) handle(job publishJob) {
	ev := job.event
	if ev == nil {
		return
	}

	// If the module did not pre-build a thumbnail but pointed us at a
	// buffered frame, render the JPEG here off the decode goroutine.
	// Apply overlays first (they live in source-coordinate space),
	// then crop, then encode. Stale refs (frame already overwritten
	// in the ring) just leave the envelope without a thumbnail_key —
	// better than blocking the worker.
	if len(ev.Thumbnail) == 0 && !ev.FrameRef.IsZero() && ev.FrameStore != nil {
		stored, ok := ev.FrameStore.Get(ev.FrameRef)
		if !ok {
			n := p.droppedEvicted.Add(1)
			if n <= 5 || n%100 == 0 {
				p.log(logger.Warn,
					"frame evicted before publish (module=%s, camera=%s, total=%d)",
					ev.ModuleName, ev.CameraID, n)
			}
		} else {
			for _, ov := range ev.Overlays {
				drawBBoxBGR(stored.Data, stored.Width, stored.Height,
					ov.BBox, ov.Color, ov.Thickness)
			}
			if ev.CropRegion != nil {
				if cropped := cropBGR(stored, *ev.CropRegion); cropped != nil {
					stored = cropped
				}
			}
			if jpg, err := encodeBGRJPEG(stored); err == nil {
				ev.Thumbnail = jpg
			} else {
				p.log(logger.Warn,
					"encode jpeg for event (module=%s, camera=%s): %v",
					ev.ModuleName, ev.CameraID, err)
			}
		}
	}

	if err := p.publishOne(ev); err != nil {
		p.log(logger.Warn,
			"publish event (module=%s, camera=%s): %v",
			ev.ModuleName, ev.CameraID, err)
	}
}

// publishOne uploads the thumbnail (if present) and publishes the
// event envelope to subject "{module}.events.{camera_path}". A failed
// thumbnail upload is logged but does not abort the publish; a failed
// JetStream publish is returned to the caller.
func (p *Publisher) publishOne(event *Event) error {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 returns an error only on entropy failure, which
		// would be catastrophic; fall back to v4 so we do not lose
		// the event over a one-off RNG hiccup.
		id = uuid.New()
	}

	envelope := eventEnvelope{
		EventID:    id.String(),
		Module:     event.ModuleName,
		DetectedAt: event.DetectedAt.UTC().Format(time.RFC3339Nano),
		CameraPath: event.CameraID,
		TrackerID:  event.TrackerID,
		Payload:    event.Payload,
	}

	if len(event.Thumbnail) > 0 {
		key := fmt.Sprintf("%s/%s.jpg", event.CameraID, envelope.EventID)
		if err := p.uploadThumbnail(key, event.Thumbnail); err != nil {
			p.log(logger.Warn, "thumbnail upload failed for event %s: %v", envelope.EventID, err)
		} else {
			envelope.ThumbnailKey = key
		}
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	subject := fmt.Sprintf("%s.events.%s", event.ModuleName, sanitizeSubjectToken(event.CameraID))

	p.mu.Lock()
	js := p.js
	p.mu.Unlock()
	if js == nil {
		return errors.New("publisher not initialised")
	}

	if _, err := js.Publish(subject, body); err != nil {
		return fmt.Errorf("jetstream publish: %w", err)
	}

	return nil
}

func (p *Publisher) uploadThumbnail(key string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailUploadTimeout)
	defer cancel()

	_, err := p.mc.PutObject(ctx, p.minioBucket, key,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/jpeg"})
	return err
}

func (p *Publisher) log(level logger.Level, format string, args ...any) {
	if p.Parent != nil {
		p.Parent.Log(level, "[analytics] "+format, args...)
	}
}

// sanitizeSubjectToken keeps a NATS subject token valid by replacing
// dots and spaces with underscores. NATS uses '.' as the separator.
func sanitizeSubjectToken(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == ' ' || c == '*' || c == '>' {
			out[i] = '_'
		} else {
			out[i] = c
		}
	}
	return string(out)
}

// envInt returns the integer value of the env var named key, or
// fallback if unset / unparseable / non-positive.
func envInt(key string, fallback int) int {
	v := envOr(key, "")
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
