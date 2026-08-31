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

func TestFssaiHandlerValidateCredData(t *testing.T) {
	t.Parallel()

	handler := testVerifier(t, nil, "FSSAI")

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"12345"}`)); err != nil {
		t.Fatalf("expected a short but present id_no to pass now that regex validation is removed: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{"id_no":"21523064000396"}`)); err != nil {
		t.Fatalf("unexpected error for valid 14-digit FSSAI cred_data: %v", err)
	}

	if err := handler.ValidateCredData(json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing required id_no")
	}
}

func TestFssaiHandlerProcessCurrentFormat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/apis/kyc/fetch_id_data/FSSAI" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"fssai_details": [
				{
					"premiseaddress": "Some Address, Mumbai",
					"licenseno": "21523064000396",
					"licensecategoryname": "Registration",
					"statename": "Maharashtra",
					"licenseactiveflag": true,
					"statusdesc": "License Issued",
					"companyname": "ACME FOODS PVT LTD",
					"licensecategoryid": 3,
					"fboid": 22955209895357833,
					"displayrefid": "30230322113072882"
				}
			]
		}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "FSSAI")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"21523064000396"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if result.CredID != "21523064000396" {
		t.Fatalf("unexpected CredID: %v", result.CredID)
	}
	if result.VerifiedData["status"] != "Active" {
		t.Fatalf("expected licenseactiveflag=true to normalize to \"Active\", got: %v", result.VerifiedData["status"])
	}
	if result.VerifiedData["company_name"] != "ACME FOODS PVT LTD" {
		t.Fatalf("unexpected company_name: %v", result.VerifiedData)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected exactly 2 evidences (request + response), got %d: %+v", len(result.Evidences), result.Evidences)
	}
}

func TestFssaiHandlerProcessLegacyFormat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"License / Registration No.": "21523064000396",
			"Status": "Active",
			"Premises Address": "Some Address, Mumbai",
			"Company Name": "ACME FOODS PVT LTD",
			"Expiry Date": "2027-01-01",
			"Kind of Business": "Manufacturer"
		}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "FSSAI")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"21523064000396"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if result.CredID != "21523064000396" {
		t.Fatalf("unexpected CredID: %v", result.CredID)
	}
	if result.VerifiedData["kind_of_business"] != "Manufacturer" {
		t.Fatalf("unexpected kind_of_business: %v", result.VerifiedData)
	}
}

func TestFssaiHandlerProcessUnexpectedFormatIsSoftFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"something_else": true}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "FSSAI")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"21523064000396"}`))
	if err != nil {
		t.Fatalf("expected a soft failure, not a Go error, got: %v", err)
	}
	if result.Success {
		t.Fatalf("expected failure for an unrecognized response shape, got: %+v", result)
	}
	if len(result.Evidences) != 2 {
		t.Fatalf("expected evidences even for an unrecognized shape (a response was received), got %d", len(result.Evidences))
	}
}

func TestFssaiHandlerProcessAttachesEvidencesOnDigioRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"Invalid FSSAI Number"}`))
	}))
	defer server.Close()

	digioClient := client.NewDigioClient(client.DigioConfig{
		BaseURL: server.URL,
		Token:   "test-token",
		Timeout: 5 * time.Second,
	})
	handler := testVerifier(t, digioClient, "FSSAI")

	result, err := handler.Process(context.Background(), json.RawMessage(`{"id_no":"21523064000396"}`))
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
