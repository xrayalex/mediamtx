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

	mu sync.Mutex
	nc *nats.Conn
	js nats.JetStreamContext
	mc *minio.Client
}

// thumbnailUploadTimeout caps how long we wait on a MinIO PutObject
// before giving up: thumbnails are best-effort and must not stall the
// fan-out goroutine.
const thumbnailUploadTimeout = 3 * time.Second

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

// NewPublisher reads ANALYTICS_* env vars and connects to NATS + MinIO.
//
// Returns an error if either backend is unreachable; callers should log
// and treat analytics as disabled in that case.
func NewPublisher(parent logger.Writer) (*Publisher, error) {
	useSSL, _ := strconv.ParseBool(envOr("ANALYTICS_MINIO_USE_SSL", "false"))
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
	}

	if err := p.dialNATS(); err != nil {
		return nil, fmt.Errorf("connect NATS: %w", err)
	}

	if err := p.dialMinIO(); err != nil {
		p.nc.Close()
		return nil, fmt.Errorf("connect MinIO: %w", err)
	}

	p.log(logger.Info, "publisher connected (nats=%s, minio=%s, bucket=%s)",
		p.natsURL, p.minioEnd, p.minioBucket)

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

// Close closes the NATS connection. The MinIO client has no Close.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nc != nil {
		p.nc.Close()
		p.nc = nil
	}
}

// Publish uploads the thumbnail (if present) and publishes the event
// envelope to subject "{module}.events.{camera_path}". A failed
// thumbnail upload is logged but does not abort the publish; a failed
// JetStream publish is returned to the caller.
func (p *Publisher) Publish(event *Event) error {
	if event == nil {
		return errors.New("nil event")
	}

	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 returns an error only on entropy failure, which
		// would be catastrophic; fall back to v4 so we don't lose the
		// event over a one-off RNG hiccup.
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
