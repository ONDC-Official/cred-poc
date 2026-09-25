package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"credential-service/internal/credential"
	"credential-service/internal/models"
	"credential-service/internal/repository"

	"github.com/google/uuid"
)

// VerifyIdentityLogger persists one credential_requests row per /verify call.
//
// Rows carry a terminal status and empty participant_id/payload_hash, which keeps them
// out of the worker (it polls PENDING) and out of the duplicate-submission check (it
// matches a real participant_id). The read API selects them by that same empty pair.
type VerifyIdentityLogger struct {
	repo      *repository.CredentialRequestRepository
	enumCache *models.EnumCache
	registry  *credential.VerifierRegistry
}

func NewVerifyIdentityLogger(
	repo *repository.CredentialRequestRepository,
	enumCache *models.EnumCache,
	registry *credential.VerifierRegistry,
) *VerifyIdentityLogger {
	return &VerifyIdentityLogger{repo: repo, enumCache: enumCache, registry: registry}
}

// VerifyIdentityLogEntry is what the handler knows at the end of a request. Result is
// nil when no verification ran, and Error then carries the handler's own message.
type VerifyIdentityLogEntry struct {
	SubscriberID string
	CredType     string
	CredData     json.RawMessage
	RequestBody  []byte
	ResponseBody []byte
	Result       *credential.VerificationResult
	Error        string
}

// Record writes the row. A nil logger is a no-op.
func (l *VerifyIdentityLogger) Record(ctx context.Context, entry VerifyIdentityLogEntry) error {
	if l == nil || l.repo == nil || l.enumCache == nil {
		return nil
	}

	// cred_type is a NOT NULL foreign key, so a type with no enum row cannot be stored.
	credTypeID := l.enumCache.CredTypeID(entry.CredType)
	if credTypeID == uuid.Nil {
		return fmt.Errorf("no CRED_TYPE enum row for %q", entry.CredType)
	}

	status := models.VerificationFailed
	if entry.Result != nil && entry.Result.Success {
		status = models.VerificationVerified
	}

	row := &models.CredentialRequest{
		ID:                 uuid.New(),
		RequestID:          uuid.New(),
		SubscriberID:       entry.SubscriberID,
		CredType:           credTypeID,
		VerificationStatus: l.enumCache.VerificationStatusID(status),
		CredData:           entry.CredData,
		RequestBody:        bodyText(entry.RequestBody),
		ResponseBody:       bodyText(entry.ResponseBody),
	}

	if issuer := l.registry.IssuerFor(entry.CredType); issuer != "" {
		if id := l.enumCache.IssuerID(issuer); id != uuid.Nil {
			row.Issuer = &id
		}
	}

	if entry.Result != nil {
		if entry.Result.Provider != "" {
			if id := l.enumCache.VerifierID(strings.ToUpper(entry.Result.Provider)); id != uuid.Nil {
				row.Verifier = &id
			}
		}
		if len(entry.Result.Evidences) > 0 {
			evidences, err := json.Marshal(entry.Result.Evidences)
			if err != nil {
				return fmt.Errorf("marshal evidences: %w", err)
			}
			raw := json.RawMessage(evidences)
			row.Evidences = &raw
		}
	}

	if message := entry.errorMessage(); message != "" {
		errJSON, err := json.Marshal(map[string]string{"error": message})
		if err != nil {
			return fmt.Errorf("marshal verification errors: %w", err)
		}
		raw := json.RawMessage(errJSON)
		row.VerificationErrors = &raw
	}

	return l.repo.Create(ctx, row)
}

// bodyText keeps the body exactly as it arrived; nil leaves the column NULL.
func bodyText(body []byte) *string {
	if len(body) == 0 {
		return nil
	}
	text := string(body)
	return &text
}

func (e VerifyIdentityLogEntry) errorMessage() string {
	if e.Error != "" {
		return e.Error
	}
	if e.Result != nil {
		return e.Result.Error
	}
	return ""
}
