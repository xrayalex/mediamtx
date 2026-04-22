package recordstore

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bluenviron/mediamtx/internal/conf"
)

// ErrNoSegmentsFound is returned when no recording segments have been found.
var ErrNoSegmentsFound = errors.New("no recording segments found")

var errFound = errors.New("found")

// Segment is a recording segment.
type Segment struct {
	Fpath string
	Start time.Time
}

func fixedPathHasSegments(pathConf *conf.Path) bool {
	recordPath := PathAddExtension(
		strings.ReplaceAll(pathConf.RecordPath, "%path", pathConf.Name),
		pathConf.RecordFormat,
	)

	// we have to convert to absolute paths
	// otherwise, recordPath and fpath inside Walk() won't have common elements
	recordPath, _ = filepath.Abs(recordPath)

	commonPath := CommonPath(recordPath)

	err := filepath.WalkDir(commonPath, func(fpath string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			var pa Path
			ok := pa.Decode(recordPath, fpath)
			if ok {
				return errFound
			}
		}

		return nil
	})
	if err != nil && !errors.Is(err, errFound) {
		return false
	}

	return errors.Is(err, errFound)
}

func regexpPathFindPathsWithSegments(pathConf *conf.Path) map[string]struct{} {
	recordPath := PathAddExtension(
		pathConf.RecordPath,
		pathConf.RecordFormat,
	)

	// we have to convert to absolute paths
	// otherwise, recordPath and fpath inside Walk() won't have common elements
	recordPath, _ = filepath.Abs(recordPath)

	commonPath := CommonPath(recordPath)

	ret := make(map[string]struct{})

	filepath.WalkDir(commonPath, func(fpath string, info fs.DirEntry, err error) error { //nolint:errcheck
		if err != nil {
			return err
		}

		if !info.IsDir() {
			var pa Path
			if ok := pa.Decode(recordPath, fpath); ok {
				if err = conf.IsValidPathName(pa.Path); err == nil {
					if pathConf.Regexp.FindStringSubmatch(pa.Path) != nil {
						ret[pa.Path] = struct{}{}
					}
				}
			}
		}

		return nil
	})

	return ret
}

// FindAllPathsWithSegments returns all paths that have at least one segment.
func FindAllPathsWithSegments(pathConfs map[string]*conf.Path) []string {
	pathNames := make(map[string]struct{})

	for _, pathConf := range pathConfs {
		if pathConf.Regexp == nil {
			if fixedPathHasSegments(pathConf) {
				pathNames[pathConf.Name] = struct{}{}
			}
		} else {
			for name := range regexpPathFindPathsWithSegments(pathConf) {
				pathNames[name] = struct{}{}
			}
		}
	}

	out := make([]string, len(pathNames))
	n := 0
	for k := range pathNames {
		out[n] = k
		n++
	}
	sort.Strings(out)

	return out
}

// FindSegments returns all segments of a path.
// Segments can be filtered by start date and end date.
func FindSegments(
	pathConf *conf.Path,
	pathName string,
	start *time.Time,
	end *time.Time,
) ([]*Segment, error) {
	// double protection against directory traversal attacks
	if err := conf.IsValidPathName(pathName); err != nil {
		return nil, fmt.Errorf("invalid path name: %w (%s)", err, pathName)
	}

	recordPath := PathAddExtension(
		strings.ReplaceAll(pathConf.RecordPath, "%path", pathName),
		pathConf.RecordFormat,
	)

	// we have to convert to absolute paths
	// otherwise, recordPath and fpath inside Walk() won't have common elements
	recordPath, _ = filepath.Abs(recordPath)

	commonPath := CommonPath(recordPath)
	var segments []*Segment

	err := filepath.WalkDir(commonPath, func(fpath string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			var pa Path
			if pa.Decode(recordPath, fpath) {
				segments = append(segments, &Segment{
					Fpath: fpath,
					Start: pa.Start,
				})
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if segments == nil {
		return nil, ErrNoSegmentsFound
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Start.Before(segments[j].Start)
	})

	// filter by overlap with [start, end].
	// each segment is approximated to span from its Start to the next segment's Start;
	// the last segment is treated as open-ended.
	// fixes #4276 (start inside segment failed due to ms-level mismatch).
	if start != nil || end != nil {
		filtered := segments[:0]
		for i, seg := range segments {
			// segment must start at or before the end of the playback
			if end != nil && end.Before(seg.Start) {
				continue
			}

			// segment's approximate end must be after the start of the playback
			if start != nil && i+1 < len(segments) && !segments[i+1].Start.After(*start) {
				continue
			}

			filtered = append(filtered, seg)
		}

		// fixes #4164: when the requested range is entirely before any segment,
		// return the first segment that starts at or after the requested start
		// instead of an empty list, so callers can present "next available".
		if len(filtered) == 0 && start != nil {
			for _, seg := range segments {
				if !seg.Start.Before(*start) {
					filtered = append(filtered, seg)
					break
				}
			}
		}

		segments = filtered
	}

	if len(segments) == 0 {
		return nil, ErrNoSegmentsFound
	}

	return segments, nil
}
