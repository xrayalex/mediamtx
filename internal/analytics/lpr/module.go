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

	// Hand PlateCore the real monotonic millisecond axis (Frame.TSKey,
	// already wrapped into 31 bits by the reader). Two consequences:
	//
	//   1. PlateCore's internal tracker sees consecutive frames spaced
	//      by ~ms instead of by "+1 frame", so the speed field on
	//      processing_result becomes meaningful (in the units the SDK
	//      uses for its own time math) rather than a per-frame delta.
	//
	//   2. PlateCore echoes the timestamp back in
	//      processing_result.timestamp; we look it up in the reader's
	//      FrameStore.tsIndex to recover the matching FrameRef. MODE_LEAVE
	//      can echo a timestamp older than the ring depth, in which case
	//      the lookup yields a zero ref — that's fine: r.Thumbnail
	//      (populated via platecore_to_jpeg from PlateCore's internal
	//      best-frame buffer) is always present in MODE_LEAVE and wins
	//      the thumbnail path anyway.
	//
	// 31-bit wrap: 0x7FFFFFFF ms ≈ 24.85 days of continuous stream. A
	// long-running reader past that horizon will see PlateCore's
	// tracker reset itself on the wrap; operational restart resets
	// streamStart and is the documented remedy.
	ts := int(frame.TSKey & 0x7FFFFFFF)

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
		// sourceRef is the ring-buffer handle of the *frame the bbox
		// refers to*, not the current submission. We derive it by
		// looking up the echoed PlateCore timestamp (which we fed in
		// as Frame.TSKey above) against the reader's FrameStore
		// secondary index. MODE_LEAVE may echo a timestamp older than
		// ring depth → LookupByTSKey returns the zero ref; publisher
		// detects that and falls back to the inline Thumbnail
		// (always populated in MODE_LEAVE via platecore_to_jpeg).
		var sourceRef analytics.FrameRef
		if frame.Store != nil {
			sourceRef = frame.Store.LookupByTSKey(uint32(r.Timestamp))
		}

		ev := analytics.Event{
			ModuleName: "lpr",
			DetectedAt: frame.Timestamp,
			CameraID:   frame.CameraID,
			TrackerID:  r.TrackerID,
			Payload: EventPayload{
				Plate:       r.Plate,
				PlateFull:   r.PlateFull,
				Region:      r.Region,
				Country:     r.Country,
				Score:       r.Score,
				BBox:        normaliseBBox(r.BBox, fw, fh),
				Width:       r.Width,
				Height:      r.Height,
				Direction:   direction(r.DirectionLR, r.DirectionUD),
				DirectionLR: r.DirectionLR,
				DirectionUD: r.DirectionUD,
				Layout:      layoutLabel(r.Layout),
				Speed:       r.Speed,
			},
			Thumbnail: r.Thumbnail,
			FrameRef:  sourceRef,
		}

		// r.Thumbnail is only populated in MODE_LEAVE + stream=1 (where
		// platecore_to_jpeg has an internal best-frame buffer to draw
		// on). In every other mode we hand the job to the publisher
		// worker: it fetches the source frame from the ring buffer,
		// applies the bbox/crop we asked for, encodes a JPEG, and
		// uploads — all off the decode goroutine.
		if r.Thumbnail == nil {
			if m.cfg.Draw != 0 {
				ev.Overlays = []analytics.Overlay{{BBox: r.BBox}}
			}
			if m.cfg.Crop != 0 {
				bbox := r.BBox
				ev.CropRegion = &bbox
			}
		}

		events = append(events, ev)
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

// direction composes PlateCore's two axis ints
// (direction_left_right, direction_up_down) into a single human-
// readable label spanning the full 9-state grid: a cardinal/diagonal
// vector plus "stationary". Per PlateCore SDK 1.2.2 docs each axis is
// one of -1 (stationary), 0 (left/up), 1 (right/down). Any value
// outside that set surfaces as "unknown" so consumers can detect SDK
// drift without crashing.
func direction(lr, ud int) string {
	switch {
	case lr == -1 && ud == -1:
		return "stationary"
	case lr == -1 && ud == 0:
		return "up"
	case lr == -1 && ud == 1:
		return "down"
	case lr == 0 && ud == -1:
		return "left"
	case lr == 0 && ud == 0:
		return "up-left"
	case lr == 0 && ud == 1:
		return "down-left"
	case lr == 1 && ud == -1:
		return "right"
	case lr == 1 && ud == 0:
		return "up-right"
	case lr == 1 && ud == 1:
		return "down-right"
	default:
		return "unknown"
	}
}

// layoutLabel translates PlateCore's layout int (0 = rectangle,
// 1 = square) into a stable label.
func layoutLabel(v int) string {
	switch v {
	case 0:
		return "rectangle"
	case 1:
		return "square"
	default:
		return "unknown"
	}
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
