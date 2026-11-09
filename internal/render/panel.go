package render

import (
	"image/color"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// The debug font's cell: every character is GlyphWidth pixels wide and a
// line LineHeight pixels tall.
const (
	GlyphWidth = 6
	LineHeight = 16
)

// panelPad is the margin inside a panel or box, in pixels.
const panelPad = 4

var (
	panelFill  = color.RGBA{R: 0x0c, G: 0x0c, B: 0x14, A: 0xd0}
	panelEdge  = color.RGBA{R: 0x70, G: 0x70, B: 0x88, A: 0xff}
	spanOn     = color.RGBA{R: 0x38, G: 0x58, B: 0x90, A: 0xff}
	spanHover  = color.RGBA{R: 0x50, G: 0x50, B: 0x60, A: 0xff}
	panelTitle = color.RGBA{R: 0x20, G: 0x20, B: 0x30, A: 0xff}
)

// Span is a run of text on a line of a panel. Act is what clicking it does,
// 0 for nothing; the panel's owner gives the numbers their meaning. On
// marks it chosen, drawn with a highlight.
type Span struct {
	Text string
	Act  int
	On   bool
}

// Panel is a box of lines of spans laid out on the debug font's grid, with
// its top-left corner at (X, Y) on the screen and Cols characters wide.
// The first line is its title.
type Panel struct {
	X, Y  int
	Cols  int
	Lines [][]Span
}

// Width returns the panel's width on the screen in pixels.
func (p *Panel) Width() int { return p.Cols*GlyphWidth + 2*panelPad }

// Height returns the panel's height on the screen in pixels.
func (p *Panel) Height() int { return len(p.Lines)*LineHeight + 2*panelPad }

// Contains reports whether the screen position (x, y) is on the panel.
func (p *Panel) Contains(x, y int) bool {
	return x >= p.X && y >= p.Y && x < p.X+p.Width() && y < p.Y+p.Height()
}

// Hit returns the act of the span at the screen position (x, y), 0 for
// none.
func (p *Panel) Hit(x, y int) int {
	if !p.Contains(x, y) {
		return 0
	}
	line := (y - p.Y - panelPad) / LineHeight
	if y < p.Y+panelPad || line >= len(p.Lines) || x < p.X+panelPad {
		return 0
	}
	col := (x - p.X - panelPad) / GlyphWidth
	at := 0
	for _, s := range p.Lines[line] {
		n := utf8.RuneCountInString(s.Text)
		if col >= at && col < at+n {
			return s.Act
		}
		at += n
	}
	return 0
}

// DrawPanel draws p, highlighting the spans that are on and the clickable
// span under the screen position (hoverX, hoverY).
func (r *Renderer) DrawPanel(dst *ebiten.Image, p *Panel, hoverX, hoverY int) {
	x, y := float32(p.X), float32(p.Y)
	w, h := float32(p.Width()), float32(p.Height())
	vector.FillRect(dst, x, y, w, h, panelFill, false)
	vector.FillRect(dst, x, y, w, LineHeight+panelPad, panelTitle, false)
	vector.StrokeRect(dst, x+0.5, y+0.5, w-1, h-1, 1, panelEdge, false)
	hover := p.Hit(hoverX, hoverY)
	for i, line := range p.Lines {
		ly := p.Y + panelPad + i*LineHeight
		col := 0
		for _, s := range line {
			sx := p.X + panelPad + col*GlyphWidth
			n := utf8.RuneCountInString(s.Text)
			switch {
			case s.On:
				vector.FillRect(dst, float32(sx), float32(ly+1), float32(n*GlyphWidth), LineHeight-2, spanOn, false)
			case s.Act != 0 && s.Act == hover:
				vector.FillRect(dst, float32(sx), float32(ly+1), float32(n*GlyphWidth), LineHeight-2, spanHover, false)
			}
			ebitenutil.DebugPrintAt(dst, s.Text, sx-1, ly)
			col += n
		}
	}
}
