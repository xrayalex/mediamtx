//go:build analytics && lpr

package core

// Side-effect import: the lpr package's init() registers the "lpr"
// module in the analytics registry. Importing it here (in the core
// integration layer) keeps the analytics package free of cyclic
// dependencies on its own modules.
import _ "github.com/bluenviron/mediamtx/internal/analytics/lpr"
