package provider_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"credential-service/internal/credential"
	"credential-service/internal/provider"
)

type stubCaller struct {
	lastMethod string
	lastPath   string
}

func (s *stubCaller) Call(_ context.Context, method, path string, _ any) ([]byte, int, error) {
	s.lastMethod = method
	s.lastPath = path
	return []byte(`{"ok":true}`), 200, nil
}

func TestLoadRepoProvidersAndInvokeDigioCapability(t *testing.T) {
	t.Parallel()

	dir, err := credential.FindProvidersDir()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := provider.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def := catalog.Get("digio")
	if def == nil {
		t.Fatal("missing digio provider")
	}
	for _, cap := range []string{
		"fetch_id_data_pan",
		"fetch_id_data_gst",
		"fetch_id_data_fssai",
		"fetch_id_data_udyam",
	} {
		if _, ok := def.Capabilities[cap]; !ok {
			t.Fatalf("digio missing capability %q", cap)
		}
	}

	stub := &stubCaller{}
	gateway, err := provider.NewGateway(catalog, map[string]provider.Caller{"digio": stub})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if _, _, err := gateway.Invoke(context.Background(), "digio", "fetch_id_data_pan", map[string]string{"id_no": "ABCDE1234F"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if stub.lastMethod != "POST" || stub.lastPath != "/v3/client/kyc/fetch_id_data/PAN" {
		t.Fatalf("unexpected call: method=%q path=%q", stub.lastMethod, stub.lastPath)
	}
}

func TestNewGatewayRequiresCallerForCataloguedProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "other.v1.yaml")
	content := []byte(`
name: other
version: 1
capabilities:
  ping:
    method: POST
    path: /ping
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := provider.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, err := provider.NewGateway(catalog, map[string]provider.Caller{}); err == nil {
		t.Fatal("expected error when caller missing")
	}
}

func TestEnsureCapabilityRejectsUnknown(t *testing.T) {
	t.Parallel()

	dir, err := credential.FindProvidersDir()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := provider.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := provider.NewGateway(catalog, map[string]provider.Caller{"digio": &stubCaller{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.EnsureCapability("digio", "no_such_cap"); err == nil {
		t.Fatal("expected unknown capability error")
	}
	if err := gateway.EnsureCapability("missing", "fetch_id_data_pan"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}
