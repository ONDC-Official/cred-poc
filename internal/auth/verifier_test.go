package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"

	"credential-service/internal/auth"
	"credential-service/pkg/ondcauth"
)

func TestVerifierAcceptsValidRegistryHeader(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{"cred_type":"PAN"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "registry.ondc.org",
		SubscriberUniqueKeyID: "registry-key-1",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
		ExpectedSubscriberID:     "registry.ondc.org",
		ExpectedUniqueKeyID:      "registry-key-1",
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	if err := verifier.Verify(header, body); err != nil {
		t.Fatalf("expected valid header, got: %v", err)
	}
}

func TestVerifierRejectsWrongSubscriber(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "attacker.example.com",
		SubscriberUniqueKeyID: "key-1",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
		ExpectedSubscriberID:     "registry.ondc.org",
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	if err := verifier.Verify(header, body); err == nil {
		t.Fatal("expected rejection for unexpected subscriber_id")
	}
}

func TestVerifierRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: "abc",
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	err = verifier.Verify("", `{}`)
	if err == nil {
		t.Fatal("expected error for missing authorization")
	}
	if err != auth.ErrMissingAuthorization {
		t.Fatalf("expected ErrMissingAuthorization, got %v", err)
	}
}

// fakeRegistry is a RegistryClient that records what it was asked for and returns
// a fixed key or error.
type fakeRegistry struct {
	key    string
	err    error
	gotSub string
	gotUK  string
	calls  int
}

func (f *fakeRegistry) LookupSubscriberKey(_ context.Context, subscriberID, ukid string) (string, error) {
	f.calls++
	f.gotSub, f.gotUK = subscriberID, ukid
	if f.err != nil {
		return "", f.err
	}
	return f.key, nil
}

func TestVerifierResolvesKeyThroughRegistry(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{"cred_type":"PAN","cred_id":"ABCDE1234F"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	registry := &fakeRegistry{key: base64.StdEncoding.EncodeToString(pub)}
	verifier, err := auth.NewVerifier(auth.Config{RegistryClient: registry})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	if err := verifier.Verify(header, body); err != nil {
		t.Fatalf("expected the registry-resolved key to verify, got: %v", err)
	}
	if registry.gotSub != "np.example.com" || registry.gotUK != "key-001" {
		t.Fatalf("registry queried with (%q, %q), want (np.example.com, key-001)", registry.gotSub, registry.gotUK)
	}
}

func TestVerifierDoesNotFallBackToStaticKeyWhenLookupFails(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	staticKey := base64.StdEncoding.EncodeToString(pub)

	body := `{}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	// The static key would verify this header. The lookup failure must still win.
	registry := &fakeRegistry{err: errors.New("registry unreachable")}
	verifier, err := auth.NewVerifier(auth.Config{
		RegistryClient:           registry,
		RegistrySigningPublicKey: staticKey,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	err = verifier.Verify(header, body)
	if err == nil {
		t.Fatal("expected rejection when the registry lookup fails")
	}
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestVerifierRejectsBodyAlteredAfterSigning(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	signedBody := `{"cred_id":"ABCDE1234F"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  signedBody,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	verifier, err := auth.NewVerifier(auth.Config{
		RegistryClient: &fakeRegistry{key: base64.StdEncoding.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	if err := verifier.Verify(header, `{"cred_id":"ZZZZZ9999Z"}`); err == nil {
		t.Fatal("expected rejection when the body differs from what was signed")
	}
}

func TestVerifierRequiresSomeKeySource(t *testing.T) {
	t.Parallel()

	if _, err := auth.NewVerifier(auth.Config{}); err == nil {
		t.Fatal("expected an error when neither a registry client nor a static key is configured")
	}
}
