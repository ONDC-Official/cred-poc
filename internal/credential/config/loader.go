package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Catalog is an immutable set of credential-type definitions keyed by type.
type Catalog struct {
	byType map[string]*Definition
}

// Get returns the definition for credType, or nil.
func (c *Catalog) Get(credType string) *Definition {
	if c == nil {
		return nil
	}
	return c.byType[credType]
}

// IssuerFor returns the configured issuer code for credType, or "".
func (c *Catalog) IssuerFor(credType string) string {
	def := c.Get(credType)
	if def == nil {
		return ""
	}
	return def.Issuer
}

// All returns all loaded definitions.
func (c *Catalog) All() []*Definition {
	if c == nil {
		return nil
	}
	out := make([]*Definition, 0, len(c.byType))
	for _, d := range c.byType {
		out = append(out, d)
	}
	return out
}

// Types returns registered credential type names.
func (c *Catalog) Types() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.byType))
	for k := range c.byType {
		out = append(out, k)
	}
	return out
}

// LoadDir loads and validates all *.yaml / *.yml files in dir.
// Fails if the directory is missing, empty, or any file is invalid.
// Duplicate credential_type values are rejected.
func LoadDir(dir string, knownRequestTransformers, knownResponseTransformers map[string]struct{}) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read credential types dir %q: %w", dir, err)
	}

	catalog := &Catalog{byType: make(map[string]*Definition)}
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
		if err := validateDefinition(def, knownRequestTransformers, knownResponseTransformers); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if _, exists := catalog.byType[def.CredentialType]; exists {
			return nil, fmt.Errorf("%s: duplicate credential_type %q", path, def.CredentialType)
		}
		catalog.byType[def.CredentialType] = def
		loaded++
	}

	if loaded == 0 {
		return nil, fmt.Errorf("credential types dir %q contains no YAML definitions", dir)
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

func validateDefinition(def *Definition, knownReq, knownResp map[string]struct{}) error {
	if strings.TrimSpace(def.CredentialType) == "" {
		return fmt.Errorf("credential_type is required")
	}
	if def.Version < 1 {
		return fmt.Errorf("version must be >= 1")
	}
	if strings.TrimSpace(def.Issuer) == "" {
		return fmt.Errorf("issuer is required")
	}
	if len(def.Providers) == 0 {
		return fmt.Errorf("providers must not be empty")
	}
	for i, p := range def.Providers {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("providers[%d].name is required", i)
		}
		if strings.TrimSpace(p.Capability) == "" {
			return fmt.Errorf("providers[%d].capability is required", i)
		}
	}
	for fieldName := range def.Validation.Fields {
		if strings.TrimSpace(fieldName) == "" {
			return fmt.Errorf("validation.fields: field name must not be empty")
		}
	}

	reqX := strings.TrimSpace(def.Request.Transformer)
	if reqX != "" {
		if _, ok := knownReq[reqX]; !ok {
			return fmt.Errorf("request.transformer %q is unknown", reqX)
		}
	} else if len(def.Request.Fields) == 0 {
		return fmt.Errorf("request.fields is required when request.transformer is empty")
	}

	respX := strings.TrimSpace(def.Response.Transformer)
	if respX != "" {
		if _, ok := knownResp[respX]; !ok {
			return fmt.Errorf("response.transformer %q is unknown", respX)
		}
	} else if strings.TrimSpace(def.Response.SuccessField) == "" {
		return fmt.Errorf("response.success_field is required when response.transformer is empty")
	}

	return nil
}
