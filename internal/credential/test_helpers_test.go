package credential_test

import (
	"context"
	"testing"

	"credential-service/internal/credential"
	"credential-service/internal/provider"
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

func testProvidersDir(t *testing.T) string {
	t.Helper()
	dir, err := credential.FindProvidersDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func testGateway(t *testing.T, digioClient *client.DigioClient) *provider.Gateway {
	t.Helper()
	// Every catalogued provider (including "mock", used for PAN's fallback
	// chain) needs a registered Caller for the gateway to build, even in
	// tests that never actually invoke it.
	callers := map[string]provider.Caller{
		"mock": noopCaller{},
	}
	if digioClient != nil {
		callers["digio"] = digioClient
	} else {
		// Validation-only tests still need a registered digio Caller.
		callers["digio"] = noopCaller{}
	}
	gateway, err := credential.NewGatewayFromProvidersDir(testProvidersDir(t), callers)
	if err != nil {
		t.Fatalf("NewGatewayFromProvidersDir: %v", err)
	}
	return gateway
}

type noopCaller struct{}

func (noopCaller) Call(context.Context, string, string, any) ([]byte, int, error) {
	return nil, 0, nil
}

func testRegistry(t *testing.T, digioClient *client.DigioClient) *credential.VerifierRegistry {
	t.Helper()
	registry, err := credential.NewRegistryFromDir(testDefinitionsDir(t), testGateway(t, digioClient))
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

