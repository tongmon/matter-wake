// Package input maps the keyboard and mouse to game.Input.
package input

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/tongmon/matter-wake/internal/game"
)

var slotKeys = [...]ebiten.Key{
	ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5,
	ebiten.Key6, ebiten.Key7, ebiten.Key8, ebiten.Key9,
}

// Read samples the devices once for the coming tick.
func Read() game.Input {
	x, y := ebiten.CursorPosition()
	in := game.Input{
		CursorX: x,
		CursorY: y,
		Place:   ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft),
		Dig:     ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight),
	}
	for i, k := range slotKeys {
		if inpututil.IsKeyJustPressed(k) {
			in.Select = i + 1
		}
	}
	return in
}
