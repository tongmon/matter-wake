package app

import "time"

// A clock reads how much time has passed, finely enough to time one tick.
// time.Now is not fine enough on Windows: it moves only about every half
// millisecond there (pitfalls.md), so each OS has its own clock. A reading
// is the time since the clock was made; newClock makes one.

// since returns the time since the clock read start.
func (c *clock) since(start time.Duration) time.Duration {
	return c.now() - start
}
