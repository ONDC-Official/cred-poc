package auth

import (
	"errors"
	"fmt"

	"credential-service/pkg/ondcauth"
)

var (
	ErrMissingAuthorization = errors.New("authorization header is required")
	ErrUnauthorized         = errors.New("invalid authorization header")
)

// Verifier authenticates inbound calls from the registry service.
// Only the registry calls credential-service; NPs never hit these APIs directly.
// The registry signs with its Ed25519 private key; this service verifies with the
// configured registry signing public key.
type Verifier struct {
	registryPublicKey string
	expectedSubscriberID string
	expectedUniqueKeyID  string
}

type Config struct {
	// RegistrySigningPublicKey is the Ed25519 public key (base64) of the registry service.
	RegistrySigningPublicKey string
	// ExpectedSubscriberID / ExpectedUniqueKeyID optionally pin the Authorization keyId
	// to the known registry identity (defense in depth).
	ExpectedSubscriberID string
	ExpectedUniqueKeyID  string
}

func NewVerifier(cfg Config) (*Verifier, error) {
	if cfg.RegistrySigningPublicKey == "" {
		return nil, errors.New("registry signing public key is required")
	}
	return &Verifier{
		registryPublicKey:    cfg.RegistrySigningPublicKey,
		expectedSubscriberID: cfg.ExpectedSubscriberID,
		expectedUniqueKeyID:  cfg.ExpectedUniqueKeyID,
	}, nil
}

// Verify checks the ONDC Authorization header against the exact raw request body
// using the configured registry public key.
func (v *Verifier) Verify(authHeader, rawBody string) error {
	if authHeader == "" {
		return ErrMissingAuthorization
	}

	subscriberID, uniqueKeyID, err := ondcauth.ParseKeyID(authHeader)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	if v.expectedSubscriberID != "" && subscriberID != v.expectedSubscriberID {
		return fmt.Errorf("%w: unexpected subscriber_id in keyId", ErrUnauthorized)
	}
	if v.expectedUniqueKeyID != "" && uniqueKeyID != v.expectedUniqueKeyID {
		return fmt.Errorf("%w: unexpected unique_key_id in keyId", ErrUnauthorized)
	}

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: authHeader,
		Payload:    rawBody,
		PublicKey:  v.registryPublicKey,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	if !ok {
		return ErrUnauthorized
	}

	return nil
}
