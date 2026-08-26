package credential

import (
	"fmt"
	"os"
	"path/filepath"

	"credential-service/internal/provider"
)

// FindDefinitionsDir locates configs/credential-types relative to common
// working directories (module root or internal/* package dirs).
func FindDefinitionsDir() (string, error) {
	return findConfigDir("configs/credential-types")
}

// FindProvidersDir locates configs/providers relative to common working directories.
func FindProvidersDir() (string, error) {
	return findConfigDir("configs/providers")
}

func findConfigDir(rel string) (string, error) {
	candidates := []string{
		rel,
		filepath.Join("..", "..", rel),
		filepath.Join("..", rel),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("could not find %s", rel)
}

// NewRegistryFromDir loads credential-type YAML and builds a verifier registry
// against the given provider gateway.
func NewRegistryFromDir(typesDir string, gateway *provider.Gateway) (*VerifierRegistry, error) {
	catalog, err := LoadCatalog(typesDir)
	if err != nil {
		return nil, err
	}
	return NewVerifierRegistry(gateway, catalog)
}

// NewGatewayFromProvidersDir loads provider YAML and binds runtime callers.
func NewGatewayFromProvidersDir(providersDir string, callers map[string]provider.Caller) (*provider.Gateway, error) {
	catalog, err := provider.LoadDir(providersDir)
	if err != nil {
		return nil, err
	}
	return provider.NewGateway(catalog, callers)
}
