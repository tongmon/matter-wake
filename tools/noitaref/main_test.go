package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/analysis is a made-up miniature of the analysis layout. It holds
// no text from the real analysis.
var fixture = filepath.Join("testdata", "analysis")

func runTool(t *testing.T, fields string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(&out, fixture, fields, args)
	return out.String(), err
}

func TestSection(t *testing.T) {
	out, err := runTool(t, "", "section", "C1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha rule") || strings.Contains(out, "beta rule") {
		t.Fatalf("section C1 should hold only its own lines:\n%s", out)
	}
	if !strings.Contains(out, "COMPLETION_PROGRESS.txt:2") {
		t.Fatalf("section C1 should cite its starting line 2:\n%s", out)
	}
	// The last section runs to the end of the file; lowercase ids work.
	out, err = runTool(t, "", "section", "c2")
	if err != nil || !strings.Contains(out, "beta rule") {
		t.Fatalf("section c2: %v\n%s", err, out)
	}
	if _, err := runTool(t, "", "section", "C9"); err == nil {
		t.Fatal("a missing section was not an error")
	}
}

func TestDoc(t *testing.T) {
	out, err := runTool(t, "", "doc", "TICK_NOTES")
	if err != nil || !strings.Contains(out, "bottom to top") {
		t.Fatalf("clone-spec doc: %v\n%s", err, out)
	}
	out, err = runTool(t, "", "doc", "REPORT_FAKE.txt")
	if err != nil || !strings.Contains(out, "flowing liquids") {
		t.Fatalf("root report: %v\n%s", err, out)
	}
	if _, err := runTool(t, "", "doc", "NOPE"); err == nil {
		t.Fatal("a missing document was not an error")
	}
}

func TestMaterial(t *testing.T) {
	out, err := runTool(t, "", "material", "goo")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"goo (id 7)", "liquid_gravity = 1.25", "density = 3.5", `warmth_melts_to_material = "steam"`, "[liquid]"} {
		if !strings.Contains(out, want) {
			t.Errorf("material output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "solid_break_to_type") {
		t.Errorf("empty strings should be left out:\n%s", out)
	}
	out, err = runTool(t, "liquid_,density", "material", "goo")
	if err != nil || strings.Contains(out, "cell_type") || !strings.Contains(out, "liquid_flow_speed = 0.5") {
		t.Fatalf("-fields filter: %v\n%s", err, out)
	}
	if _, err := runTool(t, "", "material", "unobtainium"); err == nil {
		t.Fatal("a missing material was not an error")
	}
}

func TestSearch(t *testing.T) {
	out, err := runTool(t, "", "search", "RULE", "flowing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "COMPLETION_PROGRESS.txt:5 C2.") || strings.Contains(out, "alpha") {
		t.Fatalf("search should find only the line with every word, tagged with its section:\n%s", out)
	}
	out, _ = runTool(t, "", "search", "zzz")
	if !strings.Contains(out, "no matches") {
		t.Fatalf("empty search: %s", out)
	}
}

func TestModel(t *testing.T) {
	out, err := runTool(t, "", "model", "fake_model")
	if err != nil || !strings.Contains(out, "def predict") || !strings.Contains(out, "tools/fake_model.py") {
		t.Fatalf("model: %v\n%s", err, out)
	}
	if _, err := runTool(t, "", "model", "missing_model"); err == nil {
		t.Fatal("a missing model was not an error")
	}
}

func TestRunNeedsTheAnalysis(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, t.TempDir(), "", []string{"section", "C1"}); err == nil {
		t.Fatal("a folder without clone-spec/ was accepted")
	}
	if err := run(&out, fixture, "", []string{"section"}); err == nil {
		t.Fatal("a command without arguments was accepted")
	}
}
