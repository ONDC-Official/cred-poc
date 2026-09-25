package bootstrap

import (
	"fmt"
	"log/slog"
	"strings"

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
		"mock":  client.NewMockClient(),
	}

	catalog, err := provider.LoadDir(cfg.Credential.ProvidersDir)
	if err != nil {
		return nil, fmt.Errorf("load provider definitions from %q: %w", cfg.Credential.ProvidersDir, err)
	}
	return provider.NewGateway(catalog, callers)
}

func setupAuthVerifier(cfg *config.Config) (*auth.Verifier, error) {
	if !cfg.Auth.Enabled {
		slog.Info("signature auth disabled (CREDENTIAL_SERVICE_AUTH_ENABLED=false); /verify and /credential are open")
		return nil, nil
	}
	var registryClient auth.RegistryClient
	if cfg.Auth.LookupURL != "" {
		// The lookup call must itself be signed, so this service's own credentials are
		// mandatory on this path. Fail at boot rather than on the first request.
		var missing []string
		if cfg.Auth.SigningPrivate == "" {
			missing = append(missing, "CREDENTIAL_SERVICE_AUTH_SIGNING_PRIVATE")
		}
		if cfg.Auth.SubscriberID == "" {
			missing = append(missing, "CREDENTIAL_SERVICE_AUTH_SUBSCRIBER_ID")
		}
		if cfg.Auth.UniqueKeyID == "" {
			missing = append(missing, "CREDENTIAL_SERVICE_AUTH_UNIQUE_KEY_ID")
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("%s required when CREDENTIAL_SERVICE_AUTH_LOOKUP_URL is set", strings.Join(missing, ", "))
		}

		registryClient = auth.NewONDCRegistryClient(auth.ONDCRegistryClientConfig{
			LookupURL:    cfg.Auth.LookupURL,
			PrivateKey:   cfg.Auth.SigningPrivate,
			SubscriberID: cfg.Auth.SubscriberID,
			UniqueKeyID:  cfg.Auth.UniqueKeyID,
			CacheTTL:     cfg.Auth.LookupCacheTTL,
			Timeout:      cfg.Auth.LookupTimeout,
		})
		slog.Info("signature auth enabled; signing keys resolved from ONDC Registry", "lookup_url", cfg.Auth.LookupURL)
	}

	if registryClient == nil && cfg.Auth.RegistrySigningPublicKey == "" {
		return nil, fmt.Errorf("CREDENTIAL_SERVICE_AUTH_LOOKUP_URL or CREDENTIAL_SERVICE_AUTH_REGISTRY_SIGNING_PUBLIC_KEY is required when CREDENTIAL_SERVICE_AUTH_ENABLED=true")
	}
	if registryClient == nil {
		slog.Info("signature auth enabled; verifying against the static configured signing public key")
	}

	return auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: cfg.Auth.RegistrySigningPublicKey,
		ExpectedSubscriberID:     cfg.Auth.RegistrySubscriberID,
		ExpectedUniqueKeyID:      cfg.Auth.RegistryUniqueKeyID,
		RegistryClient:           registryClient,
	})
}
