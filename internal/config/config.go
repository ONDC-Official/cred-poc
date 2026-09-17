package config

import (
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	App        AppConfig
	DB         DBConfig
	Digio      DigioConfig
	Credential CredentialConfig
	Auth       AuthConfig
	Telemetry  TelemetryConfig `envconfig:"OTEL"`
	// AccessToken guards GET /subscriber/:id only, as a bare `Authorization: <token>`
	// header. Empty leaves the route unregistered. Env: CREDENTIAL_SERVICE_ACCESS_TOKEN.
	AccessToken string `envconfig:"ACCESS_TOKEN"`
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
	// TypesDir is the filesystem path to Git-tracked credential-type YAML definitions.
	TypesDir string `envconfig:"TYPES_DIR" default:"configs/credential-types"`
	// ProvidersDir is the filesystem path to Git-tracked provider capability catalogs.
	ProvidersDir string `envconfig:"PROVIDERS_DIR" default:"configs/providers"`
}

// TelemetryConfig switches OpenTelemetry export on. Everything else (endpoint, headers,
// sampler, extra resource attributes) is read by the SDK from the standard OTEL_* env vars,
// e.g. OTEL_EXPORTER_OTLP_ENDPOINT. Env: CREDENTIAL_SERVICE_OTEL_ENABLED.
type TelemetryConfig struct {
	Enabled bool `envconfig:"ENABLED" default:"false"`
}

// AuthConfig secures credential-service APIs for registry-service → credential-service calls.
// NPs never call these endpoints; the registry signs with its Ed25519 private key and this
// service verifies with the registry's public key (CREDENTIAL_SERVICE_AUTH_*).
type AuthConfig struct {
	Enabled bool `envconfig:"ENABLED" default:"false"`
	// RegistrySigningPublicKey is the registry service Ed25519 public key (base64).
	RegistrySigningPublicKey string `envconfig:"REGISTRY_SIGNING_PUBLIC_KEY"`
	// Optional: pin Authorization keyId to the known registry identity.
	RegistrySubscriberID string `envconfig:"REGISTRY_SUBSCRIBER_ID"`
	RegistryUniqueKeyID  string `envconfig:"REGISTRY_UNIQUE_KEY_ID"`
	// Optional: this service's own signing keypair for future outbound calls to registry.
	SigningPrivate string `envconfig:"SIGNING_PRIVATE"`
	SigningPublic  string `envconfig:"SIGNING_PUBLIC"`
	SubscriberID   string `envconfig:"SUBSCRIBER_ID"`
	UniqueKeyID    string `envconfig:"UNIQUE_KEY_ID"`
	// LookupURL is the ONDC Registry /lookup endpoint. When set, inbound signing keys are
	// resolved per-subscriber from the registry instead of the single static key above.
	// Full env key: CREDENTIAL_SERVICE_AUTH_LOOKUP_URL.
	LookupURL string `envconfig:"LOOKUP_URL"`
	// LookupCacheTTL is how long a resolved signing key is reused before re-querying.
	LookupCacheTTL time.Duration `envconfig:"LOOKUP_CACHE_TTL" default:"5m"`
	// LookupTimeout bounds a single outbound /lookup call.
	LookupTimeout time.Duration `envconfig:"LOOKUP_TIMEOUT" default:"10s"`
	// EnableHeaderGenerator exposes POST /generate-header, which signs any payload with
	// this service's own private key. It is a local testing aid, never production intent.
	// Left unset it is on in development and off everywhere else; set it to force either
	// way. Full env key: CREDENTIAL_SERVICE_AUTH_ENABLE_HEADER_GENERATOR.
	EnableHeaderGenerator *bool `envconfig:"ENABLE_HEADER_GENERATOR"`
}

// HeaderGeneratorEnabled reports whether POST /generate-header should be registered.
// An explicit setting always wins; otherwise it follows the environment, so a developer
// gets the helper for free and a deployed service does not.
func (a AuthConfig) HeaderGeneratorEnabled(appEnv string) bool {
	if a.EnableHeaderGenerator != nil {
		return *a.EnableHeaderGenerator
	}
	return strings.EqualFold(strings.TrimSpace(appEnv), "development")
}

func Load() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("CREDENTIAL_SERVICE", &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
