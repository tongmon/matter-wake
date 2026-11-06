package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tongmon/matter-wake/internal/game"
	"github.com/tongmon/matter-wake/internal/material"
	"github.com/tongmon/matter-wake/internal/save"
	"github.com/tongmon/matter-wake/internal/world"
)

// A save folder holds the world header, the session (the player and the
// blocks in memory) and one file a block (design.md 7절).
const (
	worldFile   = "world.txt"
	sessionFile = "session.txt"
	blocksDir   = "blocks"
)

// defaultSave returns the save folder used when -save is not given: the
// world save in the user's config folder (%AppData% on Windows).
func defaultSave() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("app: no folder for saves: %w", err)
	}
	return filepath.Join(dir, "matter-wake", "saves", "world"), nil
}

// diskStore keeps the terrain's blocks in a save folder, a file each
// (game.Store).
type diskStore struct {
	dir  string
	mats *material.Table
}

func (s *diskStore) path(bx, by int) string {
	return filepath.Join(s.dir, blocksDir, fmt.Sprintf("%d_%d.mwb", bx, by))
}

func (s *diskStore) Load(bx, by int) (*world.Block, error) {
	path := s.path(bx, by)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // never saved: the terrain makes it
	}
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	b, err := save.DecodeBlock(bytes.NewReader(data), s.mats)
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", path, err)
	}
	return b, nil
}

func (s *diskStore) Save(bx, by int, b *world.Block) error {
	return writeFile(s.path(bx, by), func(w io.Writer) error { return save.EncodeBlock(w, b, s.mats) })
}

// writeFile writes a file through a temporary one next to it, renamed over
// it only once complete, so a failed write keeps the old file.
func writeFile(path string, write func(w io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	tmp := f.Name()
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("app: write %s: %w", path, err)
	}
	return nil
}

// openSave prepares the save folder dir for a terrain session started
// with sess and returns the session to play. If dir holds a world, the
// session goes on with it: its seed, size and tick, its blocks and the
// session file, and the view fits the saved size. Then fixed, set when the command line chose the seed or the
// size, is an error, so a world is never mixed with another's blocks.
// Otherwise dir becomes the save of a new world, and its header is
// written now, before any block, so the blocks always have their world.
func openSave(dir string, sess *game.Config, fixed bool) (*game.Config, error) {
	if err := os.MkdirAll(filepath.Join(dir, blocksDir), 0o755); err != nil {
		return nil, fmt.Errorf("app: -save: %w", err)
	}
	if err := checkWritable(dir); err != nil {
		return nil, err
	}
	out := *sess
	out.Store = &diskStore{dir: dir, mats: sess.Materials}
	meta, err := readMeta(dir)
	switch {
	case err != nil:
		return nil, err
	case meta == nil:
		if err := emptyBlocks(dir); err != nil {
			return nil, err
		}
		m := save.Meta{Seed: sess.Seed, Width: sess.Width, Height: sess.Height}
		if err := writeFile(filepath.Join(dir, worldFile), func(w io.Writer) error { return save.WriteMeta(w, m) }); err != nil {
			return nil, err
		}
		return &out, nil
	case fixed:
		return nil, fmt.Errorf("app: %s holds a world, which sets the seed and the size; drop -seed, -width and -height, or pick another -save folder", dir)
	}
	out.Seed, out.Width, out.Height = meta.Seed, meta.Width, meta.Height
	out.ViewWidth, out.ViewHeight = min(viewWidth, out.Width), min(viewHeight, out.Height)
	out.Resume, err = readSession(dir, meta.Tick, sess.Materials)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// checkWritable fails now if the save in dir could not be written later:
// if one of its files is read-only, which Windows will not replace, or a
// folder refuses new files. Finding out on exit would lose the session or
// save half of it (pitfalls.md).
func checkWritable(dir string) error {
	for _, name := range []string{worldFile, sessionFile} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		switch {
		case err != nil:
		case info.IsDir():
			err = errors.New("it is a folder")
		case info.Mode().Perm()&0o200 == 0:
			err = errors.New("it is read-only")
		}
		if err == nil {
			// Opening without O_TRUNC leaves the file as it is.
			var f *os.File
			if f, err = os.OpenFile(path, os.O_WRONLY, 0); err == nil {
				err = f.Close()
			}
		}
		if err != nil {
			return fmt.Errorf("app: -save: cannot write %s: %w", path, err)
		}
	}
	blocks := filepath.Join(dir, blocksDir)
	entries, err := os.ReadDir(blocks)
	if err != nil {
		return fmt.Errorf("app: -save: %w", err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("app: -save: %w", err)
		}
		if info.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("app: -save: %s is read-only", filepath.Join(blocks, e.Name()))
		}
	}
	for _, d := range []string{dir, blocks} {
		f, err := os.CreateTemp(d, "check.*.tmp")
		if err != nil {
			return fmt.Errorf("app: -save: %w", err)
		}
		if err := errors.Join(f.Close(), os.Remove(f.Name())); err != nil {
			return fmt.Errorf("app: -save: %w", err)
		}
	}
	return nil
}

// readMeta reads the world header of dir, or returns nil if there is none.
func readMeta(dir string) (*save.Meta, error) {
	path := filepath.Join(dir, worldFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	m, err := save.ReadMeta(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", path, err)
	}
	return &m, nil
}

// emptyBlocks fails if the blocks folder of dir, which has no world
// header, holds files: they belong to a world nothing says anything about.
func emptyBlocks(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, blocksDir))
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("app: %s has blocks but no %s; pick another -save folder", dir, worldFile)
	}
	return nil
}

// readSession reads the session file of dir into a game.Resume at tick.
// Without one, after a session that did not end, the player starts as
// new, where the world's middle is.
func readSession(dir string, tick uint32, mats *material.Table) (*game.Resume, error) {
	path := filepath.Join(dir, sessionFile)
	r := &game.Resume{Tick: tick, Slot: 1, Inventory: make([]int, mats.Len())}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	p, err := save.ReadSession(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", path, err)
	}
	r.Slot, r.CameraX, r.CameraY, r.HasCamera, r.Blocks = p.Slot, p.CameraX, p.CameraY, true, p.Blocks
	for name, n := range p.Held {
		id, ok := mats.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("app: %s: the player holds %q, which the material table does not have", path, name)
		}
		r.Inventory[id] = n
	}
	return r, nil
}

// writeSave puts the session into dir: every block in memory, then the
// world header at the current tick and the session with the camera at
// (camX, camY). The blocks go first, so a header never runs ahead of its
// blocks. The header and the session are encoded before anything is
// written, so a failure to encode them cannot leave half a save. The
// session's blocks stay in memory; it may go on.
func writeSave(dir string, g *game.Game, camX, camY int) error {
	w := g.World()
	var meta, session bytes.Buffer
	if err := save.WriteMeta(&meta, save.Meta{Seed: w.Seed(), Width: w.Width(), Height: w.Height(), Tick: w.Tick()}); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	p := &save.Session{CameraX: camX, CameraY: camY, Slot: g.Slot(), Held: map[string]int{}, Blocks: g.ResidentBlocks()}
	for id := range g.Materials().Len() {
		if n := g.Inventory(material.ID(id)); n > 0 {
			p.Held[g.Materials().Def(material.ID(id)).Name] = n
		}
	}
	if err := save.WriteSession(&session, p); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	if err := g.SaveBlocks(); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, worldFile), func(out io.Writer) error {
		_, err := out.Write(meta.Bytes())
		return err
	}); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, sessionFile), func(out io.Writer) error {
		_, err := out.Write(session.Bytes())
		return err
	})
}
