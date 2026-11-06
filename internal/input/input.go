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

// Camera speed in cells per tick, and how many times faster with Shift
// (🧪, design.md 7절).
const (
	cameraSpeed = 4
	cameraFast  = 4
)

// Camera is where the view is: its top-left cell, and the farthest that
// cell may go so the view stays in the world.
type Camera struct {
	X, Y       int
	MaxX, MaxY int
}

// Read samples the devices once for the coming tick. WASD or the arrow
// keys move the camera, kept within the world, and the cursor is turned
// from the screen into world cells under the moved camera.
func Read(cam Camera) game.Input {
	dx, dy := 0, 0
	if pressed(ebiten.KeyA, ebiten.KeyArrowLeft) {
		dx--
	}
	if pressed(ebiten.KeyD, ebiten.KeyArrowRight) {
		dx++
	}
	if pressed(ebiten.KeyW, ebiten.KeyArrowUp) {
		dy--
	}
	if pressed(ebiten.KeyS, ebiten.KeyArrowDown) {
		dy++
	}
	speed := cameraSpeed
	if pressed(ebiten.KeyShift) {
		speed *= cameraFast
	}
	camX := min(max(cam.X+dx*speed, 0), cam.MaxX)
	camY := min(max(cam.Y+dy*speed, 0), cam.MaxY)
	x, y := ebiten.CursorPosition()
	in := game.Input{
		CameraX: camX,
		CameraY: camY,
		CursorX: camX + x,
		CursorY: camY + y,
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

func pressed(keys ...ebiten.Key) bool {
	for _, k := range keys {
		if ebiten.IsKeyPressed(k) {
			return true
		}
	}
	return false
}
