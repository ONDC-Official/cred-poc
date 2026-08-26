package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credential-service/internal/bootstrap"
	"credential-service/internal/credential"
	"credential-service/internal/service"
	"credential-service/internal/service/client"

	"github.com/gofiber/fiber/v2"
)

func setupTestApp(t *testing.T, digioHandler http.HandlerFunc) *fiber.App {
	t.Helper()

	server := httptest.NewServer(digioHandler)
	t.Cleanup(server.Close)

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	dir, err := credential.FindDefinitionsDir()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := credential.NewRegistryFromDir(dir, digioClient)
	if err != nil {
		t.Fatalf("NewRegistryFromDir: %v", err)
	}
	svc := service.NewIdentityService(registry)
	h := bootstrap.NewHandlers(svc, nil)

	app := fiber.New()
	bootstrap.RegisterRoutes(app, h, nil)

	return app
}

func TestVerifyIdentityValidPAN(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pan":"ABCDE1234F","category":"Individual","status":"VALID","full_name":"John Doe"}`))
	})

	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F","cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["pan"] != "ABCDE1234F" {
		t.Fatalf("unexpected response: %v", result)
	}
}

func TestVerifyIdentityPANDigioRejectionPassesThrough(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid Pan Number Detected"}`))
	})

	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F","cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected our API to return 200 (relaying Digio's payload verbatim), got %d", resp.StatusCode)
	}

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["code"] != "BAD_REQUEST" || result["message"] != "Invalid Pan Number Detected" {
		t.Fatalf("expected Digio's raw rejection body, got: %v", result)
	}
}

func TestVerifyIdentityPANRejectionWithNonJSONBody502s(t *testing.T) {
	t.Parallel()

	// Process()'s statusCode>=400 branch never validates the body as JSON —
	// this endpoint's own json.Valid guard (identity_handler.go) is what
	// must catch this and 502 rather than relaying non-JSON bytes as if
	// they were a JSON response.
	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html>not json</html>`))
	})

	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F","cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

func TestVerifyIdentityPANSendsIDOnly(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]string
	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pan":"ABCDE1234F","category":"Individual","status":"VALID","full_name":"John Doe"}`))
	})

	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F","cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if gotPayload["id_no"] != "ABCDE1234F" {
		t.Fatalf("expected id_no to reach Digio, got %v", gotPayload)
	}
	if _, ok := gotPayload["name"]; ok {
		t.Fatalf("name must not reach Digio, got %v", gotPayload)
	}
	if _, ok := gotPayload["dob"]; ok {
		t.Fatalf("dob must not reach Digio, got %v", gotPayload)
	}
}

func TestVerifyIdentityValidGST(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error_message":"","corporate_name":"Example Corp","gstin":"29AABCU9603R1ZM","details":{}}`))
	})

	body := bytes.NewBufferString(`{"cred_id":"29AABCU9603R1ZM","cred_type":"GST"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestVerifyIdentityInvalidJSON(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {})
	body := bytes.NewBufferString(`{invalid`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVerifyIdentityMissingCredID(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {})
	body := bytes.NewBufferString(`{"cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(respBody, []byte("cred_id")) {
		t.Fatalf("expected cred_id error, got %s", string(respBody))
	}
}

func TestVerifyIdentityMissingCredType(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {})
	body := bytes.NewBufferString(`{"cred_id":"ABCDE1234F"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVerifyIdentityInvalidPANFormat(t *testing.T) {
	t.Parallel()

	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Digio should never be called for an invalid PAN format")
	})
	body := bytes.NewBufferString(`{"cred_id":"INVALID","cred_type":"PAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVerifyIdentityUnsupportedCredType(t *testing.T) {
	t.Parallel()

	// PAN_TO_GST, not UDYAM: UDYAM is a registered VerifierRegistry entry as
	// of Phase 8 (see docs/IMPLEMENTATION_ROADMAP.md), so it's no longer an
	// unsupported cred_type on this endpoint — using it here would still
	// 400, but for "invalid Udyam format" (the id "123" fails that regex),
	// not for being unsupported, silently testing the wrong thing.
	app := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Digio should never be called for an unsupported credential type")
	})
	body := bytes.NewBufferString(`{"cred_id":"123","cred_type":"PAN_TO_GST"}`)
	req := httptest.NewRequest(http.MethodPost, "/verify-identity", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(respBody, []byte("unsupported credential type")) {
		t.Fatalf("expected an unsupported-credential-type error, got %s", string(respBody))
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	h := bootstrap.NewHandlers(nil, nil)
	app := fiber.New()
	app.Get("/health", h.Health.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result)
	}
}
