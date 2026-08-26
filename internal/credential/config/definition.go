package config

import "regexp"

// Definition is the declarative credential-type config loaded from YAML.
type Definition struct {
	CredentialType string         `yaml:"credential_type"`
	Version        int            `yaml:"version"`
	Issuer         string         `yaml:"issuer"`
	Provider       ProviderConfig `yaml:"provider"`
	Normalization  Normalization  `yaml:"normalization"`
	Validation     Validation     `yaml:"validation"`
	Request        RequestConfig  `yaml:"request"`
	Response       ResponseConfig `yaml:"response"`
}

// ProviderConfig selects a runtime provider + named capability from
// configs/providers. Paths and methods live in the provider catalog, not here.
type ProviderConfig struct {
	Name       string `yaml:"name"`
	Capability string `yaml:"capability"`
}

type Normalization struct {
	Trim        bool   `yaml:"trim"`
	Uppercase   bool   `yaml:"uppercase"`
	RemoveChars string `yaml:"remove_chars"`
}

// Validation holds optional per-field rules. Fields not listed are not validated.
type Validation struct {
	Fields map[string]FieldValidation `yaml:"fields"`
}

// FieldValidation declares optional required / pattern checks for one cred_data key.
type FieldValidation struct {
	Required bool     `yaml:"required"`
	Patterns []string `yaml:"patterns"`
	// Message overrides the default "invalid <field> format" error text.
	Message string `yaml:"message"`

	// CompiledPatterns is filled by the loader (not YAML).
	CompiledPatterns []*regexp.Regexp `yaml:"-"`
}

type RequestConfig struct {
	Fields      map[string]string `yaml:"fields"`
	Transformer string            `yaml:"transformer"`
}

type ResponseConfig struct {
	Transformer   string            `yaml:"transformer"`
	SuccessField  string            `yaml:"success_field"`
	SoftFailField string            `yaml:"soft_fail_field"`
	Fields        map[string]string `yaml:"fields"`
}

// Special request field source: post-normalization credential id.
const FieldSourceNormalizedID = "normalized_id"
