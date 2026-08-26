package credential_test

import (
	"os"
	"path/filepath"
	"testing"

	"credential-service/internal/credential"
	credconfig "credential-service/internal/credential/config"
)

func TestLoadCatalogFromRepoConfigs(t *testing.T) {
	t.Parallel()

	catalog, err := credential.LoadCatalog(testDefinitionsDir(t))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for _, typ := range []string{"PAN", "GST", "FSSAI", "UDYAM"} {
		def := catalog.Get(typ)
		if def == nil {
			t.Fatalf("missing definition for %s", typ)
		}
		if def.Issuer == "" {
			t.Fatalf("%s missing issuer", typ)
		}
	}

	if got := catalog.IssuerFor("PAN"); got != "INCOME_TAX_DEPT" {
		t.Fatalf("PAN issuer: got %q", got)
	}
	if got := catalog.IssuerFor("GST"); got != "GSTN" {
		t.Fatalf("GST issuer: got %q", got)
	}
	if got := catalog.IssuerFor("FSSAI"); got != "FSSAI_AUTHORITY" {
		t.Fatalf("FSSAI issuer: got %q", got)
	}
	if got := catalog.IssuerFor("UDYAM"); got != "MSME" {
		t.Fatalf("UDYAM issuer: got %q", got)
	}
}

func TestLoadDirRejectsUnknownTransformer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "BAD.v1.yaml")
	content := []byte(`
credential_type: BAD
version: 1
issuer: MSME
provider:
  name: digio
  endpoint: /v3/client/kyc/fetch_id_data/BAD
validation:
  patterns:
    - "^X$"
request:
  fields:
    id_no: normalized_id
response:
  transformer: definitely_not_a_real_transformer
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := credconfig.LoadDir(dir, credential.KnownRequestTransformerNames(), credential.KnownResponseTransformerNames())
	if err == nil {
		t.Fatal("expected error for unknown response transformer")
	}
}

func TestLoadDirRejectsEmptyDir(t *testing.T) {
	t.Parallel()

	_, err := credconfig.LoadDir(t.TempDir(), credential.KnownRequestTransformerNames(), credential.KnownResponseTransformerNames())
	if err == nil {
		t.Fatal("expected error for empty definitions dir")
	}
}

func TestRegistryRejectsUnknownCredType(t *testing.T) {
	t.Parallel()

	registry := testRegistry(t, nil)
	if _, err := registry.Resolve("NOT_A_TYPE"); err == nil {
		t.Fatal("expected unsupported cred type error")
	}
	if registry.IssuerFor("PAN") != "INCOME_TAX_DEPT" {
		t.Fatalf("unexpected issuer: %q", registry.IssuerFor("PAN"))
	}
}
