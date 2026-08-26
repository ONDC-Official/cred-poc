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

func TestUdyamHandlerValidateCredData(t *testing.T) {
	t.Parallel()

	handler := testVerifier(t, nil, "UDYAM")

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"INVALID"}`)); err == nil {
		t.Fatal("expected error for invalid Udyam format")
	}

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"UDYAM-MH-01-1234567"}`)); err != nil {
		t.Fatalf("unexpected error for valid hyphenated Udyam cred_data: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"UDYAMMH011234567"}`)); err != nil {
		t.Fatalf("unexpected error for valid compact Udyam cred_data: %v", err)
	}
}

func TestUdyamHandlerProcessNormalizesCompactFormToHyphenated(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/apis/kyc/fetch_id_data/UDYAMAADHAAR" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Udyam Registration Number": "UDYAM-MH-01-1234567"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "UDYAM")

	_, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"udyammh011234567"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPayload["id_no"] != "UDYAM-MH-01-1234567" {
		t.Fatalf("expected compact input to be normalized to canonical hyphenated form before reaching Digio, got: %v", gotPayload)
	}
}

func TestUdyamHandlerProcessAttachesEvidences(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"Udyam Registration Number": "UDYAM-MH-01-1234567",
			"Name of Enterprise": "ACME Enterprises",
			"Major Activity": "Manufacturing",
			"Enterprise Type": [
				{"Enterprise Type": "Micro", "Classification Date": "2020-01-01"}
			],
			"National Industry Classification Code(S)": [
				{"Nic 2 Digit": "10"}
			]
		}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "UDYAM")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"UDYAM-MH-01-1234567"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if result.CredID != "UDYAM-MH-01-1234567" {
		t.Fatalf("unexpected CredID: %v", result.CredID)
	}
	if result.VerifiedData["enterprise_type"] != "Micro" {
		t.Fatalf("unexpected enterprise_type: %v", result.VerifiedData)
	}
	if result.VerifiedData["nic_2_digit"] != "10" {
		t.Fatalf("unexpected nic_2_digit: %v", result.VerifiedData)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected exactly 2 evidences (request + response), got %d: %+v", len(result.Evidences), result.Evidences)
	}
}

func TestUdyamHandlerProcessEmptyUANIsSoftFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "UDYAM")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"UDYAM-MH-01-1234567"}`))
	if err != nil {
		t.Fatalf("expected a soft failure, not a Go error, got: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure for a 200 response with no UAN, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected evidences even for an empty-UAN response, got %d", len(result.Evidences))
	}
}

func TestUdyamHandlerProcessAttachesEvidencesOnDigioRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"Udyam registration not found"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "UDYAM")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"UDYAM-MH-01-1234567"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure for a Digio 404 response, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected evidences even on a Digio-side rejection, got %d: %+v", len(result.Evidences), result.Evidences)
	}
}
