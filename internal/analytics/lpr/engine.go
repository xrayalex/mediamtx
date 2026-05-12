//go:build analytics && lpr

// Package lpr is the LPR (license plate recognition) analytics module
// backed by the PlateCore native SDK.
package lpr

/*
#cgo CFLAGS: -I${SRCDIR}/../../../deploy/vendor/platecore/include
#cgo LDFLAGS: -L${SRCDIR}/../../../deploy/vendor/platecore/x86/lib -lCore -Wl,-rpath,/opt/platecore/lib

// PlateCore's public header is C++ (it carries default arguments in
// struct fields and function declarations) so it cannot be included from
// cgo. The library exposes a stable C ABI nonetheless, so we redeclare
// the few types and prototypes we need here. Keep field layouts in sync
// with deploy/vendor/platecore/include/platecore/platecore_types.h.

#include <stdint.h>
#include <stdlib.h>

typedef void* platecore_obj;
typedef int32_t plate_core_retcode;

typedef enum {
    PLATE_TYPE_AUTO = 0,
    PLATE_TYPE_WAGON = 1,
    PLATE_TYPE_BOAT = 2,
    PLATE_TYPE_CONTAINER = 3
} plate_type;

typedef enum {
    PIX_FMT_YUV420P = 0,
    PIX_FMT_NV12 = 1,
    PIX_FMT_BGR = 2,
    PIX_FMT_RGB = 3
} pixel_format;

typedef enum {
    MODE_ENTER_ONCE     = 0,
    MODE_ENTER_INTERVAL = 1,
    MODE_LEAVE          = 2,
    MODE_RAW            = 3
} mode_filter;

typedef struct {
    float roi_rect[4];
    float plate_size_min[2];
    float plate_size_max[2];
    int min_hits;
    mode_filter mode;
    int repeat_event;
    int ttl;
    int stream;
} plate_core_init_arg;

typedef struct {
    int is_valid;
    int tracker_id;
    char* plate_text_full;
    char* plate_text;
    char* region;
    char* country;
    float score;
    int bbox[4];
    int direction_left_right;
    int direction_up_down;
    int layout;
    float speed;
    uint64_t timestamp;
    void* frame;
    int width;
    int height;
    pixel_format format;
} processing_result;

typedef struct {
    void* buffer;
    int size;
} jpeg_buffer;

extern platecore_obj create_platecore_obj();
extern plate_core_retcode platecore_init(platecore_obj* obj, plate_core_init_arg* arg, plate_type type);
extern plate_core_retcode platecore_proccesing(platecore_obj* obj, void* data, int width, int height, int timestamp, pixel_format format);
extern int platecore_get_result_count(platecore_obj* obj);
extern plate_core_retcode platecore_get_result_by_index(platecore_obj* obj, int index, processing_result* out_event);
extern plate_core_retcode platecore_to_jpeg(processing_result* out_event, jpeg_buffer* buffer, int crop, int draw);
extern plate_core_retcode platecore_processing_release(platecore_obj* obj, processing_result* out_event);
extern plate_core_retcode platecore_jpeg_release(jpeg_buffer* buffer);
extern plate_core_retcode platecore_release(platecore_obj* obj);
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// PlateCore return codes that we treat specially. Numerical values
// match deploy/vendor/platecore/include/platecore/platecore_types.h.
const (
	retOK                         = 0
	retErrInternalServer          = -500
	retErrNotFoundLicense         = -501
	retErrLicenseHardwareMismatch = -502
	retErrLicenseBusy             = -503
	retErrNotFound                = -504
)

// Mode mirrors PlateCore's mode_filter enum.
type Mode int

const (
	ModeEnterOnce     Mode = 0
	ModeEnterInterval Mode = 1
	ModeLeave         Mode = 2
	ModeRaw           Mode = 3
)

// PlateType mirrors PlateCore's plate_type enum.
type PlateType int

const (
	PlateTypeAuto      PlateType = 0
	PlateTypeWagon     PlateType = 1
	PlateTypeBoat      PlateType = 2
	PlateTypeContainer PlateType = 3
)

// EngineConfig is the Go-side mirror of PlateCore's plate_core_init_arg
// plus the plate_type passed to platecore_init.
type EngineConfig struct {
	ROIRect      [4]float32
	PlateSizeMin [2]float32
	PlateSizeMax [2]float32
	MinHits      int
	Mode         Mode
	RepeatEvent  int
	TTL          int
	Stream       int
	PlateType    PlateType
}

// PlateCoreError wraps a non-zero PlateCore return code together with
// the C entry point that produced it.
type PlateCoreError struct {
	Op   string
	Code int
}

func (e *PlateCoreError) Error() string {
	return fmt.Sprintf("platecore: %s returned %d", e.Op, e.Code)
}

func wrapErr(op string, ret C.plate_core_retcode) error {
	if ret == retOK {
		return nil
	}
	return &PlateCoreError{Op: op, Code: int(ret)}
}

// Engine wraps a single platecore_obj instance.
//
// Engine is single-goroutine: each instance must only be used from the
// goroutine that created it. PlateCore keeps per-object state (the
// tracker, the best-frame buffer for MODE_LEAVE) so sharing across
// goroutines would corrupt state.
type Engine struct {
	obj C.platecore_obj
}

// NewEngine creates a fresh PlateCore object and runs platecore_init on
// it.
//
// The license proxy must be running and reachable. When it is not,
// platecore_init returns one of the PLATECORE_ERR_*_LICENSE codes,
// surfaced here as PlateCoreError; callers must treat that as
// "LPR temporarily unavailable" rather than a fatal misconfiguration.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	obj := C.create_platecore_obj()
	if obj == nil {
		return nil, errors.New("platecore: create_platecore_obj returned NULL")
	}

	arg := configToArg(cfg)
	ret := C.platecore_init(&obj, &arg, C.plate_type(cfg.PlateType))
	if ret != retOK {
		// Best-effort cleanup: release the object we just allocated so
		// repeated init failures do not leak.
		C.platecore_release(&obj)
		return nil, wrapErr("platecore_init", ret)
	}

	return &Engine{obj: obj}, nil
}

// Close destroys the PlateCore object. Safe to call more than once.
func (e *Engine) Close() error {
	if e.obj == nil {
		return nil
	}
	ret := C.platecore_release(&e.obj)
	e.obj = nil
	return wrapErr("platecore_release", ret)
}

// Result is one recognised plate extracted from a processing_result.
//
// Thumbnail is a JPEG-encoded image when crop or draw were requested
// in Process(); otherwise nil.
type Result struct {
	TrackerID   int
	Plate       string
	PlateFull   string
	Region      string
	Country     string
	Score       float32
	BBox        [4]int
	Width       int
	Height      int
	DirectionLR int     // PlateCore raw: -1 stationary, 0 left, 1 right
	DirectionUD int     // PlateCore raw: -1 stationary, 0 up, 1 down
	Layout      int     // PlateCore raw: 0 rectangle, 1 square
	Speed       float32 // PlateCore-reported speed, SDK-defined units
	Thumbnail   []byte
}

// Process submits a BGR24 frame to PlateCore and returns the list of
// detected plates plus optional thumbnails.
//
// crop=1 returns a JPEG of just the plate region; draw=1 overlays the
// bbox and recognised text on the JPEG. crop=0 && draw=0 skips the
// to-JPEG conversion entirely.
func (e *Engine) Process(bgr []byte, width, height, timestamp, crop, draw int) ([]Result, error) {
	if e.obj == nil {
		return nil, errors.New("platecore: engine closed")
	}

	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("platecore: invalid frame dimensions %dx%d", width, height)
	}
	if len(bgr) < width*height*3 {
		return nil, fmt.Errorf("platecore: bgr buffer is %d bytes, need at least %d",
			len(bgr), width*height*3)
	}

	ret := C.platecore_proccesing(
		&e.obj,
		unsafe.Pointer(&bgr[0]),
		C.int(width),
		C.int(height),
		C.int(timestamp),
		C.PIX_FMT_BGR,
	)
	if ret != retOK {
		return nil, wrapErr("platecore_proccesing", ret)
	}

	n := int(C.platecore_get_result_count(&e.obj))
	if n <= 0 {
		return nil, nil
	}

	results := make([]Result, 0, n)

	for i := 0; i < n; i++ {
		var ev C.processing_result
		if r := C.platecore_get_result_by_index(&e.obj, C.int(i), &ev); r != retOK {
			// Skip this result, but do NOT release: we never had a
			// successful get to pair it with.
			continue
		}

		if ev.is_valid == 0 {
			C.platecore_processing_release(&e.obj, &ev)
			continue
		}

		var thumb []byte
		if crop != 0 || draw != 0 {
			var buf C.jpeg_buffer
			jr := C.platecore_to_jpeg(&ev, &buf, C.int(crop), C.int(draw))
			if jr == retOK && buf.buffer != nil && buf.size > 0 {
				thumb = C.GoBytes(buf.buffer, buf.size)
				C.platecore_jpeg_release(&buf)
			}
		}

		results = append(results, Result{
			TrackerID: int(ev.tracker_id),
			Plate:     C.GoString(ev.plate_text),
			PlateFull: C.GoString(ev.plate_text_full),
			Region:    C.GoString(ev.region),
			Country:   C.GoString(ev.country),
			Score:     float32(ev.score),
			BBox: [4]int{
				int(ev.bbox[0]), int(ev.bbox[1]),
				int(ev.bbox[2]), int(ev.bbox[3]),
			},
			Width:       int(ev.width),
			Height:      int(ev.height),
			DirectionLR: int(ev.direction_left_right),
			DirectionUD: int(ev.direction_up_down),
			Layout:      int(ev.layout),
			Speed:       float32(ev.speed),
			Thumbnail:   thumb,
		})

		// Pairs with the successful platecore_get_result_by_index above.
		// Release after copying every C string and the optional JPEG so
		// we never leave the C heap holding strings we already turned
		// into Go strings.
		C.platecore_processing_release(&e.obj, &ev)
	}

	return results, nil
}

// IsLicenseError reports whether err is a PlateCore return code that
// indicates a license-server problem (proxy unreachable, license busy,
// hardware mismatch). Callers can use this to log a softer warning
// instead of a hard error.
func IsLicenseError(err error) bool {
	var pcErr *PlateCoreError
	if !errors.As(err, &pcErr) {
		return false
	}
	switch pcErr.Code {
	case retErrInternalServer, retErrNotFoundLicense,
		retErrLicenseHardwareMismatch, retErrLicenseBusy:
		return true
	default:
		return false
	}
}

func configToArg(cfg EngineConfig) C.plate_core_init_arg {
	var arg C.plate_core_init_arg
	for i := 0; i < 4; i++ {
		arg.roi_rect[i] = C.float(cfg.ROIRect[i])
	}
	arg.plate_size_min[0] = C.float(cfg.PlateSizeMin[0])
	arg.plate_size_min[1] = C.float(cfg.PlateSizeMin[1])
	arg.plate_size_max[0] = C.float(cfg.PlateSizeMax[0])
	arg.plate_size_max[1] = C.float(cfg.PlateSizeMax[1])
	arg.min_hits = C.int(cfg.MinHits)
	arg.mode = C.mode_filter(cfg.Mode)
	arg.repeat_event = C.int(cfg.RepeatEvent)
	arg.ttl = C.int(cfg.TTL)
	arg.stream = C.int(cfg.Stream)
	return arg
}
