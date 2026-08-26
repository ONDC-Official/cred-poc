package config_test

import (
	"testing"

	"credential-service/internal/config"
)

func TestAuthEnvKeysUseAuthPrefix(t *testing.T) {
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_BASE_URL", "https://example.com")
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_TOKEN", "tok")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_ENABLED", "true")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_SIGNING_PUBLIC_KEY", "pubkey")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_SUBSCRIBER_ID", "registry.local.test")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_UNIQUE_KEY_ID", "registry-key-1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Fatal("expected AUTH_ENABLED=true to enable auth")
	}
	if cfg.Auth.RegistrySigningPublicKey != "pubkey" {
		t.Fatalf("unexpected public key: %q", cfg.Auth.RegistrySigningPublicKey)
	}
	if cfg.Auth.RegistrySubscriberID != "registry.local.test" {
		t.Fatalf("unexpected subscriber id: %q", cfg.Auth.RegistrySubscriberID)
	}
	if cfg.Auth.RegistryUniqueKeyID != "registry-key-1" {
		t.Fatalf("unexpected unique key id: %q", cfg.Auth.RegistryUniqueKeyID)
	}
}

func TestCredentialTypesDirDefaultAndOverride(t *testing.T) {
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_BASE_URL", "https://example.com")
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_TOKEN", "tok")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Credential.TypesDir != "configs/credential-types" {
		t.Fatalf("unexpected default TypesDir: %q", cfg.Credential.TypesDir)
	}

	t.Setenv("CREDENTIAL_SERVICE_CREDENTIAL_TYPES_DIR", "/etc/credential-types")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Credential.TypesDir != "/etc/credential-types" {
		t.Fatalf("unexpected overridden TypesDir: %q", cfg.Credential.TypesDir)
	}
}
