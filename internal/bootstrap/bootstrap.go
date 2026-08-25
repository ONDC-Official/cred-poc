package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"credential-service/internal/config"
	"credential-service/internal/worker"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type App struct {
	cfg    *config.Config
	fiber  *fiber.App
	db     *gorm.DB
	worker *worker.CredentialWorker
}

func Run() error {
	app, err := New()
	if err != nil {
		return err
	}

	return app.Start()
}

func New() (*App, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	db, err := setupDatabase(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup database: %w", err)
	}

	digioClient := setupClients(cfg)
	identityService := setupIdentityService(digioClient)

	var credWorker *worker.CredentialWorker
	credService, credHandler, cw, err := setupCredentialStack(db, digioClient, identityService, cfg.Credential.DefaultValidity)
	if err != nil {
		if db != nil {
			return nil, fmt.Errorf("failed to setup credential stack: %w", err)
		}
		log.Println("credential stack not initialized (no database)")
	} else {
		_ = credService
		credWorker = cw
	}

	handlers := NewHandlers(identityService, credHandler)

	fiberApp := fiber.New(fiber.Config{
		AppName: fmt.Sprintf("%s (%s)", cfg.App.Name, cfg.App.Env),
	})

	registerMiddleware(fiberApp)
	registerRoutes(fiberApp, handlers)

	return &App{
		cfg:    cfg,
		fiber:  fiberApp,
		db:     db,
		worker: credWorker,
	}, nil
}

func (a *App) Start() error {
	addr := ":" + a.cfg.App.Port
	log.Printf("starting %s on %s", a.cfg.App.Name, addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if a.worker != nil {
		a.worker.Start(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := a.fiber.Listen(addr); err != nil {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("failed to start server: %w", err)
	case <-quit:
		log.Println("shutting down server...")
	}

	if a.worker != nil {
		a.worker.Stop()
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := a.fiber.ShutdownWithContext(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Println("server stopped")
	return nil
}
