package replay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
)

// loadTable reads the replay tests' own copy of the materials, so tuning
// data/materials.json does not move the goldens.
func loadTable(t *testing.T) *material.Table {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := material.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func newGame(t *testing.T, cfg *game.Config) *game.Game {
	t.Helper()
	g, err := game.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}
