package config_test

import (
	"testing"
	"time"

	"credential-service/internal/config"
)

func TestAuthEnvKeysUseAuthPrefix(t *testing.T) {
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_BASE_URL", "https://example.com")
	t.Setenv("CREDENTIAL_SERVICE_DIGIO_TOKEN", "tok")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_ENABLED", "true")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_SIGNING_PUBLIC_KEY", "pubkey")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_SUBSCRIBER_ID", "registry.local.test")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_REGISTRY_UNIQUE_KEY_ID", "registry-key-1")
	t.Setenv("CREDENTIAL_SERVICE_AUTH_LOOKUP_URL", "https://preprod.registry.ondc.org/v2.0/lookup")

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
	// The .env in use sets this exact key; a renamed tag would silently disable lookup.
	if cfg.Auth.LookupURL != "https://preprod.registry.ondc.org/v2.0/lookup" {
		t.Fatalf("CREDENTIAL_SERVICE_AUTH_LOOKUP_URL did not map to cfg.Auth.LookupURL, got %q", cfg.Auth.LookupURL)
	}
	if cfg.Auth.LookupCacheTTL != 5*time.Minute {
		t.Fatalf("unexpected lookup cache ttl default: %v", cfg.Auth.LookupCacheTTL)
	}
	if cfg.Auth.LookupTimeout != 10*time.Second {
		t.Fatalf("unexpected lookup timeout default: %v", cfg.Auth.LookupTimeout)
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
	if cfg.Credential.ProvidersDir != "configs/providers" {
		t.Fatalf("unexpected default ProvidersDir: %q", cfg.Credential.ProvidersDir)
	}

	t.Setenv("CREDENTIAL_SERVICE_CREDENTIAL_TYPES_DIR", "/etc/credential-types")
	t.Setenv("CREDENTIAL_SERVICE_CREDENTIAL_PROVIDERS_DIR", "/etc/providers")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Credential.TypesDir != "/etc/credential-types" {
		t.Fatalf("unexpected overridden TypesDir: %q", cfg.Credential.TypesDir)
	}
	if cfg.Credential.ProvidersDir != "/etc/providers" {
		t.Fatalf("unexpected overridden ProvidersDir: %q", cfg.Credential.ProvidersDir)
	}
}
