package bootstrap

import (
	"fmt"
	"log"

	"credential-service/internal/auth"
	"credential-service/internal/config"
	"credential-service/internal/service/client"
)

func setupClients(cfg *config.Config) *client.DigioClient {
	return client.NewDigioClient(client.DigioConfig{
		BaseURL: cfg.Digio.BaseURL,
		Token:   cfg.Digio.Token,
		Timeout: cfg.Digio.Timeout,
	})
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
