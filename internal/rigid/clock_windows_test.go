package rigid

import (
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// newClock returns a function that reads the time since it was made from
// the performance counter: time.Now moves only about every half
// millisecond on Windows (docs/pitfalls.md), too coarse for one stage of a
// tick. internal/app reads it the same way for -timing.
func newClock(tb testing.TB) func() time.Duration {
	tb.Helper()
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		tb.Fatal(err)
	}
	counter, err := dll.FindProc("QueryPerformanceCounter")
	if err != nil {
		tb.Fatal(err)
	}
	frequency, err := dll.FindProc("QueryPerformanceFrequency")
	if err != nil {
		tb.Fatal(err)
	}
	var freq int64
	if ok, _, err := frequency.Call(uintptr(unsafe.Pointer(&freq))); ok == 0 || freq <= 0 {
		tb.Fatalf("QueryPerformanceFrequency: %v", err)
	}
	read := func() int64 {
		var n int64
		_, _, _ = syscall.SyscallN(counter.Addr(), uintptr(unsafe.Pointer(&n)))
		return n
	}
	origin := read()
	return func() time.Duration {
		n := read() - origin
		return time.Duration(n/freq)*time.Second + time.Duration(n%freq*int64(time.Second)/freq)
	}
}
