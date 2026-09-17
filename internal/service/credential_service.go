package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"credential-service/internal/credential"
	"credential-service/internal/handlers/dto"
	"credential-service/internal/models"
	"credential-service/internal/repository"
	"credential-service/internal/telemetry"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

var ErrRequestNotFound = errors.New("credential request not found")

// CredentialCreator is the subset of CredentialRepository that
// CredentialService needs to write to the Credential Registry. Kept as an
// interface (rather than *repository.CredentialRepository directly) so
// handleVerificationSuccess's field-mapping logic can be unit-tested with a
// fake, without a real database.
type CredentialCreator interface {
	Create(ctx context.Context, cred *models.Credential) error
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

func (s *CredentialService) SubmitCredentials(ctx context.Context, req *dto.SubmitCredentialsRequest) (*dto.SubmitCredentialsResponse, error) {
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

	pendingID := s.enumCache.VerificationStatusID(models.VerificationPending)
	payloadHash := computePayloadHash(req.ParticipantID, req.Credentials)

	// build constructs a fresh set of records for this submission. It only
	// runs if CreateManyIfNotInFlight determines no identical, still-PENDING
	// submission exists for this participant_id — see its doc comment.
	build := func() []models.CredentialRequest {
		requestID := uuid.New()
		records := make([]models.CredentialRequest, 0, len(req.Credentials))
		for i, item := range req.Credentials {
			credTypeID := s.enumCache.CredTypeID(item.CredType)

			var issuer *uuid.UUID
			if v := s.registry.IssuerFor(item.CredType); v != "" {
				id := s.enumCache.IssuerID(v)
				issuer = &id
			}

			records = append(records, models.CredentialRequest{
				ID:                 uuid.New(),
				RequestID:          requestID,
				ParticipantID:      req.ParticipantID,
				PayloadHash:        payloadHash,
				CredType:           credTypeID,
				VerificationStatus: pendingID,
				RetryCount:         0,
				MaxRetries:         3,
				CredData:           credDataByItem[i],
				Issuer:             issuer,
			})
		}
		return records
	}

	records, reused, err := s.credReqRepo.CreateManyIfNotInFlight(ctx, req.ParticipantID, payloadHash, pendingID, build)
	if err != nil {
		return nil, fmt.Errorf("failed to persist credential requests: %w", err)
	}

	telemetry.RecordSubmission(ctx, reused)
	if reused {
		slog.InfoContext(ctx, "credential request reused in-flight request (duplicate payload)",
			"participant_id", req.ParticipantID, "request_id", records[0].RequestID)
	} else {
		for _, rec := range records {
			select {
			case s.jobCh <- rec.ID:
			default:
				slog.WarnContext(ctx, "job channel full, credential request may be delayed", "credential_request_id", rec.ID)
			}
		}
	}

	resp := &dto.SubmitCredentialsResponse{
		RequestID:   records[0].RequestID.String(),
		Credentials: buildSubmitCredentialItems(records, s.enumCache),
	}

	return resp, nil
}

// computePayloadHash fingerprints a /credential submission (participant_id +
// the full credentials payload) so CreateManyIfNotInFlight can detect a
// duplicate submission before it resolves. Credentials are sorted first so
// re-ordering the same set of items doesn't defeat dedup.
func computePayloadHash(participantID string, credentials []dto.CredentialItem) string {
	sorted := make([]dto.CredentialItem, len(credentials))
	copy(sorted, credentials)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.CredType != b.CredType {
			return a.CredType < b.CredType
		}
		if a.CredID != b.CredID {
			return a.CredID < b.CredID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Dob < b.Dob
	})

	payload := struct {
		ParticipantID string               `json:"participant_id"`
		Credentials   []dto.CredentialItem `json:"credentials"`
	}{ParticipantID: participantID, Credentials: sorted}

	// Marshal cannot fail here: payload is built entirely of strings/slices.
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildSubmitCredentialItems maps persisted CredentialRequest rows onto the
// POST /credential response shape. Used for both newly created rows and
// rows reused from an in-flight duplicate submission, so the response
// always reflects the actual stored records.
func buildSubmitCredentialItems(records []models.CredentialRequest, enumCache *models.EnumCache) []dto.CredentialItemResponse {
	items := make([]dto.CredentialItemResponse, 0, len(records))
	for _, rec := range records {
		item := dto.CredentialItemResponse{ID: rec.ID.String()}
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

		items = append(items, item)
	}
	return items
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
	credReq, err := s.credReqRepo.FindByID(ctx, reqID)
	if err != nil {
		return fmt.Errorf("find credential request: %w", err)
	}

	enumType := s.enumCache.Get(credReq.CredType)
	if enumType == nil {
		return fmt.Errorf("unknown cred_type enum ID: %s", credReq.CredType)
	}
	trace.SpanFromContext(ctx).SetAttributes(telemetry.AttrCredType.String(enumType.Value))

	result, err := s.identityService.VerifyIdentity(ctx, enumType.Value, credReq.CredData)
	if err != nil {
		return s.handleProcessingError(ctx, enumType.Value, credReq, err)
	}

	if !result.Success {
		return s.handleVerificationFailure(ctx, enumType.Value, credReq, result)
	}

	return s.handleVerificationSuccess(ctx, enumType.Value, credReq, result)
}

// verifierIDFromProvider resolves the CRED_VERIFIER enum row for the
// provider that produced a VerificationResult, so credential_requests.verifier
// reflects whichever provider actually ran (not a hardcoded default) —
// nil when no provider was ever invoked (e.g. a pre-provider validation
// failure).
func (s *CredentialService) verifierIDFromProvider(providerName string) *uuid.UUID {
	if providerName == "" {
		return nil
	}
	id := s.enumCache.VerifierID(strings.ToUpper(providerName))
	return &id
}

func (s *CredentialService) handleVerificationSuccess(ctx context.Context, credType string, credReq *models.CredentialRequest, result *credential.VerificationResult) error {
	verifiedID := s.enumCache.VerificationStatusID(models.VerificationVerified)
	verifierID := s.verifierIDFromProvider(result.Provider)
	credReq.Verifier = verifierID

	evidencesJSON, err := json.Marshal(result.Evidences)
	if err != nil {
		return fmt.Errorf("marshal evidences: %w", err)
	}
	if err := s.credReqRepo.UpdateVerificationResult(ctx, credReq.ID, verifiedID, verifierID, evidencesJSON); err != nil {
		return fmt.Errorf("update verification result: %w", err)
	}

	cred, err := buildVerifiedCredential(credReq, result, evidencesJSON, time.Now(), s.defaultValidity)
	if err != nil {
		return fmt.Errorf("build credential registry entry: %w", err)
	}
	if err := s.credRepo.Create(ctx, cred); err != nil {
		return fmt.Errorf("create credential registry entry: %w", err)
	}

	telemetry.RecordWorkerProcessed(ctx, credType, telemetry.WorkerVerified)
	slog.InfoContext(ctx, "credential request verified", "credential_request_id", credReq.ID)
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

func (s *CredentialService) handleVerificationFailure(ctx context.Context, credType string, credReq *models.CredentialRequest, result *credential.VerificationResult) error {
	failedID := s.enumCache.VerificationStatusID(models.VerificationFailed)
	verifierID := s.verifierIDFromProvider(result.Provider)

	errJSON, _ := json.Marshal(map[string]string{"error": result.Error})

	// TODO: Retry classification is TBD per spec. For now, check retry_count < max_retries.
	if credReq.RetryCount < credReq.MaxRetries {
		if err := s.credReqRepo.RecordRetry(ctx, credReq.ID); err != nil {
			slog.ErrorContext(ctx, "recording retry failed", "credential_request_id", credReq.ID, "error", err)
		}
		pendingID := s.enumCache.VerificationStatusID(models.VerificationPending)
		if err := s.credReqRepo.RecordFailure(ctx, credReq.ID, pendingID, verifierID, errJSON); err != nil {
			return fmt.Errorf("record retry failure: %w", err)
		}
		telemetry.RecordWorkerProcessed(ctx, credType, telemetry.WorkerRetry)
		slog.InfoContext(ctx, "credential request scheduled for retry",
			"credential_request_id", credReq.ID, "attempt", credReq.RetryCount+1, "max_retries", credReq.MaxRetries)

		select {
		case s.jobCh <- credReq.ID:
		default:
			slog.WarnContext(ctx, "job channel full, retry may be delayed", "credential_request_id", credReq.ID)
		}
		return nil
	}

	if err := s.credReqRepo.RecordFailure(ctx, credReq.ID, failedID, verifierID, errJSON); err != nil {
		return fmt.Errorf("record final failure: %w", err)
	}
	telemetry.RecordWorkerProcessed(ctx, credType, telemetry.WorkerFailed)
	slog.WarnContext(ctx, "credential request failed permanently", "credential_request_id", credReq.ID)
	return nil
}

func (s *CredentialService) handleProcessingError(ctx context.Context, credType string, credReq *models.CredentialRequest, processErr error) error {
	failedID := s.enumCache.VerificationStatusID(models.VerificationFailed)
	errJSON, _ := json.Marshal(map[string]string{"error": processErr.Error()})

	if err := s.credReqRepo.RecordFailure(ctx, credReq.ID, failedID, nil, errJSON); err != nil {
		return fmt.Errorf("record processing error: %w", err)
	}
	telemetry.RecordWorkerProcessed(ctx, credType, telemetry.WorkerFailed)
	slog.ErrorContext(ctx, "credential request processing error", "credential_request_id", credReq.ID, "error", processErr)
	return nil
}

// GetResults returns the per-credential results of a previously submitted
// /credential request. The provider's raw response is only included when
// verbose is true.
func (s *CredentialService) GetResults(ctx context.Context, requestID uuid.UUID, verbose bool) (*dto.GetCredentialResultsResponse, error) {
	records, err := s.credReqRepo.FindByRequestID(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("find credential requests: %w", err)
	}
	if len(records) == 0 {
		return nil, ErrRequestNotFound
	}

	return &dto.GetCredentialResultsResponse{
		RequestID:   requestID.String(),
		Credentials: buildCredentialResultItems(records, s.enumCache, verbose),
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
// The provider's raw response is only attached when verbose is true.
func buildCredentialResultItems(records []models.CredentialRequest, enumCache *models.EnumCache, verbose bool) []dto.CredentialResultItem {
	items := make([]dto.CredentialResultItem, 0, len(records))
	for _, rec := range records {
		item := dto.CredentialResultItem{ID: rec.ID.String()}

		if et := enumCache.Get(rec.CredType); et != nil {
			item.CredType = et.Value
		}
		if et := enumCache.Get(rec.VerificationStatus); et != nil {
			item.Status = et.Value
		}
		if rec.Verifier != nil {
			if et := enumCache.Get(*rec.Verifier); et != nil {
				item.Provider = et.Value
			}
		}

		var storedCredData struct {
			IDNo string `json:"id_no"`
		}
		if err := json.Unmarshal(rec.CredData, &storedCredData); err == nil {
			item.CredID = storedCredData.IDNo
		}

		var hasResponseEvidence bool
		if verbose && rec.Evidences != nil {
			var entries []storedEvidence
			if err := json.Unmarshal(*rec.Evidences, &entries); err == nil {
				for _, e := range entries {
					if e.Type == "response" {
						item.ProviderResponse = e.Data
						hasResponseEvidence = true
						break
					}
				}
			}
		}
		if !hasResponseEvidence && rec.VerificationErrors != nil {
			item.VerificationErrors = *rec.VerificationErrors
		}

		items = append(items, item)
	}
	return items
}
