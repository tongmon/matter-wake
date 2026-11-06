package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// progressFile holds the numbered sections C1, C2, ... of the analysis.
const progressFile = "clone-spec/COMPLETION_PROGRESS.txt"

// sectionHeading matches "C49. Title" at the start of a line.
var sectionHeading = regexp.MustCompile(`^C(\d+)\. `)

// printSections prints each requested section, from its heading up to the
// next section heading.
func printSections(p *printer, root string, ids []string) error {
	lines, err := readLines(filepath.Join(root, progressFile))
	if err != nil {
		return err
	}
	for _, id := range ids {
		want := strings.TrimPrefix(strings.ToUpper(id), "C")
		start := -1
		for i, l := range lines {
			if m := sectionHeading.FindStringSubmatch(l); m != nil {
				if start >= 0 {
					printLines(p, progressFile, lines[start:i], start)
					start = -2
					break
				}
				if m[1] == want {
					start = i
				}
			}
		}
		switch start {
		case -1:
			return fmt.Errorf("no section C%s in %s", want, progressFile)
		case -2:
		default: // the last section runs to the end of the file
			printLines(p, progressFile, lines[start:], start)
		}
	}
	return nil
}

// printDoc prints clone-spec/NAME.txt, or NAME.txt at the analysis root
// (the REPORT_* files).
func printDoc(p *printer, root, name string) error {
	name = strings.TrimSuffix(name, ".txt") + ".txt"
	for _, rel := range []string{filepath.Join("clone-spec", name), name} {
		lines, err := readLines(filepath.Join(root, rel))
		if err == nil {
			printLines(p, filepath.ToSlash(rel), lines, 0)
			return nil
		}
	}
	return fmt.Errorf("no document %s in clone-spec/ or the analysis root", name)
}

// printModel prints tools/NAME.py, one of the analysis's executable models
// of a mechanic. The models were checked against the original, so they show
// the exact order of a rule's branches. They also hold code addresses:
// read them, never port them.
func printModel(p *printer, root, name string) error {
	rel := filepath.Join("tools", strings.TrimSuffix(name, ".py")+".py")
	lines, err := readLines(filepath.Join(root, rel))
	if err != nil {
		return fmt.Errorf("no model %s: %w", filepath.ToSlash(rel), err)
	}
	printLines(p, filepath.ToSlash(rel), lines, 0)
	return nil
}

// search prints every line of the clone-spec and REPORT documents that
// contains all the words, ignoring case.
func search(p *printer, root string, words []string) error {
	var files []string
	for _, pattern := range []string{"clone-spec/*.txt", "*.txt"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return err
		}
		files = append(files, matches...)
	}
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	hits := 0
	for _, f := range files {
		lines, err := readLines(f)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return err
		}
		section := ""
		for i, l := range lines {
			if sectionHeading.MatchString(l) {
				section = strings.SplitN(l, " ", 2)[0]
			}
			if containsAll(strings.ToLower(l), words) {
				p.printf("%s:%d %s %s\n", filepath.ToSlash(rel), i+1, section, strings.TrimSpace(l))
				hits++
			}
		}
	}
	if hits == 0 {
		p.printf("no matches\n")
	}
	return nil
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// printLines prints lines with their 1-based line numbers in file, so a
// brief can cite them.
func printLines(p *printer, file string, lines []string, first int) {
	p.printf("== %s:%d\n", file, first+1)
	for i, l := range lines {
		p.printf("%5d  %s\n", first+i+1, l)
	}
}
