//go:build analytics && lpr

package lpr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bluenviron/mediamtx/internal/analytics"
)

// Module is the analytics.Module implementation backed by a PlateCore
// Engine. One Module per camera; never shared between Readers.
type Module struct {
	cfg    Config
	engine *Engine
	cam    string
}

// Name returns the module identifier used in PathAnalyticsModule.Name.
func (*Module) Name() string { return "lpr" }

// Configure parses raw and starts a fresh PlateCore engine. A failure
// from NewEngine — typically a missing or unreachable license proxy —
// is returned as-is so the upstream Reader can decide whether to
// downgrade the path to "ready without analytics".
func (m *Module) Configure(cameraID string, raw json.RawMessage) error {
	cfg := Config{
		PlateType:   "auto",
		Mode:        "leave",
		MinHits:     3,
		RepeatEvent: 5,
		TTL:         60,
		Stream:      1,
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("lpr: invalid config: %w", err)
		}
	}

	plateType, err := parsePlateType(cfg.PlateType)
	if err != nil {
		return err
	}
	mode, err := parseMode(cfg.Mode)
	if err != nil {
		return err
	}

	engine, err := NewEngine(EngineConfig{
		ROIRect:      cfg.ROIRect,
		PlateSizeMin: cfg.PlateSizeMin,
		PlateSizeMax: cfg.PlateSizeMax,
		MinHits:      cfg.MinHits,
		Mode:         mode,
		RepeatEvent:  cfg.RepeatEvent,
		TTL:          cfg.TTL,
		Stream:       cfg.Stream,
		PlateType:    plateType,
	})
	if err != nil {
		return err
	}

	m.cfg = cfg
	m.engine = engine
	m.cam = cameraID
	return nil
}

// Process feeds the BGR24 frame to PlateCore, then converts every
// recognised plate into an analytics.Event with normalised bbox and
// (optionally) a JPEG thumbnail.
func (m *Module) Process(frame *analytics.Frame) ([]analytics.Event, error) {
	if m.engine == nil {
		return nil, nil
	}

	// PlateCore's tracker only uses the timestamp as a monotonic key;
	// microseconds give us enough resolution to keep monotonicity even
	// when multiple frames arrive in the same millisecond.
	ts := int(frame.Timestamp.UnixMicro())

	results, err := m.engine.Process(
		frame.Data, frame.Width, frame.Height, ts, m.cfg.Crop, m.cfg.Draw,
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	fw := float32(frame.Width)
	fh := float32(frame.Height)

	events := make([]analytics.Event, 0, len(results))
	for _, r := range results {
		events = append(events, analytics.Event{
			ModuleName: "lpr",
			DetectedAt: frame.Timestamp,
			CameraID:   frame.CameraID,
			TrackerID:  r.TrackerID,
			Payload: EventPayload{
				Plate:     r.Plate,
				PlateFull: r.PlateFull,
				Region:    r.Region,
				Country:   r.Country,
				Score:     r.Score,
				BBox:      normaliseBBox(r.BBox, fw, fh),
				Width:     r.Width,
				Height:    r.Height,
			},
			Thumbnail: r.Thumbnail,
		})
	}

	return events, nil
}

// Close releases the underlying PlateCore engine. Safe to call twice.
func (m *Module) Close() error {
	if m.engine == nil {
		return nil
	}
	err := m.engine.Close()
	m.engine = nil
	return err
}

// normaliseBBox turns PlateCore's pixel-space [xmin,ymin,xmax,ymax]
// into [x,y,w,h] normalised to 0..1 of the source frame so consumers
// can render it independent of display dimensions.
func normaliseBBox(bbox [4]int, fw, fh float32) [4]float32 {
	if fw <= 0 || fh <= 0 {
		return [4]float32{}
	}
	x := float32(bbox[0]) / fw
	y := float32(bbox[1]) / fh
	w := float32(bbox[2]-bbox[0]) / fw
	h := float32(bbox[3]-bbox[1]) / fh
	return [4]float32{x, y, w, h}
}

func parseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "enter_once":
		return ModeEnterOnce, nil
	case "enter_interval":
		return ModeEnterInterval, nil
	case "leave", "":
		return ModeLeave, nil
	case "raw":
		return ModeRaw, nil
	default:
		return 0, fmt.Errorf("lpr: invalid mode %q", s)
	}
}

func parsePlateType(s string) (PlateType, error) {
	switch strings.ToLower(s) {
	case "auto", "":
		return PlateTypeAuto, nil
	case "wagon":
		return PlateTypeWagon, nil
	case "boat":
		return PlateTypeBoat, nil
	case "container":
		return PlateTypeContainer, nil
	default:
		return 0, fmt.Errorf("lpr: invalid plate_type %q", s)
	}
}

func init() {
	analytics.RegisterModule("lpr", func() analytics.Module { return &Module{} })
}
