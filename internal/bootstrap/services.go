package bootstrap

import (
	"fmt"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/handlers"
	"credential-service/internal/models"
	"credential-service/internal/repository"
	"credential-service/internal/service"
	"credential-service/internal/service/client"
	"credential-service/internal/worker"

	"gorm.io/gorm"
)

func setupIdentityService(digioClient *client.DigioClient) *service.IdentityService {
	registry := credential.NewVerifierRegistry(digioClient)
	return service.NewIdentityService(registry)
}

func setupCredentialStack(db *gorm.DB, digioClient *client.DigioClient, identityService *service.IdentityService, defaultValidity time.Duration) (*service.CredentialService, *handlers.CredentialHandler, *worker.CredentialWorker, error) {
	if db == nil {
		return nil, nil, nil, fmt.Errorf("database required for credential stack")
	}

	enumCache, err := models.LoadEnumCache(db)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load enum cache: %w", err)
	}

	credReqRepo := repository.NewCredentialRequestRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	registry := credential.NewVerifierRegistry(digioClient)

	credService := service.NewCredentialService(credReqRepo, credRepo, registry, identityService, enumCache, defaultValidity)
	credHandler := handlers.NewCredentialHandler(credService)
	credWorker := worker.NewCredentialWorker(credService, credReqRepo, enumCache)

	return credService, credHandler, credWorker, nil
}
