package bootstrap

import (
	"fmt"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/handlers"
	"credential-service/internal/models"
	"credential-service/internal/provider"
	"credential-service/internal/repository"
	"credential-service/internal/service"
	"credential-service/internal/worker"

	"gorm.io/gorm"
)

func setupIdentityService(registry *credential.VerifierRegistry) *service.IdentityService {
	return service.NewIdentityService(registry)
}

// loadVerifierRegistry loads YAML credential-type definitions and builds the verifier registry.
func loadVerifierRegistry(typesDir string, gateway *provider.Gateway) (*credential.VerifierRegistry, error) {
	catalog, err := credential.LoadCatalog(typesDir)
	if err != nil {
		return nil, fmt.Errorf("load credential type definitions from %q: %w", typesDir, err)
	}
	registry, err := credential.NewVerifierRegistry(gateway, catalog)
	if err != nil {
		return nil, fmt.Errorf("build verifier registry: %w", err)
	}
	return registry, nil
}

// credentialStack is everything that needs a database, built together because these
// parts share one enum cache and one repository.
type credentialStack struct {
	service     *service.CredentialService
	handler     *handlers.CredentialHandler
	worker      *worker.CredentialWorker
	logger      *service.VerifyIdentityLogger
	logsHandler *handlers.SubscriberLogHandler
}

func setupCredentialStack(
	db *gorm.DB,
	registry *credential.VerifierRegistry,
	identityService *service.IdentityService,
	defaultValidity time.Duration,
) (*credentialStack, error) {
	if db == nil {
		return nil, fmt.Errorf("database required for credential stack")
	}

	enumCache, err := models.LoadEnumCache(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load enum cache: %w", err)
	}

	credReqRepo := repository.NewCredentialRequestRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	credService := service.NewCredentialService(credReqRepo, credRepo, registry, identityService, enumCache, defaultValidity)
	credHandler := handlers.NewCredentialHandler(credService)
	credWorker := worker.NewCredentialWorker(credService, credReqRepo, enumCache)

	return &credentialStack{
		service: credService,
		handler: credHandler,
		worker:  credWorker,
		logger:      service.NewVerifyIdentityLogger(credReqRepo, enumCache, registry),
		logsHandler: handlers.NewSubscriberLogHandler(credReqRepo, enumCache),
	}, nil
}
