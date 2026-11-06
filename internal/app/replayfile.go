package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/tongmon/matter-wake/internal/replay"
)

// loadReplay reads the replay file at path.
func loadReplay(path string) (*replay.Replay, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	r, err := replay.Read(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", path, err)
	}
	return r, nil
}

// saveReplay writes r to path. It writes a temporary file next to it and
// renames it only once that succeeds, so a failed save keeps the old file.
func saveReplay(path string, r *replay.Replay) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("app: save replay: %w", err)
	}
	tmp := f.Name()
	err = replay.Write(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("app: save replay %s: %w", path, err)
	}
	return nil
}

// prepareRecording checks before the session starts that a replay can be
// saved at path, so a bad path cannot lose the whole session's inputs on
// exit. It creates the folder, refuses a path that is a folder, and makes
// and removes a temporary file there to check that it can write.
func prepareRecording(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("app: -record: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("app: -record %s is a directory", path)
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("app: -record: %w", err)
	}
	if err := errors.Join(f.Close(), os.Remove(f.Name())); err != nil {
		return fmt.Errorf("app: -record: %w", err)
	}
	return nil
}

// checkShots fails if a shot comes after the last of a replay's ticks.
func checkShots(shots []uint32, ticks int) error {
	if len(shots) == 0 {
		return nil
	}
	if last := slices.Max(shots); int64(last) > int64(ticks) {
		return fmt.Errorf("app: shot tick %d is past the replay's end at tick %d", last, ticks)
	}
	return nil
}
