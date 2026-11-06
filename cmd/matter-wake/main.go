// Command matter-wake runs the game.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/tongmon/matter-wake/internal/app"
	"github.com/tongmon/matter-wake/internal/game"
)

func main() {
	if err := run(); err != nil {
		slog.Error("matter-wake failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg app.Config
	scene := flag.String("scene", "sandbox", "world to play in: sandbox, a small room, or terrain, the big world")
	flag.IntVar(&cfg.Width, "width", 0, "world width in cells (default: 480 in the sandbox, 16384 in the terrain)")
	flag.IntVar(&cfg.Height, "height", 0, "world height in cells (default: 270 in the sandbox, 16384 in the terrain)")
	flag.IntVar(&cfg.Scale, "scale", 2, "window scale")
	seed := flag.Uint64("seed", 0, "world seed (default: a new seed, logged at start)")
	flag.IntVar(&cfg.Workers, "workers", runtime.GOMAXPROCS(0), "goroutines a tick may use")
	flag.StringVar(&cfg.MaterialsPath, "materials", "data/materials.json", "material table")
	shots := flag.String("shots", "", "comma-separated ticks to save as PNG, then exit")
	flag.StringVar(&cfg.ShotsDir, "shots-dir", "out/shots", "folder for -shots")
	flag.StringVar(&cfg.ReplayPath, "replay", "", "play back this replay file instead of the devices, then exit")
	flag.StringVar(&cfg.RecordPath, "record", "", "save this session's inputs to this replay file on exit")
	flag.BoolVar(&cfg.Creative, "creative", false, "place without taking from the inventory, to try materials out")
	flag.StringVar(&cfg.SavePath, "save", "", "save folder of the terrain (default: matter-wake/saves/world in the user's config folder)")
	flag.BoolVar(&cfg.Timing, "timing", false, "log how long ticks, draws and frames took, on exit")
	profile := flag.String("cpuprofile", "", "write a CPU profile of the session to this file, for go tool pprof")
	flag.Parse()

	if cfg.ReplayPath != "" {
		for _, name := range []string{"scene", "seed", "width", "height", "creative"} {
			if flagSet(name) {
				return fmt.Errorf("-%s cannot be combined with -replay: the replay sets it", name)
			}
		}
	}
	if err := setScene(&cfg, *scene); err != nil {
		return err
	}
	cfg.FixedWorld = flagSet("seed") || flagSet("width") || flagSet("height")
	cfg.Seed = *seed
	if !flagSet("seed") {
		cfg.Seed = uint64(time.Now().UnixNano())
	}
	ticks, err := parseTicks(*shots)
	if err != nil {
		return err
	}
	cfg.Shots = ticks
	if *profile == "" {
		return app.Run(&cfg)
	}
	f, err := createProfile(*profile)
	if err != nil {
		return err
	}
	if err := profileTo(f, func() error { return app.Run(&cfg) }); err != nil {
		return err
	}
	slog.Info("profiled", "path", *profile)
	return nil
}

// createProfile creates the file of a CPU profile, and its folder.
func createProfile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("-cpuprofile: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("-cpuprofile: %w", err)
	}
	return f, nil
}

// profileTo runs fn while a CPU profile is written to w, and closes w. A
// profile that cannot be finished fails the run even when fn did not.
func profileTo(w io.WriteCloser, fn func() error) (err error) {
	// pprof drops the writer's errors, so keep the first one here.
	kept := &keepError{w: w}
	if err := pprof.StartCPUProfile(kept); err != nil {
		return errors.Join(fmt.Errorf("-cpuprofile: %w", err), w.Close())
	}
	defer func() {
		pprof.StopCPUProfile()
		for _, perr := range []error{kept.err, w.Close()} {
			if perr != nil {
				err = errors.Join(err, fmt.Errorf("-cpuprofile: %w", perr))
			}
		}
	}()
	return fn()
}

// keepError writes to w and keeps the first error it gave.
type keepError struct {
	w   io.Writer
	err error
}

func (k *keepError) Write(p []byte) (int, error) {
	n, err := k.w.Write(p)
	if err != nil && k.err == nil {
		k.err = err
	}
	return n, err
}

// setScene sets the scene and fills in the world size the flags left out:
// the sandbox fills the screen, and the terrain is the big world of
// design.md 7절 (🧪).
func setScene(cfg *app.Config, scene string) error {
	width, height := 480, 270
	switch scene {
	case "sandbox":
		cfg.Scene = game.SceneSandbox
	case "terrain":
		cfg.Scene = game.SceneTerrain
		width, height = 16384, 16384
	default:
		return fmt.Errorf("-scene %q: want sandbox or terrain", scene)
	}
	if !flagSet("width") {
		cfg.Width = width
	}
	if !flagSet("height") {
		cfg.Height = height
	}
	return nil
}

func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func parseTicks(s string) ([]uint32, error) {
	if s == "" {
		return nil, nil
	}
	var ticks []uint32
	for field := range strings.SplitSeq(s, ",") {
		n, err := strconv.ParseUint(strings.TrimSpace(field), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("-shots: %q is not a tick number", field)
		}
		ticks = append(ticks, uint32(n))
	}
	return ticks, nil
}
