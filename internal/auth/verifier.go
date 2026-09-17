package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"credential-service/pkg/ondcauth"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Span attributes set on the inbound request span. Never the header value itself.
const (
	attrAuthResult       = attribute.Key("auth.result")
	attrAuthSubscriberID = attribute.Key("auth.subscriber_id")
)

var (
	ErrMissingAuthorization = errors.New("authorization header is required")
	ErrUnauthorized         = errors.New("invalid authorization header")
)

// Verifier authenticates inbound ONDC-signed calls.
//
// The signing public key comes from the ONDC Registry, looked up per request by the
// subscriber_id and unique_key_id carried in the Authorization header. A statically
// configured key is supported only as the whole alternative configuration, for local
// testing without a registry; it is never a fallback when a lookup fails.
type Verifier struct {
	registryPublicKey    string
	expectedSubscriberID string
	expectedUniqueKeyID  string
	registryClient       RegistryClient
}

type Config struct {
	// RegistrySigningPublicKey is a static Ed25519 public key (base64), used only when
	// RegistryClient is nil.
	RegistrySigningPublicKey string
	// ExpectedSubscriberID / ExpectedUniqueKeyID pin the Authorization keyId to a known
	// identity. They apply to the static-key path, where there is no registry to confirm
	// the caller against.
	ExpectedSubscriberID string
	ExpectedUniqueKeyID  string
	// RegistryClient resolves signing keys from the ONDC Registry. When set, it is the
	// only source of trust.
	RegistryClient RegistryClient
}

func NewVerifier(cfg Config) (*Verifier, error) {
	if cfg.RegistryClient == nil && cfg.RegistrySigningPublicKey == "" {
		return nil, errors.New("either a registry client or a static signing public key is required")
	}
	return &Verifier{
		registryPublicKey:    cfg.RegistrySigningPublicKey,
		expectedSubscriberID: cfg.ExpectedSubscriberID,
		expectedUniqueKeyID:  cfg.ExpectedUniqueKeyID,
		registryClient:       cfg.RegistryClient,
	}, nil
}

// Identity is the ONDC caller a verified Authorization header belongs to.
type Identity struct {
	SubscriberID string
	UniqueKeyID  string
}

// Verify checks the ONDC Authorization header against the exact raw request body.
// It returns nil only when the signature verifies under a key the registry vouches for.
func (v *Verifier) Verify(authHeader, rawBody string) error {
	return v.VerifyContext(context.Background(), authHeader, rawBody)
}

// VerifyContext is Verify with a caller-supplied context, so the outbound registry
// lookup is bounded by the inbound request's lifetime.
func (v *Verifier) VerifyContext(ctx context.Context, authHeader, rawBody string) error {
	_, err := v.VerifyRequest(ctx, authHeader, rawBody)
	return err
}

// VerifyRequest is VerifyContext that also returns who the caller is, so a handler
// need not re-parse the header.
func (v *Verifier) VerifyRequest(ctx context.Context, authHeader, rawBody string) (Identity, error) {
	span := trace.SpanFromContext(ctx)
	if authHeader == "" {
		span.SetAttributes(attrAuthResult.String("missing_header"))
		return Identity{}, ErrMissingAuthorization
	}

	subscriberID, uniqueKeyID, err := ondcauth.ParseKeyID(authHeader)
	if err != nil {
		span.SetAttributes(attrAuthResult.String("malformed_key_id"))
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	span.SetAttributes(attrAuthSubscriberID.String(subscriberID))

	publicKey, err := v.resolvePublicKey(ctx, subscriberID, uniqueKeyID)
	if err != nil {
		span.SetAttributes(attrAuthResult.String("key_resolution_failed"))
		slog.WarnContext(ctx, "auth: key resolution failed",
			"subscriber_id", subscriberID, "ukid", uniqueKeyID, "error", err)
		return Identity{}, err
	}

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: authHeader,
		Payload:    rawBody,
		PublicKey:  publicKey,
	})
	if err != nil {
		span.SetAttributes(attrAuthResult.String("verification_error"))
		slog.WarnContext(ctx, "auth: signature verification failed",
			"subscriber_id", subscriberID, "ukid", uniqueKeyID, "error", err)
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	if !ok {
		span.SetAttributes(attrAuthResult.String("invalid_signature"))
		slog.WarnContext(ctx, "auth: signature invalid", "subscriber_id", subscriberID, "ukid", uniqueKeyID)
		return Identity{}, ErrUnauthorized
	}

	span.SetAttributes(attrAuthResult.String("ok"))
	slog.InfoContext(ctx, "auth: request authenticated", "subscriber_id", subscriberID, "ukid", uniqueKeyID)
	return Identity{SubscriberID: subscriberID, UniqueKeyID: uniqueKeyID}, nil
}

func (v *Verifier) resolvePublicKey(ctx context.Context, subscriberID, uniqueKeyID string) (string, error) {
	if v.registryClient != nil {
		// Registry-backed: a lookup failure is an authentication failure. Falling back
		// to the static key here would let an unregistered subscriber in whenever the
		// registry is unreachable.
		publicKey, err := v.registryClient.LookupSubscriberKey(ctx, subscriberID, uniqueKeyID)
		if err != nil {
			return "", fmt.Errorf("%w: registry lookup failed: %v", ErrUnauthorized, err)
		}
		return publicKey, nil
	}

	// Static-key path: no registry to confirm the caller, so the configured pins are
	// the only thing tying the header to a known identity.
	if v.expectedSubscriberID != "" && subscriberID != v.expectedSubscriberID {
		return "", fmt.Errorf("%w: unexpected subscriber_id in keyId", ErrUnauthorized)
	}
	if v.expectedUniqueKeyID != "" && uniqueKeyID != v.expectedUniqueKeyID {
		return "", fmt.Errorf("%w: unexpected unique_key_id in keyId", ErrUnauthorized)
	}
	if v.registryPublicKey == "" {
		return "", fmt.Errorf("%w: no public key available for verification", ErrUnauthorized)
	}
	return v.registryPublicKey, nil
}
