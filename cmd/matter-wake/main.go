// Command matter-wake runs the game.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tongmon/matter-wake/internal/app"
)

func main() {
	if err := run(); err != nil {
		slog.Error("matter-wake failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg app.Config
	flag.IntVar(&cfg.Width, "width", 480, "world width in cells")
	flag.IntVar(&cfg.Height, "height", 270, "world height in cells")
	flag.IntVar(&cfg.Scale, "scale", 2, "window scale")
	seed := flag.Uint64("seed", 0, "world seed (default: a new seed, logged at start)")
	flag.IntVar(&cfg.Workers, "workers", runtime.GOMAXPROCS(0), "goroutines a tick may use")
	flag.StringVar(&cfg.MaterialsPath, "materials", "data/materials.json", "material table")
	shots := flag.String("shots", "", "comma-separated ticks to save as PNG, then exit")
	flag.StringVar(&cfg.ShotsDir, "shots-dir", "out/shots", "folder for -shots")
	flag.StringVar(&cfg.ReplayPath, "replay", "", "play back this replay file instead of the devices, then exit")
	flag.StringVar(&cfg.RecordPath, "record", "", "save this session's inputs to this replay file on exit")
	flag.Parse()

	if cfg.ReplayPath != "" {
		for _, name := range []string{"seed", "width", "height"} {
			if flagSet(name) {
				return fmt.Errorf("-%s cannot be combined with -replay: the replay sets it", name)
			}
		}
	}
	cfg.Seed = *seed
	if !flagSet("seed") {
		cfg.Seed = uint64(time.Now().UnixNano())
	}
	ticks, err := parseTicks(*shots)
	if err != nil {
		return err
	}
	cfg.Shots = ticks
	return app.Run(&cfg)
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
