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

	// CompiledPatterns is filled by the loader (not YAML).
	CompiledPatterns []*regexp.Regexp `yaml:"-"`
}

type ProviderConfig struct {
	Name     string `yaml:"name"`
	Endpoint string `yaml:"endpoint"`
}

type Normalization struct {
	Trim        bool   `yaml:"trim"`
	Uppercase   bool   `yaml:"uppercase"`
	RemoveChars string `yaml:"remove_chars"`
}

type Validation struct {
	Patterns       []string `yaml:"patterns"`
	RequiredFields []string `yaml:"required_fields"`
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
