// Command noitaref looks things up in the Noita analysis that lives outside
// this repository, so an agent reads what the analysis says instead of
// relying on memory. It prints analysis text for reading only: never copy
// it, or the code addresses and pseudocode it mentions, into the repository.
//
//	go run ./tools/noitaref section C49 C50     sections of COMPLETION_PROGRESS.txt
//	go run ./tools/noitaref doc MATERIAL_TICK   a whole clone-spec or REPORT document
//	go run ./tools/noitaref material water sand the engine's final values
//	go run ./tools/noitaref -fields liquid_,density material water
//	go run ./tools/noitaref search 충격 통계       lines that mention every word
//	go run ./tools/noitaref model native_probe_liquid_grid
//	                                            an executable behavior model (tools/*.py)
//
// The analysis is found at -root, else $NOITA_ANALYSIS, else
// C:\Users\tongm\Downloads\noita-analysis.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultRoot = `C:\Users\tongm\Downloads\noita-analysis`

func main() {
	root := flag.String("root", "", "the analysis folder")
	fields := flag.String("fields", "", "material: comma-separated field name prefixes to show (default: all)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: noitaref [-root dir] [-fields p1,p2] section|doc|material|search args...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if err := run(os.Stdout, analysisRoot(*root), *fields, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "noitaref:", err)
		os.Exit(1)
	}
}

func analysisRoot(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("NOITA_ANALYSIS"); env != "" {
		return env
	}
	return defaultRoot
}

func run(out io.Writer, root, fields string, args []string) error {
	if len(args) < 2 {
		return errors.New("need a command and at least one argument (see -h)")
	}
	if _, err := os.Stat(filepath.Join(root, "clone-spec")); err != nil {
		return fmt.Errorf("no analysis at %s (set -root or NOITA_ANALYSIS): %w", root, err)
	}
	p := &printer{w: out}
	var err error
	switch args[0] {
	case "section":
		err = printSections(p, root, args[1:])
	case "doc":
		err = printDoc(p, root, args[1])
	case "material":
		err = printMaterials(p, root, splitList(fields), args[1:])
	case "search":
		err = search(p, root, args[1:])
	case "model":
		err = printModel(p, root, args[1])
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return err
	}
	return p.err
}

// printer writes formatted output and remembers the first write error, so
// the commands can print freely and report a failure once.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

func splitList(s string) []string {
	var parts []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}
