//go:build !windows

package rigid

import (
	"testing"
	"time"
)

// newClock returns a function that reads the time since it was made.
// time.Now is fine enough everywhere but Windows.
func newClock(testing.TB) func() time.Duration {
	origin := time.Now()
	return func() time.Duration { return time.Since(origin) }
}
