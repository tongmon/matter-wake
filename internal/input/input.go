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
// from the screen into world cells under the moved camera. B drops one
// body per press.
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
		Drop:    inpututil.IsKeyJustPressed(ebiten.KeyB),
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

// Keys are the test dashboard's keys, as a set.
type Keys uint16

// The dashboard's keys (🔶): Tab opens and closes the panel, Space pauses,
// F runs one tick and pauses, G slows the ticks, and F1 to F5 turn the
// views on and off.
const (
	KeyPanel Keys = 1 << iota
	KeyPause
	KeyStep
	KeySlow
	// KeyView is F1; the views follow it, one key each.
	KeyView
)

// keyOf maps the dashboard's keys to Ebitengine's, in the order of Keys.
var keyOf = [...]ebiten.Key{
	ebiten.KeyTab, ebiten.KeySpace, ebiten.KeyF, ebiten.KeyG,
	ebiten.KeyF1, ebiten.KeyF2, ebiten.KeyF3, ebiten.KeyF4, ebiten.KeyF5,
}

// UI is what the devices did for the test dashboard in one update.
type UI struct {
	// X and Y are the cursor on the screen, in logical pixels.
	X, Y int
	// Click is set when the left button was just pressed.
	Click bool
	// Shift is set while Shift is held.
	Shift bool
	// Wheel is how far the wheel turned, up positive: 1 a notch of a
	// mouse wheel, less from a touchpad.
	Wheel float64
	// Keys are the dashboard's keys just pressed.
	Keys Keys
}

// ReadUI samples the devices once for the dashboard.
func ReadUI() UI {
	x, y := ebiten.CursorPosition()
	ui := UI{
		X: x, Y: y,
		Click: inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft),
		Shift: ebiten.IsKeyPressed(ebiten.KeyShift),
	}
	_, ui.Wheel = ebiten.Wheel()
	for i, k := range keyOf {
		if inpututil.IsKeyJustPressed(k) {
			ui.Keys |= 1 << i
		}
	}
	return ui
}
