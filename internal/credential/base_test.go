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

func newTestDigioVerifier(t *testing.T, digioClient *client.DigioClient, parseResponseFn func([]byte) (*credential.VerificationResult, error)) credential.Verifier {
	t.Helper()
	return credential.NewDigioVerifier(credential.DigioVerifierConfig{
		Client:   digioClient,
		Endpoint: "/test-endpoint",
		CredType: "TEST",
		ValidateFn: func(json.RawMessage) error {
			return nil
		},
		BuildRequestFn: func(data json.RawMessage) (any, error) {
			return map[string]string{"echo": string(data)}, nil
		},
		ParseResponseFn: parseResponseFn,
	})
}

func TestDigioVerifierProcessNoEvidencesOnValidationFailure(t *testing.T) {
	t.Parallel()

	handler := credential.NewDigioVerifier(credential.DigioVerifierConfig{
		Client:   nil,
		Endpoint: "/test-endpoint",
		CredType: "TEST",
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
		t.Fatalf("expected no evidences when validation fails before any Digio call, got: %+v", result.Evidences)
	}
}

func TestDigioVerifierProcessSuccessHasTwoEvidences(t *testing.T) {
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
	handler := newTestDigioVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
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

func TestDigioVerifierProcessDigioErrorHasTwoEvidences(t *testing.T) {
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
	handler := newTestDigioVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
		t.Fatal("ParseResponseFn should not be called on a Digio error status")
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
		t.Fatalf("expected 2 evidences even on a Digio-side error, got %d: %+v", len(result.Evidences), result.Evidences)
	}
}

func TestDigioVerifierProcessParseErrorReturnsNoResult(t *testing.T) {
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
	handler := newTestDigioVerifier(t, digioClient, func(body []byte) (*credential.VerificationResult, error) {
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
