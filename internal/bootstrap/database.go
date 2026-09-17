package bootstrap

import (
	"fmt"
	"log/slog"

	"credential-service/internal/config"
	dbmigrate "credential-service/internal/database"
	"credential-service/pkg/database"

	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

func setupDatabase(cfg *config.Config) (*gorm.DB, error) {
	if cfg.DB.Host == "" {
		slog.Info("database host not configured, skipping database setup")
		return nil, nil
	}

	db, err := database.NewPostgres(cfg.DB)
	if err != nil {
		return nil, err
	}

	// A span per query, nested under the caller's span when the query runs with
	// db.WithContext(ctx), plus connection-pool metrics. Query variables are left out
	// because they carry PAN, GST and other credential ids.
	if err := db.Use(tracing.NewPlugin(tracing.WithoutQueryVariables())); err != nil {
		return nil, fmt.Errorf("failed to register database tracing: %w", err)
	}

	if err := dbmigrate.RunMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}
