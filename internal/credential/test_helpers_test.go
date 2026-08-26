package credential_test

import (
	"testing"

	"credential-service/internal/credential"
	"credential-service/internal/service/client"
)

func testDefinitionsDir(t *testing.T) string {
	t.Helper()
	dir, err := credential.FindDefinitionsDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func testRegistry(t *testing.T, digioClient *client.DigioClient) *credential.VerifierRegistry {
	t.Helper()
	registry, err := credential.NewRegistryFromDir(testDefinitionsDir(t), digioClient)
	if err != nil {
		t.Fatalf("NewRegistryFromDir: %v", err)
	}
	return registry
}

func testVerifier(t *testing.T, digioClient *client.DigioClient, credType string) credential.Verifier {
	t.Helper()
	v, err := testRegistry(t, digioClient).Resolve(credType)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", credType, err)
	}
	return v
}
