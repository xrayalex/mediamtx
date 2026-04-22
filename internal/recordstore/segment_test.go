package recordstore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/stretchr/testify/require"
)

func ptrOf[T any](v T) *T {
	p := new(T)
	*p = v
	return p
}

func TestFindAllPathsWithSegments(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
	require.NoError(t, err)

	err = os.Mkdir(filepath.Join(dir, "path2"), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "path1", "2015-05-19_22-15-25-000427.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "path2", "2015-07-19_22-15-25-000427.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	paths := FindAllPathsWithSegments(map[string]*conf.Path{
		"~^.*$": {
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
		"path2": {
			Name:         "path2",
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
	})
	require.Equal(t, []string{"path1", "path2"}, paths)
}

func TestFindAllPathsWithSegmentsInvalidPath(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.WriteFile(filepath.Join(dir, "_2015-05-19_22-15-25-000427.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	paths := FindAllPathsWithSegments(map[string]*conf.Path{
		"~^.*$": {
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path_%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
	})
	require.Equal(t, []string{}, paths)
}

func TestFindSegments(t *testing.T) {
	for _, ca := range []string{
		"no filtering",
		"filtering",
		"start before first",
	} {
		t.Run(ca, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "mediamtx-recordstore")
			require.NoError(t, err)
			defer os.RemoveAll(dir)

			err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
			require.NoError(t, err)

			err = os.Mkdir(filepath.Join(dir, "path2"), 0o755)
			require.NoError(t, err)

			err = os.WriteFile(filepath.Join(dir, "path1", "2015-05-19_22-15-25-000427.mp4"), []byte{1}, 0o644)
			require.NoError(t, err)

			err = os.WriteFile(filepath.Join(dir, "path1", "2016-05-19_22-15-25-000427.mp4"), []byte{1}, 0o644)
			require.NoError(t, err)

			var start *time.Time
			var end *time.Time

			switch ca {
			case "no filtering":

			case "filtering":
				start = ptrOf(time.Date(2015, 5, 19, 22, 18, 25, 427000, time.Local))
				end = ptrOf(start.Add(60 * time.Minute))

			case "start before first":
				start = ptrOf(time.Date(2014, 5, 19, 22, 18, 25, 427000, time.Local))
			}

			segments, err := FindSegments(
				&conf.Path{
					Name:         "~^.*$",
					Regexp:       regexp.MustCompile("^.*$"),
					RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
					RecordFormat: conf.RecordFormatFMP4,
				},
				"path1",
				start,
				end,
			)
			require.NoError(t, err)

			switch ca {
			case "no filtering", "start before first":
				require.Equal(t, []*Segment{
					{
						Fpath: filepath.Join(dir, "path1", "2015-05-19_22-15-25-000427.mp4"),
						Start: time.Date(2015, 5, 19, 22, 15, 25, 427000, time.Local),
					},
					{
						Fpath: filepath.Join(dir, "path1", "2016-05-19_22-15-25-000427.mp4"),
						Start: time.Date(2016, 5, 19, 22, 15, 25, 427000, time.Local),
					},
				}, segments)

			case "filtering":
				require.Equal(t, []*Segment{
					{
						Fpath: filepath.Join(dir, "path1", "2015-05-19_22-15-25-000427.mp4"),
						Start: time.Date(2015, 5, 19, 22, 15, 25, 427000, time.Local),
					},
				}, segments)
			}
		})
	}
}

// fixes #4276: when start is inside a segment but does not match the
// filename timestamp exactly (sub-second drift), the segment must still
// be returned.
func TestFindSegmentsStartMidSegment(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "path1", "2024-01-01_14-00-00-687000.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	start := time.Date(2024, 1, 1, 14, 0, 0, 676000000, time.Local)
	end := time.Date(2024, 1, 1, 14, 0, 0, 700000000, time.Local)

	segments, err := FindSegments(
		&conf.Path{
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
		"path1",
		&start,
		&end,
	)
	require.NoError(t, err)
	require.Equal(t, []*Segment{
		{
			Fpath: filepath.Join(dir, "path1", "2024-01-01_14-00-00-687000.mp4"),
			Start: time.Date(2024, 1, 1, 14, 0, 0, 687000000, time.Local),
		},
	}, segments)
}

// fixes #4164: when the requested range is entirely before any segment,
// FindSegments should return the first available segment instead of
// ErrNoSegmentsFound.
func TestFindSegmentsStartBeforeFirst(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "path1", "2024-01-01_14-00-00-000000.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "path1", "2024-01-01_15-00-00-000000.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.Local)
	end := time.Date(2024, 1, 1, 11, 0, 0, 0, time.Local)

	segments, err := FindSegments(
		&conf.Path{
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
		"path1",
		&start,
		&end,
	)
	require.NoError(t, err)
	require.Equal(t, []*Segment{
		{
			Fpath: filepath.Join(dir, "path1", "2024-01-01_14-00-00-000000.mp4"),
			Start: time.Date(2024, 1, 1, 14, 0, 0, 0, time.Local),
		},
	}, segments)
}

// when start falls into a presumed gap between two segments,
// the segment whose interval covers the gap is returned;
// downstream filters can then drop it based on the actual file duration.
func TestFindSegmentsStartInGap(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "path1", "2024-01-01_14-00-00-000000.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "path1", "2024-01-01_16-00-00-000000.mp4"), []byte{1}, 0o644)
	require.NoError(t, err)

	start := time.Date(2024, 1, 1, 15, 0, 0, 0, time.Local)
	end := time.Date(2024, 1, 1, 15, 30, 0, 0, time.Local)

	segments, err := FindSegments(
		&conf.Path{
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
		"path1",
		&start,
		&end,
	)
	require.NoError(t, err)
	require.Equal(t, []*Segment{
		{
			Fpath: filepath.Join(dir, "path1", "2024-01-01_14-00-00-000000.mp4"),
			Start: time.Date(2024, 1, 1, 14, 0, 0, 0, time.Local),
		},
	}, segments)
}

// sanity: an empty directory yields ErrNoSegmentsFound.
func TestFindSegmentsNoOverlapEmpty(t *testing.T) {
	dir, err := os.MkdirTemp("", "mediamtx-recordstore")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = os.Mkdir(filepath.Join(dir, "path1"), 0o755)
	require.NoError(t, err)

	start := time.Date(2024, 1, 1, 14, 0, 0, 0, time.Local)

	segments, err := FindSegments(
		&conf.Path{
			Name:         "~^.*$",
			Regexp:       regexp.MustCompile("^.*$"),
			RecordPath:   filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
			RecordFormat: conf.RecordFormatFMP4,
		},
		"path1",
		&start,
		nil,
	)
	require.ErrorIs(t, err, ErrNoSegmentsFound)
	require.Nil(t, segments)
}
