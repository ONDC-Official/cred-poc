package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"credential-service/internal/config"
	"credential-service/internal/telemetry"
	"credential-service/internal/worker"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type App struct {
	cfg               *config.Config
	fiber             *fiber.App
	db                *gorm.DB
	worker            *worker.CredentialWorker
	shutdownTelemetry telemetry.Shutdown
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

	// First, so boot logs, migrations and the GORM plugin all see the real providers.
	shutdownTelemetry, err := telemetry.Setup(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup telemetry: %w", err)
	}

	app, err := newApp(cfg)
	if err != nil {
		// Flush whatever boot managed to emit before the failure.
		_ = shutdownTelemetry(context.Background())
		return nil, err
	}
	app.shutdownTelemetry = shutdownTelemetry
	return app, nil
}

func newApp(cfg *config.Config) (*App, error) {
	db, err := setupDatabase(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup database: %w", err)
	}

	gateway, err := setupProviderGateway(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup provider gateway: %w", err)
	}

	registry, err := loadVerifierRegistry(cfg.Credential.TypesDir, gateway)
	if err != nil {
		return nil, err
	}

	identityService := setupIdentityService(registry)

	var credWorker *worker.CredentialWorker
	stack, err := setupCredentialStack(db, registry, identityService, cfg.Credential.DefaultValidity)
	if err != nil {
		if db != nil {
			return nil, fmt.Errorf("failed to setup credential stack: %w", err)
		}
		slog.Info("credential stack not initialized (no database)")
		stack = &credentialStack{}
	} else {
		credWorker = stack.worker
	}

	handlers := NewHandlers(cfg, identityService, stack.handler, stack.logger, stack.logsHandler)
	if handlers.Auth != nil {
		slog.Warn("POST /generate-header is enabled and UNAUTHENTICATED; it signs any payload with this service's key. Local testing only.")
	}
	authVerifier, err := setupAuthVerifier(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup auth verifier: %w", err)
	}

	fiberApp := fiber.New(fiber.Config{
		AppName: fmt.Sprintf("%s (%s)", cfg.App.Name, cfg.App.Env),
	})

	registerMiddleware(fiberApp)
	registerRoutes(fiberApp, handlers, authVerifier)

	return &App{
		cfg:    cfg,
		fiber:  fiberApp,
		db:     db,
		worker: credWorker,
	}, nil
}

func (a *App) Start() error {
	addr := ":" + a.cfg.App.Port
	slog.Info("starting server", "service", a.cfg.App.Name, "addr", addr)

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
		slog.Info("shutting down server...")
	}

	if a.worker != nil {
		a.worker.Stop()
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := a.fiber.ShutdownWithContext(shutdownCtx); err != nil {
		_ = a.shutdownTelemetry(shutdownCtx)
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("server stopped")
	// Last, so the spans, metrics and logs of shutdown itself are flushed.
	if err := a.shutdownTelemetry(shutdownCtx); err != nil {
		return fmt.Errorf("telemetry shutdown: %w", err)
	}
	return nil
}
