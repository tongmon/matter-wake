package material

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedTableLoads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "materials.json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	// The program looks these up by name; the data must keep them.
	for _, name := range []string{"air", "stone", "sand", "water"} {
		if _, ok := tab.Lookup(name); !ok {
			t.Errorf("shipped table lacks %q", name)
		}
	}
	if id := tab.MustLookup("air"); id != Air {
		t.Errorf("air has ID %d, want %d", id, Air)
	}
}

func TestParseRejectsBadTables(t *testing.T) {
	tests := []struct {
		name, json, wantErr string
	}{
		{"unknown field", `{"materials":[{"name":"air","kind":"empty","colour":"#000000"}]}`, "unknown field"},
		{"trailing data", `{"materials":[{"name":"air","kind":"empty"}]} {}`, "trailing data"},
		{"trailing bracket", `{"materials":[{"name":"air","kind":"empty"}]} ]`, "trailing data"},
		{"trailing brace", `{"materials":[{"name":"air","kind":"empty"}]}}`, "trailing data"},
		{"empty table", `{"materials":[]}`, "empty"},
		{"first not empty", `{"materials":[{"name":"stone","kind":"static"}]}`, "must be kind empty"},
		{"unknown kind", `{"materials":[{"name":"air","kind":"plasma"}]}`, "unknown kind"},
		{"duplicate", `{"materials":[{"name":"air","kind":"empty"},{"name":"air","kind":"static"}]}`, "duplicate"},
		{"missing name", `{"materials":[{"name":"air","kind":"empty"},{"kind":"static"}]}`, "missing name"},
		{"bad color", `{"materials":[{"name":"air","kind":"empty","color":"red"}]}`, "color"},
		{"dispersion on powder", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","dispersion":2}]}`, "only liquids"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.json))
			if err == nil {
				t.Fatalf("Parse accepted %s", tt.json)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestColorParsing(t *testing.T) {
	var c Color
	if err := c.UnmarshalText([]byte("#102030")); err != nil {
		t.Fatal(err)
	}
	if c != (Color{0x10, 0x20, 0x30, 0xff}) {
		t.Fatalf("got %+v", c)
	}
	if err := c.UnmarshalText([]byte("#10203040")); err != nil {
		t.Fatal(err)
	}
	if c != (Color{0x10, 0x20, 0x30, 0x40}) {
		t.Fatalf("got %+v", c)
	}
}

func TestKindString(t *testing.T) {
	if Liquid.String() != "liquid" || Kind(200).String() != "Kind(200)" {
		t.Fatal("Kind.String is wrong")
	}
}
