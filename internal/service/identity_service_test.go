package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/provider"
	"credential-service/internal/service"
	"credential-service/internal/service/client"
)

// IdentityService.VerifyIdentity is now a thin resolve+Process wrapper — the
// same function CredentialService.ProcessCredentialRequest calls for the
// async /credential flow, so there is exactly one place that talks to Digio
// for a single credential. HTTP-shaping (400/502/200, raw-body extraction)
// moved to identity_handler.go and is tested there; these tests cover
// VerifyIdentity's own contract: resolve, delegate, return the full
// VerificationResult (or a resolve error) unmodified.

func newTestRegistry(t *testing.T, digioHandler http.HandlerFunc) (*credential.VerifierRegistry, func()) {
	t.Helper()

	server := httptest.NewServer(digioHandler)
	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	typesDir, err := credential.FindDefinitionsDir()
	if err != nil {
		t.Fatal(err)
	}
	providersDir, err := credential.FindProvidersDir()
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := credential.NewGatewayFromProvidersDir(providersDir, map[string]provider.Caller{
		"digio": digioClient,
		"mock":  client.NewMockClient(),
	})
	if err != nil {
		t.Fatalf("NewGatewayFromProvidersDir: %v", err)
	}
	registry, err := credential.NewRegistryFromDir(typesDir, gateway)
	if err != nil {
		t.Fatalf("NewRegistryFromDir: %v", err)
	}
	return registry, server.Close
}

func TestIdentityServiceResolvesPAN(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pan":"ABCDE1234F","category":"Individual","status":"VALID","full_name":"John Doe"}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "ABCDE1234F"})

	result, err := svc.VerifyIdentity(context.Background(), "PAN", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if result.CredID != "ABCDE1234F" {
		t.Fatalf("unexpected CredID: %v", result.CredID)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected 2 evidences (request+response), got %d", len(result.Evidences))
	}
}

func TestIdentityServiceResolvesGST(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error_message":"","corporate_name":"Example Corp","gstin":"29AABCU9603R1ZM","details":{}}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "29AABCU9603R1ZM"})

	result, err := svc.VerifyIdentity(context.Background(), "GST", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.CredID != "29AABCU9603R1ZM" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// FSSAI/UDYAM are registered in the same VerifierRegistry PAN/GST use, so
// they resolve on /verify too, even though docs/IMPLEMENTATION_ROADMAP.md
// originally sketched Phase 8 as "async path only" — that plan didn't
// account for IdentityService.VerifyIdentity being the one shared function
// both paths depend on (see the Round 5 architecture note in CLAUDE.md).
func TestIdentityServiceResolvesFSSAI(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fssai_details":[{"licenseno":"21523064000396","licenseactiveflag":true,"companyname":"ACME FOODS PVT LTD"}]}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "21523064000396"})

	result, err := svc.VerifyIdentity(context.Background(), "FSSAI", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.CredID != "21523064000396" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestIdentityServiceResolvesUDYAM(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Udyam Registration Number":"UDYAM-MH-01-1234567","Name of Enterprise":"ACME Enterprises"}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "UDYAM-MH-01-1234567"})

	result, err := svc.VerifyIdentity(context.Background(), "UDYAM", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.CredID != "UDYAM-MH-01-1234567" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestIdentityServiceDigioRejectionReturnsFailureNotError(t *testing.T) {
	t.Parallel()

	// GST has a single provider (no fallback), so a Digio-side rejection
	// isn't masked by a fallback success — PAN's fallback-to-mock behavior
	// is covered separately in TestIdentityServicePANFallsBackToMock.
	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid GSTIN"}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "29AABCU9603R1ZM"})

	result, err := svc.VerifyIdentity(context.Background(), "GST", credData)
	if err != nil {
		t.Fatalf("expected a Digio-side rejection to surface as Success:false, not a Go error, got: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected evidences even on a Digio-side rejection (a response was received), got %d", len(result.Evidences))
	}
}

func TestIdentityServicePANDigioRejectionIsFailure(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid Pan Number Detected"}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "ABCDE1234F"})

	result, err := svc.VerifyIdentity(context.Background(), "PAN", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PAN has no fallback provider: Digio's rejection is the result.
	if result.Success || result.Provider != "digio" {
		t.Fatalf("expected Digio's rejection to be a failure from digio, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected request and response evidences, got %d", len(result.Evidences))
	}
}

func TestIdentityServiceMissingRequiredFieldReturnsFailureNotError(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no provider should be called when a required field is missing")
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": ""})

	result, err := svc.VerifyIdentity(context.Background(), "PAN", credData)
	if err != nil {
		t.Fatalf("expected a validation failure to surface as Success:false, not a Go error, got: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure, got: %+v", result)
	}
	if len(result.Evidences) != 0 {
		t.Fatalf("expected no evidences (no provider call was made), got %d", len(result.Evidences))
	}
}

func TestIdentityServiceFormatInvalidButPresentIDPassesValidation(t *testing.T) {
	t.Parallel()

	// Regex validation was removed — a format-invalid but present id_no now
	// reaches the provider instead of being rejected before any call.
	called := false
	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pan":"INVALID","category":"Individual","status":"VALID","full_name":"John Doe"}`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "INVALID"})

	result, err := svc.VerifyIdentity(context.Background(), "PAN", credData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected the provider to be called now that regex validation is removed")
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
}

func TestIdentityServiceSuccessNonJSONBodyReturnsError(t *testing.T) {
	t.Parallel()

	// Note: a non-2xx status with a non-JSON body is NOT an error at this
	// layer — Process()'s statusCode>=400 branch never calls parseResponseFn,
	// so it returns Success:false with the raw bytes preserved as evidence,
	// no error. That's by design (the evidences pipeline just needs raw
	// bytes) — the JSON-validity guarantee for HTTP responses now lives in
	// identity_handler.go, tested there. This test instead covers the case
	// parseResponseFn DOES run (a 2xx status) and fails to parse.
	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>not json</html>`))
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)
	credData, _ := json.Marshal(map[string]string{"id_no": "ABCDE1234F"})

	if _, err := svc.VerifyIdentity(context.Background(), "PAN", credData); err == nil {
		t.Fatal("expected an error when a 2xx Digio response body isn't valid JSON (parseResponseFn fails)")
	}
}

func TestIdentityServiceUnsupportedType(t *testing.T) {
	t.Parallel()

	registry, closeServer := newTestRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Digio should never be called for an unsupported credential type")
	})
	defer closeServer()

	svc := service.NewIdentityService(registry)

	_, err := svc.VerifyIdentity(context.Background(), "UNKNOWN", json.RawMessage(`{}`))
	if !errors.Is(err, credential.ErrUnsupportedCredType) {
		t.Fatalf("expected ErrUnsupportedCredType, got %v", err)
	}
}
