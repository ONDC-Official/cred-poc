package credential

import (
	"fmt"

	credconfig "credential-service/internal/credential/config"
	"credential-service/internal/service/client"
)

type VerifierRegistry struct {
	verifiers map[string]Verifier
	catalog   *credconfig.Catalog
}

// NewVerifierRegistry builds a registry from a loaded credential-type catalog.
func NewVerifierRegistry(digioClient *client.DigioClient, catalog *credconfig.Catalog) (*VerifierRegistry, error) {
	if catalog == nil {
		return nil, fmt.Errorf("credential type catalog is required")
	}

	verifiers := make(map[string]Verifier, len(catalog.All()))
	for _, def := range catalog.All() {
		v, err := NewVerifierFromDefinition(def, digioClient)
		if err != nil {
			return nil, fmt.Errorf("build verifier for %s: %w", def.CredentialType, err)
		}
		verifiers[def.CredentialType] = v
	}

	return &VerifierRegistry{
		verifiers: verifiers,
		catalog:   catalog,
	}, nil
}

// LoadCatalog loads YAML definitions from dir using the built-in transformer registries.
func LoadCatalog(dir string) (*credconfig.Catalog, error) {
	return credconfig.LoadDir(dir, KnownRequestTransformerNames(), KnownResponseTransformerNames())
}

func (r *VerifierRegistry) Resolve(credType string) (Verifier, error) {
	v, ok := r.verifiers[credType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCredType, credType)
	}
	return v, nil
}

// IssuerFor returns the issuer code from the credential-type definition.
func (r *VerifierRegistry) IssuerFor(credType string) string {
	if r == nil || r.catalog == nil {
		return ""
	}
	return r.catalog.IssuerFor(credType)
}

// Catalog exposes the loaded definitions (read-only usage).
func (r *VerifierRegistry) Catalog() *credconfig.Catalog {
	if r == nil {
		return nil
	}
	return r.catalog
}

var ErrUnsupportedCredType = fmt.Errorf("unsupported credential type")
