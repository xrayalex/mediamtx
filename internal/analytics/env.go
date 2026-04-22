//go:build analytics

package analytics

import "os"

// envOr returns the value of the environment variable named key,
// or fallback if the variable is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
