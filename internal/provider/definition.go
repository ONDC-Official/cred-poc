package provider

// Definition is a declarative KYC-provider catalog loaded from YAML.
type Definition struct {
	Name         string                          `yaml:"name"`
	Version      int                             `yaml:"version"`
	Capabilities map[string]CapabilityDefinition `yaml:"capabilities"`
}

// CapabilityDefinition maps a named capability to an HTTP operation.
type CapabilityDefinition struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}
