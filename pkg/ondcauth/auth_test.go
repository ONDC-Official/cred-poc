package ondcauth_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"credential-service/pkg/ondcauth"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	body := `{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe","dob":"01/01/1990"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            privB64,
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	subscriberID, ukID, err := ondcauth.ParseKeyID(header)
	if err != nil {
		t.Fatalf("parse keyId: %v", err)
	}
	if subscriberID != "np.example.com" || ukID != "key-001" {
		t.Fatalf("unexpected keyId parts: %s %s", subscriberID, ukID)
	}

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header,
		Payload:    body,
		PublicKey:  pubB64,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected valid signature")
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{"cred_id":"ABCDE1234F","cred_type":"PAN"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	_, err = ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header,
		Payload:    `{"cred_id":"TAMPERED","cred_type":"PAN"}`,
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
	})
	if err == nil {
		t.Fatal("expected verification to fail for tampered body")
	}
}

func TestSignAndVerifyWithSeedPrivateKey(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// A 32-byte seed must work as well as the 64-byte expanded key.
	seed := base64.StdEncoding.EncodeToString(priv.Seed())

	body := `{"cred_type":"PAN"}`
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            seed,
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
	})
	if err != nil {
		t.Fatalf("create header from seed: %v", err)
	}

	ok, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header,
		Payload:    body,
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected a seed-signed header to verify")
	}
}

func TestParseKeyIDRejectsNonEd25519Algorithm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{
			name:   "algorithm parameter",
			header: `Signature keyId="np.example.com|key-001|ed25519",algorithm="rsa",created="1",expires="2",headers="(created) (expires) digest",signature="c2ln"`,
		},
		{
			name:   "keyId segment",
			header: `Signature keyId="np.example.com|key-001|rsa",algorithm="ed25519",created="1",expires="2",headers="(created) (expires) digest",signature="c2ln"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := ondcauth.ParseKeyID(tc.header); err == nil {
				t.Fatal("expected a non-ed25519 algorithm to be rejected")
			}
		})
	}
}

func TestVerifyRejectsNonEd25519Algorithm(t *testing.T) {
	t.Parallel()

	header := `Signature keyId="np.example.com|key-001|ed25519",algorithm="rsa",created="1",expires="99999999999",headers="(created) (expires) digest",signature="c2ln"`
	if _, err := ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header,
		Payload:    `{}`,
		PublicKey:  "cHVibGlja2V5",
	}); err == nil {
		t.Fatal("expected a non-ed25519 algorithm to be rejected")
	}
}

func TestVerifyRejectsExpiredHeader(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{}`
	// created/expires both well in the past.
	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  body,
		PrivateKey:            base64.StdEncoding.EncodeToString(priv),
		SubscriberID:          "np.example.com",
		SubscriberUniqueKeyID: "key-001",
		Created:               "1000000000",
		Expires:               "1000003600",
	})
	if err != nil {
		t.Fatalf("create header: %v", err)
	}

	_, err = ondcauth.VerifyAuthorisationHeader(ondcauth.VerifyAuthorisationHeaderParams{
		AuthHeader: header,
		Payload:    body,
		PublicKey:  "cHVibGlja2V5",
	})
	if err == nil {
		t.Fatal("expected an expired header to be rejected")
	}
}
