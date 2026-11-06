package main

import (
	"bytes"
	"errors"
	"testing"
)

// failingClose is a profile file whose Close fails, as a full disk can.
type failingClose struct{ bytes.Buffer }

var errClose = errors.New("disk full")

func (*failingClose) Close() error { return errClose }

// A profile that cannot be finished fails the run even when the session
// itself went well (Codex review).
func TestProfileReportsAFailedClose(t *testing.T) {
	err := profileTo(new(failingClose), func() error { return nil })
	if !errors.Is(err, errClose) {
		t.Fatalf("profileTo = %v, want the close error", err)
	}
}

func TestProfileKeepsTheRunsError(t *testing.T) {
	errRun := errors.New("session failed")
	err := profileTo(new(failingClose), func() error { return errRun })
	if !errors.Is(err, errRun) || !errors.Is(err, errClose) {
		t.Fatalf("profileTo = %v, want both the session's and the close error", err)
	}
}

// failingWrite is a profile file that takes no data, as a full disk, but
// closes.
type failingWrite struct{}

var errWrite = errors.New("no space left")

func (failingWrite) Write([]byte) (int, error) { return 0, errWrite }
func (failingWrite) Close() error              { return nil }

// pprof drops the errors of the writer it is given, so profileTo keeps
// them itself (second Codex review).
func TestProfileReportsAFailedWrite(t *testing.T) {
	err := profileTo(failingWrite{}, func() error { return nil })
	if !errors.Is(err, errWrite) {
		t.Fatalf("profileTo = %v, want the write error", err)
	}
}
