package credential_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/service/client"
)

func newTestConfigVerifier(t *testing.T, digioClient *client.DigioClient, parseResponseFn func([]byte) (*credential.VerificationResult, error)) credential.Verifier {
	t.Helper()
	return credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name: "digio",
				Invoke: func(ctx context.Context, payload any) ([]byte, int, error) {
					return digioClient.Call(ctx, http.MethodPost, "/test-endpoint", payload)
				},
			},
		},
		ValidateFn: func(json.RawMessage) error {
			return nil
		},
		BuildRequestFn: func(data json.RawMessage) (any, error) {
			return map[string]string{"echo": string(data)}, nil
		},
		ParseResponseFn: parseResponseFn,
	})
}

func TestConfigVerifierProcessNoEvidencesOnValidationFailure(t *testing.T) {
	t.Parallel()

	handler := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name: "digio",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					t.Fatal("Invoke should not be called when validation fails")
					return nil, 0, nil
				},
			},
		},
		ValidateFn: func(json.RawMessage) error {
			return errors.New("always invalid")
		},
		BuildRequestFn: func(json.RawMessage) (any, error) {
			t.Fatal("BuildRequestFn should not be called when validation fails")
			return nil, nil
		},
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			t.Fatal("ParseResponseFn should not be called when validation fails")
			return nil, nil
		},
	})

	result, err := handler.Process(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if len(result.Evidences) != 0 {
		t.Fatalf("expected no evidences when validation fails before any provider call, got: %+v", result.Evidences)
	}
}

func TestConfigVerifierProcessSuccessHasTwoEvidences(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := newTestConfigVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
		return &credential.VerificationResult{Success: true, CredID: "abc"}, nil
	})

	result, err := handler.Process(context.Background(), json.RawMessage(`"payload"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected 2 evidences, got %d: %+v", len(result.Evidences), result.Evidences)
	}
	if result.Evidences[0].Type != "request" || result.Evidences[1].Type != "response" {
		t.Fatalf("unexpected evidence types: %+v", result.Evidences)
	}
}

func TestConfigVerifierProcessProviderErrorHasTwoEvidences(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := newTestConfigVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
		t.Fatal("ParseResponseFn should not be called on a provider error status")
		return nil, nil
	})

	result, err := handler.Process(context.Background(), json.RawMessage(`"payload"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected 2 evidences even on a provider-side error, got %d: %+v", len(result.Evidences), result.Evidences)
	}
}

func TestConfigVerifierProcessParseErrorReturnsNoResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := newTestConfigVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
		return nil, errors.New("parse failed")
	})

	result, err := handler.Process(context.Background(), json.RawMessage(`"payload"`))
	if err == nil {
		t.Fatal("expected an error when parseResponseFn fails")
	}
	if result != nil {
		t.Fatalf("expected nil result on parse error, got: %+v", result)
	}
}

func TestConfigVerifierProcessFallsBackOnTransportError(t *testing.T) {
	t.Parallel()

	primaryCalled := false
	secondaryCalled := false

	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name: "primary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					primaryCalled = true
					return nil, 0, errors.New("connection refused")
				},
			},
			{
				Name: "secondary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					secondaryCalled = true
					return []byte(`{"ok":true}`), http.StatusOK, nil
				},
			},
		},
		ValidateFn:     func(json.RawMessage) error { return nil },
		BuildRequestFn: func(json.RawMessage) (any, error) { return map[string]string{}, nil },
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			return &credential.VerificationResult{Success: true, CredID: "abc"}, nil
		},
	})

	result, err := verifier.Process(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !primaryCalled || !secondaryCalled {
		t.Fatalf("expected both providers to be attempted, primary=%v secondary=%v", primaryCalled, secondaryCalled)
	}
	if !result.Success {
		t.Fatalf("expected success from secondary provider, got: %+v", result)
	}
	if result.Provider != "secondary" {
		t.Fatalf("expected Provider to be %q, got %q", "secondary", result.Provider)
	}
}

func TestConfigVerifierProcessFallsBackOnFailedResult(t *testing.T) {
	t.Parallel()

	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name: "primary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					return []byte(`{"error":"rejected"}`), http.StatusBadRequest, nil
				},
			},
			{
				Name: "secondary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					return []byte(`{"ok":true}`), http.StatusOK, nil
				},
			},
		},
		ValidateFn:     func(json.RawMessage) error { return nil },
		BuildRequestFn: func(json.RawMessage) (any, error) { return map[string]string{}, nil },
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			return &credential.VerificationResult{Success: true, CredID: "abc"}, nil
		},
	})

	result, err := verifier.Process(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.Provider != "secondary" {
		t.Fatalf("expected fallback to secondary provider to succeed, got: %+v", result)
	}
}

func TestConfigVerifierProcessAllProvidersFailReturnsLastFailure(t *testing.T) {
	t.Parallel()

	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name:   "primary",
				Invoke: func(context.Context, any) ([]byte, int, error) { return nil, 0, errors.New("boom") },
			},
			{
				Name: "secondary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					return []byte(`{"error":"still rejected"}`), http.StatusBadRequest, nil
				},
			},
		},
		ValidateFn:     func(json.RawMessage) error { return nil },
		BuildRequestFn: func(json.RawMessage) (any, error) { return map[string]string{}, nil },
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			t.Fatal("parseResponseFn should not be called for a failure-status response")
			return nil, nil
		},
	})

	result, err := verifier.Process(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected exhausted providers to return a soft failure, not a Go error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.Provider != "secondary" {
		t.Fatalf("expected the last-attempted provider to be recorded, got %q", result.Provider)
	}
}

func TestConfigVerifierProcessSuccessSkipsRemainingProviders(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "TEST",
		Providers: []credential.ProviderInvoker{
			{
				Name:   "primary",
				Invoke: func(context.Context, any) ([]byte, int, error) { return []byte(`{"ok":true}`), http.StatusOK, nil },
			},
			{
				Name: "secondary",
				Invoke: func(context.Context, any) ([]byte, int, error) {
					secondaryCalled = true
					return []byte(`{"ok":true}`), http.StatusOK, nil
				},
			},
		},
		ValidateFn:     func(json.RawMessage) error { return nil },
		BuildRequestFn: func(json.RawMessage) (any, error) { return map[string]string{}, nil },
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			return &credential.VerificationResult{Success: true, CredID: "abc"}, nil
		},
	})

	result, err := verifier.Process(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.Provider != "primary" {
		t.Fatalf("expected primary success, got: %+v", result)
	}
	if secondaryCalled {
		t.Fatal("secondary provider should never be invoked when primary succeeds")
	}
}
