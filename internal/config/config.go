package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	App        AppConfig
	DB         DBConfig
	Digio      DigioConfig
	Credential CredentialConfig
}

type AppConfig struct {
	Name string `envconfig:"NAME" default:"credential-service"`
	Port string `envconfig:"PORT" default:"8080"`
	Env  string `envconfig:"ENV" default:"development"`
}

type DBConfig struct {
	Host            string        `envconfig:"HOST"`
	Port            int           `envconfig:"PORT" default:"5432"`
	User            string        `envconfig:"USER"`
	Password        string        `envconfig:"PASSWORD"`
	Name            string        `envconfig:"NAME"`
	SSLMode         string        `envconfig:"SSL_MODE" default:"disable"`
	MaxOpenConns    int           `envconfig:"MAX_OPEN_CONNS" default:"10"`
	MaxIdleConns    int           `envconfig:"MAX_IDLE_CONNS" default:"5"`
	ConnMaxLifetime time.Duration `envconfig:"CONN_MAX_LIFETIME" default:"30m"`
}

type DigioConfig struct {
	BaseURL string        `envconfig:"BASE_URL" required:"true"`
	Token   string        `envconfig:"TOKEN" required:"true"`
	Timeout time.Duration `envconfig:"TIMEOUT" default:"30s"`
}

// CredentialConfig holds policy defaults for the Credential Registry.
// DefaultValidity is a placeholder default (1 year), not a confirmed
// product/compliance policy per credential type — see
// docs/IMPLEMENTATION_ROADMAP.md Phase 3.
type CredentialConfig struct {
	DefaultValidity time.Duration `envconfig:"DEFAULT_VALIDITY" default:"8760h"`
}

func Load() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("CREDENTIAL_SERVICE", &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
