package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// materialsFile holds every material's values as the engine loaded them,
// after inheritance, which can differ from the declarations in the XML.
const materialsFile = "analysis-detail/runtime-completion/materials-expanded-analysis.json"

type materialRecord struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Values  map[string]any    `json:"values"`
	Strings map[string]string `json:"strings"`
	Tags    []string          `json:"tags"`
}

// printMaterials prints the named materials' final values, optionally only
// the fields that start with one of prefixes.
func printMaterials(p *printer, root string, prefixes, names []string) error {
	data, err := os.ReadFile(filepath.Join(root, materialsFile))
	if err != nil {
		return err
	}
	var file struct {
		Materials []materialRecord `json:"materials"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("%s: %w", materialsFile, err)
	}
	for _, name := range names {
		i := slices.IndexFunc(file.Materials, func(m materialRecord) bool { return m.Name == name })
		if i < 0 {
			return fmt.Errorf("no material %q in %s (try: search %s)", name, materialsFile, name)
		}
		printMaterial(p, &file.Materials[i], prefixes)
	}
	return nil
}

func printMaterial(p *printer, m *materialRecord, prefixes []string) {
	p.printf("== %s (id %d) from %s\n", m.Name, m.ID, materialsFile)
	if len(prefixes) == 0 {
		p.printf("tags: %s\n", strings.Join(m.Tags, " "))
	}
	fields := make([]string, 0, len(m.Values)+len(m.Strings))
	values := make(map[string]string, cap(fields))
	for k, v := range m.Values {
		values[k] = fmt.Sprint(v)
	}
	for k, v := range m.Strings {
		if v != "" {
			values[k] = fmt.Sprintf("%q", v)
		}
	}
	for k := range values {
		if matchesPrefix(k, prefixes) {
			fields = append(fields, k)
		}
	}
	slices.Sort(fields)
	for _, k := range fields {
		p.printf("%s = %s\n", k, values[k])
	}
}

func matchesPrefix(field string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(field, p) {
			return true
		}
	}
	return false
}
