package credential_test

import (
	"encoding/json"
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
  capability: fetch_id_data_pan
validation:
  fields:
    id_no:
      required: true
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

func TestLoadDirRejectsEmptyFieldRule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "EMPTY.v1.yaml")
	content := []byte(`
credential_type: EMPTY
version: 1
issuer: MSME
provider:
  name: digio
  capability: fetch_id_data_pan
validation:
  fields:
    name: {}
request:
  fields:
    id_no: normalized_id
response:
  success_field: id
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := credconfig.LoadDir(dir, credential.KnownRequestTransformerNames(), credential.KnownResponseTransformerNames())
	if err == nil {
		t.Fatal("expected error for empty field validation rule")
	}
}

func TestFieldAlignedValidationAppliesToConfiguredFieldsOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "NAMECHECK.v1.yaml")
	content := []byte(`
credential_type: NAMECHECK
version: 1
issuer: MSME
provider:
  name: digio
  capability: fetch_id_data_pan
normalization:
  trim: true
  uppercase: true
validation:
  fields:
    id_no:
      required: true
      patterns:
        - "^[A-Z]{5}[0-9]{4}[A-Z]$"
    name:
      required: true
      patterns:
        - "^[A-Za-z ]+$"
      message: "invalid name format"
request:
  fields:
    id_no: normalized_id
    name: name
response:
  success_field: pan
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := credconfig.LoadDir(dir, credential.KnownRequestTransformerNames(), credential.KnownResponseTransformerNames())
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def := catalog.Get("NAMECHECK")
	if def == nil {
		t.Fatal("missing NAMECHECK definition")
	}

	verifier, err := credential.NewVerifierFromDefinition(def, testGateway(t, nil))
	if err != nil {
		t.Fatalf("NewVerifierFromDefinition: %v", err)
	}

	if err := verifier.ValidateCredData(json.RawMessage(`{"id_no":"ABCDE1234F","name":"John Doe"}`)); err != nil {
		t.Fatalf("expected valid payload to pass: %v", err)
	}
	if err := verifier.ValidateCredData(json.RawMessage(`{"id_no":"ABCDE1234F"}`)); err == nil {
		t.Fatal("expected missing required name to fail")
	}
	if err := verifier.ValidateCredData(json.RawMessage(`{"id_no":"ABCDE1234F","name":"John123"}`)); err == nil {
		t.Fatal("expected invalid name pattern to fail")
	}
	if err := verifier.ValidateCredData(json.RawMessage(`{"id_no":"INVALID","name":"John Doe"}`)); err == nil {
		t.Fatal("expected invalid id_no pattern to fail")
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
