package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/handlers/dto"
	"credential-service/internal/models"
	"credential-service/internal/repository"

	"github.com/google/uuid"
)

var ErrRequestNotFound = errors.New("credential request not found")

// CredentialCreator is the subset of CredentialRepository that
// CredentialService needs to write to the Credential Registry. Kept as an
// interface (rather than *repository.CredentialRepository directly) so
// handleVerificationSuccess's field-mapping logic can be unit-tested with a
// fake, without a real database.
type CredentialCreator interface {
	Create(cred *models.Credential) error
}

type CredentialService struct {
	credReqRepo *repository.CredentialRequestRepository
	credRepo    CredentialCreator
	// registry is used only for SubmitCredentials' pre-persistence
	// validation (ValidateCredData) — cheap, synchronous, never calls
	// Digio. The actual Digio verification always goes through
	// identityService.VerifyIdentity, the same function /verify-identity
	// uses, so there is exactly one place in the codebase that talks to
	// Digio for a single credential.
	registry        *credential.VerifierRegistry
	identityService *IdentityService
	enumCache       *models.EnumCache
	jobCh           chan uuid.UUID
	defaultValidity time.Duration
}

func NewCredentialService(
	credReqRepo *repository.CredentialRequestRepository,
	credRepo CredentialCreator,
	registry *credential.VerifierRegistry,
	identityService *IdentityService,
	enumCache *models.EnumCache,
	defaultValidity time.Duration,
) *CredentialService {
	return &CredentialService{
		credReqRepo:     credReqRepo,
		credRepo:        credRepo,
		registry:        registry,
		identityService: identityService,
		enumCache:       enumCache,
		jobCh:           make(chan uuid.UUID, 100),
		defaultValidity: defaultValidity,
	}
}

func (s *CredentialService) JobChannel() <-chan uuid.UUID {
	return s.jobCh
}

func (s *CredentialService) SubmitCredentials(req *dto.SubmitCredentialsRequest) (*dto.SubmitCredentialsResponse, error) {
	credDataByItem := make([]json.RawMessage, len(req.Credentials))
	for i, item := range req.Credentials {
		verifier, err := s.registry.Resolve(item.CredType)
		if err != nil {
			return nil, fmt.Errorf("credential type %q: %w", item.CredType, err)
		}

		credData, err := buildCredData(item)
		if err != nil {
			return nil, fmt.Errorf("credential type %q: build cred_data: %w", item.CredType, err)
		}
		if err := verifier.ValidateCredData(credData); err != nil {
			return nil, fmt.Errorf("credential type %q validation: %w", item.CredType, err)
		}
		credDataByItem[i] = credData
	}

	requestID := uuid.New()
	pendingID := s.enumCache.VerificationStatusID(models.VerificationPending)
	verifierID := s.enumCache.VerifierID(models.VerifierDigio)

	var records []models.CredentialRequest
	for i, item := range req.Credentials {
		credTypeID := s.enumCache.CredTypeID(item.CredType)

		var issuer *uuid.UUID
		if v := s.registry.IssuerFor(item.CredType); v != "" {
			id := s.enumCache.IssuerID(v)
			issuer = &id
		}

		rec := models.CredentialRequest{
			ID:                 uuid.New(),
			RequestID:          requestID,
			CredType:           credTypeID,
			VerificationStatus: pendingID,
			RetryCount:         0,
			MaxRetries:         3,
			CredData:           credDataByItem[i],
			Issuer:             issuer,
			Verifier:           &verifierID,
		}
		records = append(records, rec)
	}

	if err := s.credReqRepo.CreateMany(records); err != nil {
		return nil, fmt.Errorf("failed to persist credential requests: %w", err)
	}

	for _, rec := range records {
		select {
		case s.jobCh <- rec.ID:
		default:
			log.Printf("warning: job channel full, credential request %s may be delayed", rec.ID)
		}
	}

	resp := &dto.SubmitCredentialsResponse{
		RequestID: requestID.String(),
	}
	for i, rec := range records {
		resp.Credentials = append(resp.Credentials, dto.CredentialItemResponse{
			ID:       rec.ID.String(),
			CredType: req.Credentials[i].CredType,
			CredID:   req.Credentials[i].CredID,
			Status:   models.VerificationPending,
		})
	}

	return resp, nil
}

// buildCredData translates the flat wire-level request shape (cred_type,
// cred_id, optional name/dob) into the internal per-type cred_data blob the
// credential handlers expect ({"id_no", "name", "dob"}). Digio request
// builders only pull fields declared in each type's YAML (PAN/GST: id_no).
func buildCredData(item dto.CredentialItem) (json.RawMessage, error) {
	return json.Marshal(map[string]string{
		"id_no": item.CredID,
		"name":  item.Name,
		"dob":   item.Dob,
	})
}

func (s *CredentialService) ProcessCredentialRequest(ctx context.Context, reqID uuid.UUID) error {
	credReq, err := s.credReqRepo.FindByID(reqID)
	if err != nil {
		return fmt.Errorf("find credential request: %w", err)
	}

	enumType := s.enumCache.Get(credReq.CredType)
	if enumType == nil {
		return fmt.Errorf("unknown cred_type enum ID: %s", credReq.CredType)
	}

	result, err := s.identityService.VerifyIdentity(ctx, enumType.Value, credReq.CredData)
	if err != nil {
		return s.handleProcessingError(credReq, err)
	}

	if !result.Success {
		return s.handleVerificationFailure(credReq, result)
	}

	return s.handleVerificationSuccess(credReq, result)
}

func (s *CredentialService) handleVerificationSuccess(credReq *models.CredentialRequest, result *credential.VerificationResult) error {
	verifiedID := s.enumCache.VerificationStatusID(models.VerificationVerified)

	evidencesJSON, err := json.Marshal(result.Evidences)
	if err != nil {
		return fmt.Errorf("marshal evidences: %w", err)
	}
	if err := s.credReqRepo.UpdateVerificationResult(credReq.ID, verifiedID, evidencesJSON); err != nil {
		return fmt.Errorf("update verification result: %w", err)
	}

	cred, err := buildVerifiedCredential(credReq, result, evidencesJSON, time.Now(), s.defaultValidity)
	if err != nil {
		return fmt.Errorf("build credential registry entry: %w", err)
	}
	if err := s.credRepo.Create(cred); err != nil {
		return fmt.Errorf("create credential registry entry: %w", err)
	}

	log.Printf("credential request %s verified successfully", credReq.ID)
	return nil
}

// buildVerifiedCredential maps a successful VerificationResult onto a new
// Credential Registry row. Pulled out of handleVerificationSuccess so this
// mapping can be unit-tested without a database (see credential_service_test.go).
func buildVerifiedCredential(credReq *models.CredentialRequest, result *credential.VerificationResult, evidencesJSON json.RawMessage, now time.Time, validity time.Duration) (*models.Credential, error) {
	verifiedDataJSON, err := json.Marshal(result.VerifiedData)
	if err != nil {
		return nil, fmt.Errorf("marshal credential data: %w", err)
	}

	evidencesRaw := json.RawMessage(evidencesJSON)
	validUntil := now.Add(validity)
	return &models.Credential{
		CredID:         result.CredID,
		CredType:       credReq.CredType,
		VerifiedData:   verifiedDataJSON,
		Evidences:      &evidencesRaw,
		Issuer:         credReq.Issuer,
		Verifier:       credReq.Verifier,
		ValidFrom:      &now,
		ValidUntil:     &validUntil,
		LastVerifiedAt: &now,
	}, nil
}

func (s *CredentialService) handleVerificationFailure(credReq *models.CredentialRequest, result *credential.VerificationResult) error {
	failedID := s.enumCache.VerificationStatusID(models.VerificationFailed)

	errJSON, _ := json.Marshal(map[string]string{"error": result.Error})

	// TODO: Retry classification is TBD per spec. For now, check retry_count < max_retries.
	if credReq.RetryCount < credReq.MaxRetries {
		if err := s.credReqRepo.RecordRetry(credReq.ID); err != nil {
			log.Printf("error recording retry for %s: %v", credReq.ID, err)
		}
		pendingID := s.enumCache.VerificationStatusID(models.VerificationPending)
		if err := s.credReqRepo.RecordFailure(credReq.ID, pendingID, errJSON); err != nil {
			return fmt.Errorf("record retry failure: %w", err)
		}
		log.Printf("credential request %s scheduled for retry (%d/%d)", credReq.ID, credReq.RetryCount+1, credReq.MaxRetries)

		select {
		case s.jobCh <- credReq.ID:
		default:
			log.Printf("warning: job channel full, retry for %s may be delayed", credReq.ID)
		}
		return nil
	}

	if err := s.credReqRepo.RecordFailure(credReq.ID, failedID, errJSON); err != nil {
		return fmt.Errorf("record final failure: %w", err)
	}
	log.Printf("credential request %s failed permanently", credReq.ID)
	return nil
}

func (s *CredentialService) handleProcessingError(credReq *models.CredentialRequest, processErr error) error {
	failedID := s.enumCache.VerificationStatusID(models.VerificationFailed)
	errJSON, _ := json.Marshal(map[string]string{"error": processErr.Error()})

	if err := s.credReqRepo.RecordFailure(credReq.ID, failedID, errJSON); err != nil {
		return fmt.Errorf("record processing error: %w", err)
	}
	log.Printf("credential request %s processing error: %v", credReq.ID, processErr)
	return nil
}

// GetResults returns the per-credential results of a previously submitted
// /credential request, including Digio's raw response once processed.
func (s *CredentialService) GetResults(requestID uuid.UUID) (*dto.GetCredentialResultsResponse, error) {
	records, err := s.credReqRepo.FindByRequestID(requestID)
	if err != nil {
		return nil, fmt.Errorf("find credential requests: %w", err)
	}
	if len(records) == 0 {
		return nil, ErrRequestNotFound
	}

	return &dto.GetCredentialResultsResponse{
		RequestID:   requestID.String(),
		Credentials: buildCredentialResultItems(records, s.enumCache),
	}, nil
}

// storedEvidence mirrors credential.Evidence but keeps Data as raw bytes so
// the request/response payloads can be extracted verbatim, with no
// intermediate map[string]any round-trip that would reorder keys or lose
// numeric precision.
type storedEvidence struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// buildCredentialResultItems maps persisted CredentialRequest rows onto the
// GET /credential response shape. Pulled out as a pure function (no DB
// access) so it can be unit-tested directly — see credential_service_test.go.
func buildCredentialResultItems(records []models.CredentialRequest, enumCache *models.EnumCache) []dto.CredentialResultItem {
	items := make([]dto.CredentialResultItem, 0, len(records))
	for _, rec := range records {
		item := dto.CredentialResultItem{ID: rec.ID.String()}

		if et := enumCache.Get(rec.CredType); et != nil {
			item.CredType = et.Value
		}
		if et := enumCache.Get(rec.VerificationStatus); et != nil {
			item.Status = et.Value
		}

		var storedCredData struct {
			IDNo string `json:"id_no"`
		}
		if err := json.Unmarshal(rec.CredData, &storedCredData); err == nil {
			item.CredID = storedCredData.IDNo
		}

		if rec.Evidences != nil {
			var entries []storedEvidence
			if err := json.Unmarshal(*rec.Evidences, &entries); err == nil {
				for _, e := range entries {
					if e.Type == "response" {
						item.DigioResponse = e.Data
						break
					}
				}
			}
		}
		if item.DigioResponse == nil && rec.VerificationErrors != nil {
			item.VerificationErrors = *rec.VerificationErrors
		}

		items = append(items, item)
	}
	return items
}
