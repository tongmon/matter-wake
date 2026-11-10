package material

import (
	"os"
	"path/filepath"
	"slices"
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
	if water := tab.Def(tab.MustLookup("water")); water.FallAccel.Float() != 0.375 {
		t.Errorf("water fallAccel = %v, want 0.375", water.FallAccel)
	}
	// Every material a tool may dig says how hard it is to dig.
	for id := range tab.Len() {
		if d := tab.Def(ID(id)); d.Kind.Diggable() && d.HP == 0 {
			t.Errorf("%s is diggable but has no hp", d.Name)
		}
	}
}

func TestDiggable(t *testing.T) {
	want := map[Kind]bool{Empty: false, Static: true, Powder: true, Liquid: false, Solid: true}
	for k, dig := range want {
		if k.Diggable() != dig {
			t.Errorf("%v.Diggable() = %v, want %v", k, !dig, dig)
		}
	}
}

func TestParseDigValues(t *testing.T) {
	tab, err := Parse(strings.NewReader(`{"materials":[{"name":"air","kind":"empty"},
		{"name":"gold","kind":"static","hardness":12,"hp":4000000000}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if d := tab.Def(1); d.Hardness != 12 || d.HP != 4000000000 {
		t.Fatalf("gold has hardness %d and hp %d, want 12 and 4000000000", d.Hardness, d.HP)
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
		{"hardness on liquid", `{"materials":[{"name":"air","kind":"empty"},{"name":"water","kind":"liquid","hardness":1}]}`, "only powders, statics and solids"},
		{"hp on air", `{"materials":[{"name":"air","kind":"empty","hp":5}]}`, "only powders, statics and solids"},
		{"negative hp", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","hp":-1}]}`, "hp"},
		{"hardness over 255", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","hardness":256}]}`, "hardness"},
		{"accel not a number", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","fallAccel":"fast"}]}`, "not a number"},
		{"friction on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"stone","kind":"static","friction":0.5}]}`, "only solids"},
		{"restitution on sand", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder","restitution":0.5}]}`, "only solids"},
		{"solid without density", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid"}]}`, "density"},
		{"negative friction", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"friction":-1}]}`, "friction"},
		{"restitution over 1", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"restitution":1.5}]}`, "restitution"},
		{"buoyancy on water", `{"materials":[{"name":"air","kind":"empty"},{"name":"water","kind":"liquid","buoyancy":1}]}`, "only solids"},
		{"negative buoyancy", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"buoyancy":-0.5}]}`, "buoyancy"},
		{"fall on wood", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"fallAccel":0.5}]}`, "only powders and liquids"},
		{"body on sand", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6},{"name":"sand","kind":"powder","body":"wood"}]}`, "only statics"},
		{"body unknown", `{"materials":[{"name":"air","kind":"empty"},{"name":"ice","kind":"static","body":"ice_body"}]}`, "not a solid"},
		{"body not solid", `{"materials":[{"name":"air","kind":"empty"},{"name":"ice","kind":"static","body":"stone"},{"name":"stone","kind":"static"}]}`, "not a solid"},
		{"anchor on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"stone","kind":"static","anchor":"top"}]}`, "only solids"},
		{"unknown anchor", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"anchor":"middle"}]}`, "unknown anchor"},
		{"empty anchor", `{"materials":[{"name":"air","kind":"empty"},{"name":"wood","kind":"solid","density":6,"anchor":""}]}`, "unknown anchor"},
		{"shatter on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder"},{"name":"stone","kind":"static","shatter":"sand"}]}`, "only solids"},
		{"shatter to water", `{"materials":[{"name":"air","kind":"empty"},{"name":"water","kind":"liquid"},{"name":"glass","kind":"solid","density":6,"shatter":"water"}]}`, "not a powder"},
		{"cracks on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"stone","kind":"static","cracks":true}]}`, "only solids"},
		{"sleepTo on stone", `{"materials":[{"name":"air","kind":"empty"},{"name":"stone","kind":"static","sleepTo":"stone"}]}`, "only solids"},
		{"sleepTo sand", `{"materials":[{"name":"air","kind":"empty"},{"name":"sand","kind":"powder"},{"name":"ice","kind":"solid","density":6,"sleepTo":"sand"}]}`, "not a static"},
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

func TestLinks(t *testing.T) {
	tab, err := Parse(strings.NewReader(`{"materials":[{"name":"air","kind":"empty"},
		{"name":"ice","kind":"static","body":"ice_body"},
		{"name":"ice_body","kind":"solid","density":6,"anchor":"top","shatter":"powder","cracks":true,"sleepTo":"ice"},
		{"name":"powder","kind":"powder"},
		{"name":"wood","kind":"solid","density":6,"anchor":"bottom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ice, body, powder, wood := tab.MustLookup("ice"), tab.MustLookup("ice_body"), tab.MustLookup("powder"), tab.MustLookup("wood")
	if tab.BodyOf(ice) != body || tab.BodyOf(body) != Air || tab.BodyOf(powder) != Air {
		t.Errorf("BodyOf: ice %d, ice_body %d, powder %d; want %d, %d, %d", tab.BodyOf(ice), tab.BodyOf(body), tab.BodyOf(powder), body, Air, Air)
	}
	if tab.ShatterOf(body) != powder || tab.ShatterOf(wood) != Air {
		t.Errorf("ShatterOf: ice_body %d, wood %d; want %d, %d", tab.ShatterOf(body), tab.ShatterOf(wood), powder, Air)
	}
	if tab.SleepOf(body) != ice || tab.SleepOf(wood) != Air || !tab.Def(body).Cracks || tab.Def(wood).Cracks {
		t.Errorf("SleepOf: ice_body %d, wood %d; cracks %t, %t; want %d, %d, true, false",
			tab.SleepOf(body), tab.SleepOf(wood), tab.Def(body).Cracks, tab.Def(wood).Cracks, ice, Air)
	}
	if tab.Def(body).Anchor != AnchorTop || tab.Def(wood).Anchor != AnchorBottom || tab.Def(ice).Anchor != NoAnchor {
		t.Errorf("anchors %v, %v, %v; want top, bottom and none", tab.Def(body).Anchor, tab.Def(wood).Anchor, tab.Def(ice).Anchor)
	}
}

// The fields bodies come of are left out of a table's fingerprint when
// unset, so a table that sets none keeps the fingerprint it had before
// they were added, and the replays recorded with it give no warning.
func TestLinksLeaveOldFingerprints(t *testing.T) {
	// The table internal/replay/testdata held before ice and glass, which
	// the replays there were recorded with.
	tab, err := Parse(strings.NewReader(`{"materials": [
		{"name": "air", "kind": "empty", "density": 0, "color": "#00000000"},
		{"name": "stone", "kind": "static", "density": 255, "hardness": 10, "hp": 100000, "color": "#6b6a72", "toneSpread": 40},
		{"name": "sand", "kind": "powder", "density": 30, "fallAccel": 0.225, "sinkChance": 25, "slideChance": 50,
		 "hardness": 4, "hp": 800, "color": "#e0c27a", "toneSpread": 50},
		{"name": "water", "kind": "liquid", "density": 20, "fallAccel": 0.375, "sinkChance": 50,
		 "flowSpeed": 0.9, "flowDamping": 0.8, "dispersion": 4, "color": "#3a7bd5c8", "toneSpread": 20},
		{"name": "wood", "kind": "solid", "density": 6, "hardness": 8, "hp": 5000,
		 "friction": 0.9, "restitution": 0.2, "buoyancy": 0.7, "color": "#8a5a32", "toneSpread": 40}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tab.Fingerprint(), "cbe9996c5ef09a49"; got != want {
		t.Fatalf("fingerprint %s, want %s", got, want)
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

// A replay keeps the fingerprint of the table it was recorded with, so the
// fingerprint must follow every value and nothing else.
func TestFingerprint(t *testing.T) {
	defs := []Def{
		{Name: "air", Kind: Empty},
		{Name: "sand", Kind: Powder, Density: 30, FallAccel: 64, SlideChance: 50, Color: Color{1, 2, 3, 255}},
		{Name: "glass", Kind: Solid, Density: 6},
		{Name: "pane", Kind: Static},
	}
	tab, err := NewTable(defs)
	if err != nil {
		t.Fatal(err)
	}
	fp := tab.Fingerprint()
	if len(fp) != 16 || strings.Trim(fp, "0123456789abcdef") != "" {
		t.Fatalf("fingerprint %q is not 16 lowercase hex digits", fp)
	}
	same, err := NewTable(defs)
	if err != nil {
		t.Fatal(err)
	}
	if same.Fingerprint() != fp {
		t.Fatal("equal tables have different fingerprints")
	}
	edits := []struct {
		name string
		edit func(d []Def) []Def
	}{
		{"name", func(d []Def) []Def { d[1].Name = "dust"; return d }},
		{"density", func(d []Def) []Def { d[1].Density++; return d }},
		{"fallAccel", func(d []Def) []Def { d[1].FallAccel++; return d }},
		{"slideChance", func(d []Def) []Def { d[1].SlideChance++; return d }},
		{"color", func(d []Def) []Def { d[1].Color.R++; return d }},
		{"hardness", func(d []Def) []Def { d[1].Hardness++; return d }},
		{"hp", func(d []Def) []Def { d[1].HP++; return d }},
		{"another material", func(d []Def) []Def { return append(d, Def{Name: "stone", Kind: Static}) }},
		{"shatter", func(d []Def) []Def { d[2].Shatter = "sand"; return d }},
		{"anchor", func(d []Def) []Def { d[2].Anchor = AnchorLargest; return d }},
		{"body", func(d []Def) []Def { d[3].Body = "glass"; return d }},
		{"cracks", func(d []Def) []Def { d[2].Cracks = true; return d }},
		{"sleepTo", func(d []Def) []Def { d[2].SleepTo = "pane"; return d }},
	}
	for _, e := range edits {
		other, err := NewTable(e.edit(slices.Clone(defs)))
		if err != nil {
			t.Fatal(err)
		}
		if other.Fingerprint() == fp {
			t.Errorf("changing the %s kept the fingerprint", e.name)
		}
	}
}
