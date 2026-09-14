package auth

import (
	"context"
	"errors"
	"fmt"
	"log"

	"credential-service/pkg/ondcauth"
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

// Verify checks the ONDC Authorization header against the exact raw request body.
// It returns nil only when the signature verifies under a key the registry vouches for.
func (v *Verifier) Verify(authHeader, rawBody string) error {
	return v.VerifyContext(context.Background(), authHeader, rawBody)
}

// VerifyContext is Verify with a caller-supplied context, so the outbound registry
// lookup is bounded by the inbound request's lifetime.
func (v *Verifier) VerifyContext(ctx context.Context, authHeader, rawBody string) error {
	if authHeader == "" {
		return ErrMissingAuthorization
	}

	subscriberID, uniqueKeyID, err := ondcauth.ParseKeyID(authHeader)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	publicKey, err := v.resolvePublicKey(ctx, subscriberID, uniqueKeyID)
	if err != nil {
		log.Printf("auth: key resolution failed for subscriber %q (ukid %q): %v", subscriberID, uniqueKeyID, err)
		return err
	}

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: authHeader,
		Payload:    rawBody,
		PublicKey:  publicKey,
	})
	if err != nil {
		log.Printf("auth: signature verification failed for subscriber %q (ukid %q): %v", subscriberID, uniqueKeyID, err)
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	if !ok {
		log.Printf("auth: signature invalid for subscriber %q (ukid %q)", subscriberID, uniqueKeyID)
		return ErrUnauthorized
	}

	log.Printf("auth: request authenticated for subscriber %q (ukid %q)", subscriberID, uniqueKeyID)
	return nil
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
