package handlers_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"credential-service/internal/auth"
	"credential-service/internal/handlers"

	"github.com/gofiber/fiber/v2"
)

type generateHeaderBody struct {
	Authorization string `json:"authorization"`
	Payload       string `json:"payload"`
	SubscriberID  string `json:"subscriber_id"`
	UniqueKeyID   string `json:"unique_key_id"`
	ExpiresAt     string `json:"expires_at"`
	ValidForSecs  int64  `json:"valid_for_seconds"`
	Error         string `json:"error"`
}

func generateHeaderApp(t *testing.T, privB64 string) *fiber.App {
	t.Helper()
	h := handlers.NewAuthHandler(handlers.AuthHandlerConfig{
		PrivateKey:   privB64,
		SubscriberID: "np.example.com",
		UniqueKeyID:  "key-001",
	})
	app := fiber.New()
	app.Post("/generate-header", h.GenerateHeader)
	return app
}

func postGenerateHeader(t *testing.T, app *fiber.App, target, body string) generateHeaderBody {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out generateHeaderBody
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %q: %v", string(raw), err)
	}
	return out
}

// The header this endpoint returns must actually authenticate the same payload.
// That round trip is the whole point of the endpoint, so it is the main test.
func TestGenerateHeaderProducesAVerifiableHeader(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	payload := `{"cred_id":"ABCDE1234F","cred_type":"PAN","name":"John Doe","dob":"01/01/1990"}`
	got := postGenerateHeader(t, app, "/generate-header", payload)

	if got.Authorization == "" {
		t.Fatalf("no authorization returned: %+v", got)
	}
	if got.Payload != payload {
		t.Fatalf("payload echoed as %q, want the exact bytes sent", got.Payload)
	}
	if got.ValidForSecs != 3600 {
		t.Fatalf("valid_for_seconds = %d, want 3600", got.ValidForSecs)
	}
	if got.ExpiresAt == "" {
		t.Fatal("expires_at is empty; it exists so an expired header is diagnosable")
	}

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
		ExpectedSubscriberID:     "np.example.com",
		ExpectedUniqueKeyID:      "key-001",
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.Verify(got.Authorization, payload); err != nil {
		t.Fatalf("generated header did not verify against its own payload: %v", err)
	}
}

// A header is bound to exact bytes. Re-indenting the body must break it, otherwise the
// endpoint would be handing out headers that silently fail later.
func TestGenerateHeaderIsBoundToExactBytes(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	compact := `{"cred_type":"PAN","cred_id":"ABCDE1234F"}`
	got := postGenerateHeader(t, app, "/generate-header", compact)

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	pretty := "{\n  \"cred_type\": \"PAN\",\n  \"cred_id\": \"ABCDE1234F\"\n}"
	if err := verifier.Verify(got.Authorization, pretty); err == nil {
		t.Fatal("a reformatted body verified; the signature is not covering raw bytes")
	}
}

func TestGenerateHeaderHonoursQueryOverrides(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	got := postGenerateHeader(t, app,
		"/generate-header?subscriber_id=other.example.com&unique_key_id=key-999", `{}`)

	if got.SubscriberID != "other.example.com" || got.UniqueKeyID != "key-999" {
		t.Fatalf("overrides ignored: %+v", got)
	}
	if !strings.Contains(got.Authorization, `keyId="other.example.com|key-999|ed25519"`) {
		t.Fatalf("keyId does not carry the overrides: %s", got.Authorization)
	}
}

func TestGenerateHeaderSignsAnEmptyBodyForGETRequests(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	got := postGenerateHeader(t, app, "/generate-header", "")
	if got.Authorization == "" {
		t.Fatalf("no header for an empty body: %+v", got)
	}

	verifier, err := auth.NewVerifier(auth.Config{
		RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.Verify(got.Authorization, ""); err != nil {
		t.Fatalf("empty-body header did not verify: %v", err)
	}
}

func TestGenerateHeaderReportsMissingPrivateKey(t *testing.T) {
	t.Parallel()

	app := generateHeaderApp(t, "")
	req := httptest.NewRequest(http.MethodPost, "/generate-header", strings.NewReader(`{}`))
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when no signing key is configured", resp.StatusCode)
	}
}

// Plain-text mode exists so a human can copy the header without JSON escaping. The
// response must therefore be the header itself and nothing else.
func TestGenerateHeaderPlainTextModeReturnsBareHeader(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))
	payload := `{"cred_id":"CWGPR7141M","cred_type":"PAN"}`

	for _, target := range []string{"/generate-header?format=text", "/generate-header?format=raw"} {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s: request: %v", target, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: read body: %v", target, err)
		}
		header := string(raw)

		if !strings.HasPrefix(header, "Signature keyId=") {
			t.Fatalf("%s: body is not a bare header: %q", target, header)
		}
		if strings.Contains(header, `\"`) {
			t.Fatalf("%s: body still carries JSON escaping: %q", target, header)
		}
		if strings.Contains(header, `"authorization"`) {
			t.Fatalf("%s: body is JSON, not plain text: %q", target, header)
		}
		if resp.Header.Get("X-Header-Expires-At") == "" {
			t.Fatalf("%s: expiry header missing", target)
		}

		// The copied value must authenticate the payload as-is.
		verifier, err := auth.NewVerifier(auth.Config{
			RegistrySigningPublicKey: base64.StdEncoding.EncodeToString(pub),
		})
		if err != nil {
			t.Fatalf("new verifier: %v", err)
		}
		if err := verifier.Verify(header, payload); err != nil {
			t.Fatalf("%s: plain-text header did not verify: %v", target, err)
		}
	}
}

func TestGenerateHeaderPlainTextViaAcceptHeader(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	req := httptest.NewRequest(http.MethodPost, "/generate-header", strings.NewReader(`{}`))
	req.Header.Set("Accept", "text/plain")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if !strings.HasPrefix(string(raw), "Signature keyId=") {
		t.Fatalf("Accept: text/plain did not return a bare header: %q", string(raw))
	}
}

// JSON stays the default so existing callers are unaffected.
func TestGenerateHeaderDefaultsToJSON(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	app := generateHeaderApp(t, base64.StdEncoding.EncodeToString(priv))

	got := postGenerateHeader(t, app, "/generate-header", `{}`)
	if got.Authorization == "" {
		t.Fatalf("default mode is no longer JSON: %+v", got)
	}
}
