package auth_test

import (
	"crypto/ed25519"
	"encoding/base64"
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
