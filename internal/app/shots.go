package app

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/render"
)

// shooter saves the world at chosen ticks. It draws inside Update rather
// than taking frames from Draw, because Ebitengine may run several Updates
// between two Draws and a tick would be missed.
type shooter struct {
	ticks []uint32
	dir   string
	img   *ebiten.Image
	pix   []byte
	rect  image.Rectangle
}

func newShooter(ticks []uint32, dir string, width, height int) *shooter {
	return &shooter{
		ticks: sortedTicks(ticks),
		dir:   dir,
		img:   ebiten.NewImage(width, height),
		pix:   make([]byte, 4*width*height),
		rect:  image.Rect(0, 0, width, height),
	}
}

// capture saves the current tick if it was asked for. It reports whether
// every shot has been taken.
func (s *shooter) capture(g *game.Game, r *render.Renderer) (bool, error) {
	tick := g.World().Tick()
	for len(s.ticks) > 0 && s.ticks[0] <= tick {
		if s.ticks[0] == tick {
			if err := s.save(g, r, tick); err != nil {
				return false, err
			}
		}
		s.ticks = s.ticks[1:]
	}
	return len(s.ticks) == 0, nil
}

func (s *shooter) save(g *game.Game, r *render.Renderer, tick uint32) error {
	r.DrawWorld(s.img, g.World())
	s.img.ReadPixels(s.pix)
	// ReadPixels gives premultiplied RGBA, which is what image.RGBA holds.
	img := &image.RGBA{Pix: s.pix, Stride: 4 * s.rect.Dx(), Rect: s.rect}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("shots: %w", err)
	}
	path := filepath.Join(s.dir, fmt.Sprintf("tick_%06d.png", tick))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("shots: %w", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return fmt.Errorf("shots: %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("shots: %s: %w", path, err)
	}
	return nil
}
