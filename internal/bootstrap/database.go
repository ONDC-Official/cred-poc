package bootstrap

import (
	"fmt"
	"log"

	"credential-service/internal/config"
	dbmigrate "credential-service/internal/database"
	"credential-service/pkg/database"

	"gorm.io/gorm"
)

func setupDatabase(cfg *config.Config) (*gorm.DB, error) {
	if cfg.DB.Host == "" {
		log.Println("database host not configured, skipping database setup")
		return nil, nil
	}

	db, err := database.NewPostgres(cfg.DB)
	if err != nil {
		return nil, err
	}

	if err := dbmigrate.RunMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}
