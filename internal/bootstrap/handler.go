package bootstrap

import (
	"log/slog"
	"strings"

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
	// SubscriberLogs is nil without both a database and an access token.
	SubscriberLogs *handlers.SubscriberLogHandler

	accessToken string
}

func NewHandlers(
	cfg *config.Config,
	identityService *service.IdentityService,
	credHandler *handlers.CredentialHandler,
	verifyIdentityLogger *service.VerifyIdentityLogger,
	subscriberLogs *handlers.SubscriberLogHandler,
) *Handlers {
	h := &Handlers{
		Health:      handlers.NewHealthHandler(),
		Identity:    handlers.NewIdentityHandler(identityService, verifyIdentityLogger),
		Credential:  credHandler,
		accessToken: strings.TrimSpace(cfg.AccessToken),
	}

	// An unset token leaves the route unregistered rather than open.
	switch {
	case subscriberLogs == nil:
	case h.accessToken == "":
		slog.Info("GET /subscriber/:id not registered: CREDENTIAL_SERVICE_ACCESS_TOKEN is not set")
	default:
		h.SubscriberLogs = subscriberLogs
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
