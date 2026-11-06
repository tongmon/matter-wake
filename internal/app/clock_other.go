//go:build !windows

package app

import "time"

// clock reads time.Now, which is fine enough on every OS but Windows.
type clock struct{ origin time.Time }

func newClock() (*clock, error) {
	return &clock{origin: time.Now()}, nil
}

// now returns the time since the clock was made.
func (c *clock) now() time.Duration {
	return time.Since(c.origin)
}
