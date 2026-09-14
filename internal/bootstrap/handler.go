package bootstrap

import (
	"credential-service/internal/config"
	"credential-service/internal/handlers"
	"credential-service/internal/service"
)

type Handlers struct {
	Health     *handlers.HealthHandler
	Identity   *handlers.IdentityHandler
	Credential *handlers.CredentialHandler
	// Auth is nil unless the header generator is enabled; routes.go skips the route then.
	Auth *handlers.AuthHandler
}

func NewHandlers(cfg *config.Config, identityService *service.IdentityService, credHandler *handlers.CredentialHandler) *Handlers {
	h := &Handlers{
		Health:     handlers.NewHealthHandler(),
		Identity:   handlers.NewIdentityHandler(identityService),
		Credential: credHandler,
	}

	if cfg.Auth.HeaderGeneratorEnabled(cfg.App.Env) {
		h.Auth = handlers.NewAuthHandler(handlers.AuthHandlerConfig{
			PrivateKey:   cfg.Auth.SigningPrivate,
			SubscriberID: cfg.Auth.SubscriberID,
			UniqueKeyID:  cfg.Auth.UniqueKeyID,
		})
	}

	return h
}
