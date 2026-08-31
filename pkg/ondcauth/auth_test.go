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
