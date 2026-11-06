package app

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// clock reads the performance counter, which counts far finer than
// time.Now on Windows.
type clock struct {
	// counter is the address of QueryPerformanceCounter.
	counter uintptr
	// freq is how many counts the counter makes a second.
	freq int64
	// origin is the count when the clock was made.
	origin int64
}

func newClock() (*clock, error) {
	// The standard library loads kernel32.dll itself and marks it a system
	// DLL, so LoadDLL takes it from the system folder only.
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return nil, err
	}
	counter, err := dll.FindProc("QueryPerformanceCounter")
	if err != nil {
		return nil, err
	}
	frequency, err := dll.FindProc("QueryPerformanceFrequency")
	if err != nil {
		return nil, err
	}
	c := &clock{counter: counter.Addr()}
	if ok, _, err := frequency.Call(uintptr(unsafe.Pointer(&c.freq))); ok == 0 {
		return nil, fmt.Errorf("QueryPerformanceFrequency: %w", err)
	}
	if c.freq <= 0 {
		return nil, errors.New("the performance counter has no frequency")
	}
	c.origin = c.read()
	return c, nil
}

// read returns the performance counter's count.
func (c *clock) read() int64 {
	var n int64
	// QueryPerformanceCounter does not fail on Windows XP and later.
	_, _, _ = syscall.SyscallN(c.counter, uintptr(unsafe.Pointer(&n)))
	return n
}

// now returns the time since the clock was made.
func (c *clock) now() time.Duration {
	return countsToDuration(c.read()-c.origin, c.freq)
}

// countsToDuration converts n counts at freq counts a second to a duration.
// It divides before it multiplies, so n can be as large as a counter that
// has run for years.
func countsToDuration(n, freq int64) time.Duration {
	return time.Duration(n/freq)*time.Second + time.Duration(n%freq*int64(time.Second)/freq)
}
