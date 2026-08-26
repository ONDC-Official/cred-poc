package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Catalog is an immutable set of provider definitions keyed by name.
type Catalog struct {
	byName map[string]*Definition
}

// Get returns the definition for provider name, or nil.
func (c *Catalog) Get(name string) *Definition {
	if c == nil {
		return nil
	}
	return c.byName[strings.ToLower(strings.TrimSpace(name))]
}

// All returns all loaded definitions.
func (c *Catalog) All() []*Definition {
	if c == nil {
		return nil
	}
	out := make([]*Definition, 0, len(c.byName))
	for _, d := range c.byName {
		out = append(out, d)
	}
	return out
}

// LoadDir loads and validates all *.yaml / *.yml files in dir.
func LoadDir(dir string) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read providers dir %q: %w", dir, err)
	}

	catalog := &Catalog{byName: make(map[string]*Definition)}
	loaded := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, name)
		def, err := loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateDefinition(def); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		key := strings.ToLower(def.Name)
		if _, exists := catalog.byName[key]; exists {
			return nil, fmt.Errorf("%s: duplicate provider name %q", path, def.Name)
		}
		catalog.byName[key] = def
		loaded++
	}

	if loaded == 0 {
		return nil, fmt.Errorf("providers dir %q contains no YAML definitions", dir)
	}
	return catalog, nil
}

func loadFile(path string) (*Definition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def Definition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return &def, nil
}

func validateDefinition(def *Definition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if def.Version < 1 {
		return fmt.Errorf("version must be >= 1")
	}
	if len(def.Capabilities) == 0 {
		return fmt.Errorf("capabilities must not be empty")
	}
	for id, cap := range def.Capabilities {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("capabilities: capability id must not be empty")
		}
		method := strings.ToUpper(strings.TrimSpace(cap.Method))
		if method == "" {
			return fmt.Errorf("capabilities.%s.method is required", id)
		}
		path := strings.TrimSpace(cap.Path)
		if path == "" {
			return fmt.Errorf("capabilities.%s.path is required", id)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("capabilities.%s.path must start with /", id)
		}
		cap.Method = method
		cap.Path = path
		def.Capabilities[id] = cap
	}
	return nil
}
