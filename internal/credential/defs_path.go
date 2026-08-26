package credential

import (
	"fmt"
	"os"
	"path/filepath"

	"credential-service/internal/service/client"
)

// FindDefinitionsDir locates configs/credential-types relative to common
// working directories (module root or internal/* package dirs).
func FindDefinitionsDir() (string, error) {
	candidates := []string{
		"configs/credential-types",
		filepath.Join("..", "..", "configs", "credential-types"),
		filepath.Join("..", "configs", "credential-types"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("could not find configs/credential-types")
}

// NewRegistryFromDir loads YAML definitions from dir and builds a verifier registry.
func NewRegistryFromDir(dir string, digioClient *client.DigioClient) (*VerifierRegistry, error) {
	catalog, err := LoadCatalog(dir)
	if err != nil {
		return nil, err
	}
	return NewVerifierRegistry(digioClient, catalog)
}
