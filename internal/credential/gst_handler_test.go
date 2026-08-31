package credential_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credential-service/internal/service/client"
)

func TestGstHandlerValidateCredData(t *testing.T) {
	t.Parallel()

	handler := testVerifier(t, nil, "GST")

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"INVALID"}`)); err != nil {
		t.Fatalf("expected a format-invalid but present id_no to pass now that regex validation is removed: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"29AABCU9603R1ZM"}`)); err != nil {
		t.Fatalf("unexpected error for valid GST cred_data: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing required id_no")
	}
}

func TestGstHandlerProcessAttachesEvidences(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/client/kyc/fetch_id_data/GST" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error_message":"","corporate_name":"Example Corp","gstin":"29AABCU9603R1ZM","details":{}}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "GST")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"29AABCU9603R1ZM"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected exactly 2 evidences (request + response), got %d: %+v", len(result.Evidences), result.Evidences)
	}
	if result.Evidences[0].Type != "request" || result.Evidences[1].Type != "response" {
		t.Fatalf("unexpected evidence types: %+v", result.Evidences)
	}
}

func TestGstHandlerProcessAttachesEvidencesOnDigioRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid GSTIN"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "GST")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"29AABCU9603R1ZM"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure for a Digio 400 response, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected evidences even on a Digio-side rejection, got %d: %+v", len(result.Evidences), result.Evidences)
	}
}
