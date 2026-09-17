package credential_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credential-service/internal/service/client"
)

func TestPanHandlerValidateCredDataIDOnly(t *testing.T) {
	t.Parallel()

	handler := testVerifier(t, nil, "PAN")

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"ABCDE1234F"}`)); err != nil {
		t.Fatalf("unexpected error for id-only cred_data: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"not-a-real-pan-format"}`)); err != nil {
		t.Fatalf("expected a format-invalid but present id_no to pass now that regex validation is removed: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing required id_no")
	}
}

func TestPanHandlerProcessSendsIDOnlyToDigio(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/client/kyc/fetch_id_data/PAN" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		if payload["id_no"] != "ABCDE1234F" {
			t.Fatalf("unexpected id_no: %v", payload)
		}
		if _, ok := payload["name"]; ok {
			t.Fatalf("name must not be sent to Digio: %v", payload)
		}
		if _, ok := payload["dob"]; ok {
			t.Fatalf("dob must not be sent to Digio: %v", payload)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pan":"ABCDE1234F","category":"Individual","status":"VALID","full_name":"John Doe"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "PAN")

	credData := json.RawMessage(`{"id_no":"ABCDE1234F"}`)
	result, err := handler.Process(context.Background(), credData)
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

	requestBytes, err := json.Marshal(result.Evidences[0].Data)
	if err != nil {
		t.Fatalf("unexpected error marshaling request evidence: %v", err)
	}
	var requestPayload map[string]string
	if err := json.Unmarshal(requestBytes, &requestPayload); err != nil {
		t.Fatalf("request evidence is not valid JSON: %v", err)
	}
	if requestPayload["id_no"] != "ABCDE1234F" {
		t.Fatalf("expected request evidence id_no only, got: %v", requestPayload)
	}
	if _, ok := requestPayload["name"]; ok {
		t.Fatalf("request evidence must not include name: %v", requestPayload)
	}
	if _, ok := requestPayload["dob"]; ok {
		t.Fatalf("request evidence must not include dob: %v", requestPayload)
	}

	responseBytes, err := json.Marshal(result.Evidences[1].Data)
	if err != nil {
		t.Fatalf("unexpected error marshaling response evidence: %v", err)
	}
	var responsePayload map[string]any
	if err := json.Unmarshal(responseBytes, &responsePayload); err != nil {
		t.Fatalf("response evidence is not valid JSON: %v", err)
	}
	if responsePayload["status"] != "VALID" {
		t.Fatalf("expected response evidence to be the exact body Digio returned, got: %v", responsePayload)
	}
}

func TestPanHandlerProcessDigioRejectionIsFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid Pan Number Detected"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	// PAN.v1.yaml lists only digio, so a Digio rejection is the final answer: an
	// invalid PAN must never come back as a success from a fallback provider.
	handler := testVerifier(t, digioClient, "PAN")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"ABCDE1234F"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected Digio's rejection to be a failure, got: %+v", result)
	}
	if result.Provider != "digio" {
		t.Fatalf("expected Provider to be %q, got %q", "digio", result.Provider)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected request and response evidences from the Digio attempt, got %d: %+v", len(result.Evidences), result.Evidences)
	}
}
