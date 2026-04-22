//go:build analytics

package analytics

// Publisher delivers Events to NATS and uploads thumbnails to MinIO.
//
// This file holds the type so other files in the package can reference
// it; the concrete NATS / MinIO wiring is added in a later commit.
type Publisher struct{}

// Publish is a no-op placeholder until the NATS / MinIO implementation
// lands. Returning nil keeps the reader's fan-out well-behaved when no
// real publisher is wired in.
func (*Publisher) Publish(*Event) error { return nil }

// Close releases publisher resources.
func (*Publisher) Close() {}
