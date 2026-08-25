package middleware_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"credential-service/internal/auth"
	"credential-service/internal/middleware"
	"credential-service/pkg/ondcauth"

	"github.com/gofiber/fiber/v2"
)

func TestSignatureAuthAllowsRegistrySignedRequest(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	body := `{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe","dob":"01/01/1990"}`
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

	app := fiber.New()
	app.Post("/verify-identity",
		middleware.CaptureRawBody(),
		middleware.SignatureAuth(verifier),
		func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/verify-identity", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", header)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSignatureAuthRejectsUnsignedRequest(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	app := fiber.New()
	app.Post("/verify-identity",
		middleware.CaptureRawBody(),
		middleware.SignatureAuth(verifier),
		func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/verify-identity", strings.NewReader(`{"cred_type":"PAN"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
