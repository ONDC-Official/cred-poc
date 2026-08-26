package bootstrap

import (
	"fmt"
	"log"

	"credential-service/internal/auth"
	"credential-service/internal/config"
	"credential-service/internal/provider"
	"credential-service/internal/service/client"
)

func setupProviderGateway(cfg *config.Config) (*provider.Gateway, error) {
	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: cfg.Digio.BaseURL,
		Token:   cfg.Digio.Token,
		Timeout: cfg.Digio.Timeout,
	})

	// Register each vendor Caller under the same name as configs/providers/*.yaml.
	// Future providers: add YAML + implement Caller + register here.
	callers := map[string]provider.Caller{
		"digio": digioClient,
	}

	catalog, err := provider.LoadDir(cfg.Credential.ProvidersDir)
	if err != nil {
		return nil, fmt.Errorf("load provider definitions from %q: %w", cfg.Credential.ProvidersDir, err)
	}
	return provider.NewGateway(catalog, callers)
}

func setupAuthVerifier(cfg *config.Config) (*auth.Verifier, error) {
	if !cfg.Auth.Enabled {
		log.Println("signature auth disabled (CREDENTIAL_SERVICE_AUTH_ENABLED=false); /verify-identity and /credential are open")
		return nil, nil
	}
	if cfg.Auth.RegistrySigningPublicKey == "" {
		return nil, fmt.Errorf("CREDENTIAL_SERVICE_AUTH_REGISTRY_SIGNING_PUBLIC_KEY is required when CREDENTIAL_SERVICE_AUTH_ENABLED=true")
	}

	log.Println("signature auth enabled; registry-signed Authorization header required")
	return auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: cfg.Auth.RegistrySigningPublicKey,
		ExpectedSubscriberID:     cfg.Auth.RegistrySubscriberID,
		ExpectedUniqueKeyID:      cfg.Auth.RegistryUniqueKeyID,
	})
}
