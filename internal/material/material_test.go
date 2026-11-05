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
	// Motion values arrive as fixed-point numbers.
	if water := tab.Def(tab.MustLookup("water")); water.FlowSpeed.Float() != 0.5 {
		t.Errorf("water flowSpeed = %v, want 0.5", water.FlowSpeed)
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
		{"flow on powder", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","flowSpeed":0.5}]}`, "only liquids"},
		{"slide on liquid", `{"materials":[{"name":"air","kind":"empty"},{"name":"water","kind":"liquid","slideChance":10}]}`, "only powders may"},
		{"fall on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"stone","kind":"static","fallAccel":0.25}]}`, "only powders and liquids"},
		{"sink over 100", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","sinkChance":101}]}`, "percents"},
		{"negative accel", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","fallAccel":-1}]}`, "fallAccel"},
		{"damping over 1", `{"materials":[{"name":"air","kind":"empty"},{"name":"water","kind":"liquid","flowDamping":1.5}]}`, "flowDamping"},
		{"accel not a number", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","fallAccel":"fast"}]}`, "not a number"},
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
