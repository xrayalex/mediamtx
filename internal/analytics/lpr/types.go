//go:build analytics && lpr

package lpr

// Config is the per-camera LPR configuration parsed from
// PathAnalyticsModule.Config (JSON).
//
// Field defaults are applied in Module.Configure before unmarshaling so a
// minimal `config: {}` keeps PlateCore in its sensible "stream tracker
// with leave-mode events" defaults.
type Config struct {
	PlateType    string     `json:"plate_type"`     // "auto" (default), "wagon", "boat", "container"
	Mode         string     `json:"mode"`           // "enter_once", "enter_interval", "leave" (default), "raw"
	MinHits      int        `json:"min_hits"`       // accumulator depth (default 3)
	RepeatEvent  int        `json:"repeat_event"`   // seconds (default 5)
	TTL          int        `json:"ttl"`            // seconds (default 60)
	Stream       int        `json:"stream"`         // 1 = stream/tracker mode (default), 0 = single-frame
	ROIRect      [4]float32 `json:"roi_rect"`       // xmin,ymin,xmax,ymax in 0..1
	PlateSizeMin [2]float32 `json:"plate_size_min"` // w,h in 0..1
	PlateSizeMax [2]float32 `json:"plate_size_max"` // w,h in 0..1
	Crop         int        `json:"crop"`           // 1 = thumbnail is plate crop (default 0 → full frame)
	Draw         int        `json:"draw"`           // 1 = overlay bbox+text on thumbnail
}

// EventPayload is the JSON shape emitted as Event.Payload.
//
// BBox is normalised to [x, y, w, h] in 0..1 of the source frame so
// the coordinates remain meaningful regardless of the consumer's
// display dimensions.
//
// Direction is the composed 2D motion vector across both PlateCore
// axes (9 states: "stationary", "up", "down", "left", "right",
// "up-left", "up-right", "down-left", "down-right"). DirectionLR
// and DirectionUD carry the raw per-axis values from
// processing_result (-1 = stationary, 0 = left/up, 1 = right/down)
// for consumers that need axis-level filtering. Unrecognised SDK
// combinations land in Direction as "unknown".
//
// Layout mirrors PlateCore's layout int (0 = rectangle, 1 = square).
//
// Speed is the value reported by PlateCore. Units per SDK 1.2.2 docs:
// metres per second (when calibration is available); 0 when not.
type EventPayload struct {
	Plate       string     `json:"plate"`
	PlateFull   string     `json:"plate_full,omitempty"`
	Region      string     `json:"region,omitempty"`
	Country     string     `json:"country,omitempty"`
	Score       float32    `json:"score"`
	BBox        [4]float32 `json:"bbox"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	Direction   string     `json:"direction"`    // composed 9-state label
	DirectionLR int        `json:"direction_lr"` // raw: -1 stationary, 0 left, 1 right
	DirectionUD int        `json:"direction_ud"` // raw: -1 stationary, 0 up,   1 down
	Layout      string     `json:"layout"`       // "rectangle" | "square" | "unknown"
	Speed       float32    `json:"speed"`        // metres per second (PlateCore SDK 1.2.2)
}
