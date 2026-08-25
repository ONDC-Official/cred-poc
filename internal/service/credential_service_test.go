package service

import (
	"encoding/json"
	"testing"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/handlers/dto"
	"credential-service/internal/models"

	"github.com/google/uuid"
)

// buildCredData translates the flat wire-level request (cred_type, cred_id,
// name, dob) into the internal per-type cred_data blob the credential
// handlers expect ({"id_no", "name", "dob"}).

func TestBuildCredDataMapsFlatRequestToIDNo(t *testing.T) {
	t.Parallel()

	credData, err := buildCredData(dto.CredentialItem{
		CredType: models.CredTypePAN,
		CredID:   "ABCDE1234F",
		Name:     "John Doe",
		Dob:      "01/01/1990",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(credData, &got); err != nil {
		t.Fatalf("credData is not valid JSON: %v", err)
	}
	if got["id_no"] != "ABCDE1234F" {
		t.Fatalf("expected cred_id to map to id_no, got: %v", got)
	}
	if got["name"] != "John Doe" || got["dob"] != "01/01/1990" {
		t.Fatalf("expected name/dob to pass through, got: %v", got)
	}
}

func TestBuildCredDataGSTIgnoresEmptyNameDob(t *testing.T) {
	t.Parallel()

	credData, err := buildCredData(dto.CredentialItem{
		CredType: models.CredTypeGST,
		CredID:   "29AABCU9603R1ZM",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(credData, &got); err != nil {
		t.Fatalf("credData is not valid JSON: %v", err)
	}
	if got["id_no"] != "29AABCU9603R1ZM" {
		t.Fatalf("expected cred_id to map to id_no, got: %v", got)
	}
}

// buildVerifiedCredential is the mapping logic behind Phase 3 (populating the
// Credential Registry on successful verification). It's tested directly here
// because CredentialService.handleVerificationSuccess also calls
// credReqRepo.UpdateVerificationResult, which is a concrete gorm-backed
// repository with no test double available yet (Phase 0's DB test harness is
// not implemented) — see docs/IMPLEMENTATION_ROADMAP.md Phase 0 and Phase 3.

func TestBuildVerifiedCredentialMapsFields(t *testing.T) {
	t.Parallel()

	credTypeID := uuid.New()
	verifierID := uuid.New()
	issuerID := uuid.New()
	credReq := &models.CredentialRequest{
		ID:       uuid.New(),
		CredType: credTypeID,
		Verifier: &verifierID,
		Issuer:   &issuerID,
	}
	result := &credential.VerificationResult{
		Success: true,
		CredID:  "ABCDE1234F",
		VerifiedData: map[string]any{
			"pan":    "ABCDE1234F",
			"status": "VALID",
		},
		Evidences: []credential.Evidence{
			{Data: map[string]any{"status": "VALID"}, Type: "digio_pan_verification"},
		},
	}
	evidencesJSON, err := json.Marshal(result.Evidences)
	if err != nil {
		t.Fatalf("unexpected error marshaling evidences: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	validity := 365 * 24 * time.Hour

	cred, err := buildVerifiedCredential(credReq, result, evidencesJSON, now, validity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cred.CredID != "ABCDE1234F" {
		t.Fatalf("expected CredID to come from result, got %q", cred.CredID)
	}
	if cred.CredType != credTypeID {
		t.Fatalf("expected CredType to come from credReq, got %v", cred.CredType)
	}
	if cred.Verifier == nil || *cred.Verifier != verifierID {
		t.Fatalf("expected Verifier to be passed through from credReq, got %v", cred.Verifier)
	}
	if cred.Issuer == nil || *cred.Issuer != issuerID {
		t.Fatalf("expected Issuer to be passed through from credReq, got %v", cred.Issuer)
	}
	if cred.ValidFrom == nil || !cred.ValidFrom.Equal(now) {
		t.Fatalf("expected ValidFrom to be now, got %v", cred.ValidFrom)
	}
	if cred.ValidUntil == nil || !cred.ValidUntil.Equal(now.Add(validity)) {
		t.Fatalf("expected ValidUntil to be now+validity, got %v", cred.ValidUntil)
	}
	if cred.LastVerifiedAt == nil || !cred.LastVerifiedAt.Equal(now) {
		t.Fatalf("expected LastVerifiedAt to be now, got %v", cred.LastVerifiedAt)
	}

	var gotVerifiedData map[string]any
	if err := json.Unmarshal(cred.VerifiedData, &gotVerifiedData); err != nil {
		t.Fatalf("VerifiedData did not round-trip as JSON: %v", err)
	}
	if gotVerifiedData["pan"] != "ABCDE1234F" {
		t.Fatalf("expected VerifiedData to contain result.VerifiedData, got %v", gotVerifiedData)
	}

	if cred.Evidences == nil {
		t.Fatal("expected Evidences to be set")
	}
	var gotEvidences []credential.Evidence
	if err := json.Unmarshal(*cred.Evidences, &gotEvidences); err != nil {
		t.Fatalf("Evidences did not round-trip as JSON: %v", err)
	}
	if len(gotEvidences) != 1 || gotEvidences[0].Type != "digio_pan_verification" {
		t.Fatalf("unexpected evidences: %v", gotEvidences)
	}
}

func TestBuildVerifiedCredentialInvalidVerifiedData(t *testing.T) {
	t.Parallel()

	credReq := &models.CredentialRequest{ID: uuid.New(), CredType: uuid.New()}
	result := &credential.VerificationResult{
		Success:      true,
		CredID:       "ABCDE1234F",
		VerifiedData: map[string]any{"bad": make(chan int)}, // unmarshalable
	}

	_, err := buildVerifiedCredential(credReq, result, json.RawMessage(`[]`), time.Now(), time.Hour)
	if err == nil {
		t.Fatal("expected error for unmarshalable VerifiedData")
	}
}

// fakeCredentialCreator lets NewCredentialService be exercised with a
// CredentialCreator test double, confirming the interface is wired the way
// production code (bootstrap.setupCredentialStack) expects.
type fakeCredentialCreator struct {
	created []*models.Credential
}

func (f *fakeCredentialCreator) Create(cred *models.Credential) error {
	f.created = append(f.created, cred)
	return nil
}

func TestNewCredentialServiceAcceptsCredentialCreator(t *testing.T) {
	t.Parallel()

	fake := &fakeCredentialCreator{}
	svc := NewCredentialService(nil, fake, nil, nil, nil, time.Hour)
	if svc.credRepo != fake {
		t.Fatal("expected credRepo to be the provided CredentialCreator")
	}
}

// buildCredentialResultItems is the mapping logic behind GET
// /api/creds/{request_id}. Tested directly (pure function, no DB) for the
// same reason as buildVerifiedCredential above.

func newTestEnumCache() (*models.EnumCache, uuid.UUID, uuid.UUID, uuid.UUID) {
	panTypeID := uuid.New()
	verifiedID := uuid.New()
	pendingID := uuid.New()
	cache := models.NewEnumCache([]models.EnumType{
		{ID: panTypeID, Category: models.CategoryCredType, Value: models.CredTypePAN},
		{ID: verifiedID, Category: models.CategoryCredVerification, Value: models.VerificationVerified},
		{ID: pendingID, Category: models.CategoryCredVerification, Value: models.VerificationPending},
	})
	return cache, panTypeID, verifiedID, pendingID
}

func TestBuildCredentialResultItemsExtractsDigioResponse(t *testing.T) {
	t.Parallel()

	cache, panTypeID, verifiedID, _ := newTestEnumCache()

	evidences := json.RawMessage(`[
		{"type":"request","data":{"id_no":"ABCDE1234F"}},
		{"type":"response","data":{"pan":"ABCDE1234F","status":"VALID"}}
	]`)
	rec := models.CredentialRequest{
		ID:                 uuid.New(),
		CredType:           panTypeID,
		VerificationStatus: verifiedID,
		CredData:           json.RawMessage(`{"id_no":"ABCDE1234F","name":"John Doe","dob":"01/01/1990"}`),
		Evidences:          &evidences,
	}

	items := buildCredentialResultItems([]models.CredentialRequest{rec}, cache)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]
	if item.CredType != models.CredTypePAN {
		t.Fatalf("expected CredType to resolve to its enum value, got %q", item.CredType)
	}
	if item.CredID != "ABCDE1234F" {
		t.Fatalf("expected CredID to be extracted from stored cred_data, got %q", item.CredID)
	}
	if item.Status != models.VerificationVerified {
		t.Fatalf("expected Status to resolve to its enum value, got %q", item.Status)
	}
	if item.DigioResponse == nil {
		t.Fatal("expected DigioResponse to be populated from the evidences' response entry")
	}
	var digioResponse map[string]any
	if err := json.Unmarshal(item.DigioResponse, &digioResponse); err != nil {
		t.Fatalf("DigioResponse is not valid JSON: %v", err)
	}
	if digioResponse["pan"] != "ABCDE1234F" || digioResponse["status"] != "VALID" {
		t.Fatalf("expected DigioResponse to be the exact response evidence, got: %v", digioResponse)
	}
	if item.VerificationErrors != nil {
		t.Fatalf("expected no VerificationErrors when a response was recorded, got: %s", item.VerificationErrors)
	}
}

func TestBuildCredentialResultItemsFallsBackToVerificationErrors(t *testing.T) {
	t.Parallel()

	cache, panTypeID, _, pendingID := newTestEnumCache()

	verificationErrors := json.RawMessage(`{"error":"digio call failed: timeout"}`)
	rec := models.CredentialRequest{
		ID:                 uuid.New(),
		CredType:           panTypeID,
		VerificationStatus: pendingID,
		CredData:           json.RawMessage(`{"id_no":"ABCDE1234F","name":"John Doe","dob":"01/01/1990"}`),
		VerificationErrors: &verificationErrors,
	}

	items := buildCredentialResultItems([]models.CredentialRequest{rec}, cache)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]
	if item.CredID != "ABCDE1234F" {
		t.Fatalf("expected CredID to be extracted from stored cred_data even on failure, got %q", item.CredID)
	}
	if item.DigioResponse != nil {
		t.Fatalf("expected no DigioResponse when no evidences were ever recorded, got: %s", item.DigioResponse)
	}
	if item.VerificationErrors == nil {
		t.Fatal("expected VerificationErrors to be surfaced as a fallback")
	}
	var gotErrors map[string]string
	if err := json.Unmarshal(item.VerificationErrors, &gotErrors); err != nil {
		t.Fatalf("VerificationErrors is not valid JSON: %v", err)
	}
	if gotErrors["error"] != "digio call failed: timeout" {
		t.Fatalf("unexpected VerificationErrors content: %v", gotErrors)
	}
}

func TestBuildCredentialResultItemsUnknownEnumIDDoesNotPanic(t *testing.T) {
	t.Parallel()

	cache := models.NewEnumCache(nil)
	rec := models.CredentialRequest{
		ID:                 uuid.New(),
		CredType:           uuid.New(),
		VerificationStatus: uuid.New(),
	}

	items := buildCredentialResultItems([]models.CredentialRequest{rec}, cache)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].CredType != "" || items[0].Status != "" {
		t.Fatalf("expected empty strings for unresolvable enum IDs, got: %+v", items[0])
	}
}
