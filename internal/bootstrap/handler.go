package bootstrap

import (
	"credential-service/internal/handlers"
	"credential-service/internal/service"
)

type Handlers struct {
	Health     *handlers.HealthHandler
	Identity   *handlers.IdentityHandler
	Credential *handlers.CredentialHandler
}

func NewHandlers(identityService *service.IdentityService, credHandler *handlers.CredentialHandler) *Handlers {
	return &Handlers{
		Health:     handlers.NewHealthHandler(),
		Identity:   handlers.NewIdentityHandler(identityService),
		Credential: credHandler,
	}
}
