package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"credential-service/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CredentialRequestRepository struct {
	db *gorm.DB
}

func NewCredentialRequestRepository(db *gorm.DB) *CredentialRequestRepository {
	return &CredentialRequestRepository{db: db}
}

func (r *CredentialRequestRepository) Create(ctx context.Context, req *models.CredentialRequest) error {
	return r.db.WithContext(ctx).Create(req).Error
}

// CreateManyIfNotInFlight checks whether a request for the same
// participantID + payloadHash is already in pendingStatusID (i.e. a prior
// submission with an identical payload is still being processed), and if
// so returns those existing records instead of creating duplicates. The
// check and insert run inside one transaction serialized by a Postgres
// advisory lock keyed on participantID+payloadHash, so two identical
// requests arriving at the same instant can't both pass the check and both
// insert — the second blocks until the first commits, then finds and
// reuses the first's rows.
func (r *CredentialRequestRepository) CreateManyIfNotInFlight(
	ctx context.Context,
	participantID, payloadHash string,
	pendingStatusID uuid.UUID,
	build func() []models.CredentialRequest,
) (records []models.CredentialRequest, reused bool, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockKey := participantID + ":" + payloadHash
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", lockKey).Error; err != nil {
			return fmt.Errorf("acquire dedup lock: %w", err)
		}

		var existing []models.CredentialRequest
		if err := tx.Where(
			"participant_id = ? AND payload_hash = ? AND verification_status = ?",
			participantID, payloadHash, pendingStatusID,
		).Order("created_at ASC").Find(&existing).Error; err != nil {
			return fmt.Errorf("check in-flight credential requests: %w", err)
		}
		if len(existing) > 0 {
			records, reused = existing, true
			return nil
		}

		records = build()
		for i := range records {
			if err := tx.Create(&records[i]).Error; err != nil {
				return fmt.Errorf("failed to create credential request %d: %w", i, err)
			}
		}
		return nil
	})
	return records, reused, err
}

func (r *CredentialRequestRepository) FindPending(ctx context.Context, statusID uuid.UUID, limit int) ([]models.CredentialRequest, error) {
	var results []models.CredentialRequest
	err := r.db.WithContext(ctx).
		Where("verification_status = ?", statusID).
		Order("created_at ASC").
		Limit(limit).
		Find(&results).Error
	return results, err
}

func (r *CredentialRequestRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.CredentialRequest, error) {
	var req models.CredentialRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&req).Error
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *CredentialRequestRepository) FindByRequestID(ctx context.Context, requestID uuid.UUID) ([]models.CredentialRequest, error) {
	var results []models.CredentialRequest
	err := r.db.WithContext(ctx).Where("request_id = ?", requestID).Find(&results).Error
	return results, err
}

func (r *CredentialRequestRepository) UpdateStatus(ctx context.Context, id uuid.UUID, statusID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"updated_at":          time.Now(),
		}).Error
}

func (r *CredentialRequestRepository) RecordFailure(ctx context.Context, id uuid.UUID, statusID uuid.UUID, verifierID *uuid.UUID, verificationErrors json.RawMessage) error {
	return r.db.WithContext(ctx).Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"verifier":            verifierID,
			"verification_errors": verificationErrors,
			"updated_at":          time.Now(),
		}).Error
}

func (r *CredentialRequestRepository) RecordRetry(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		UpdateColumn("retry_count", gorm.Expr("retry_count + 1")).
		UpdateColumn("updated_at", time.Now()).Error
}

// SubscriberLogFilter selects one subscriber's /verify rows for the read API.
type SubscriberLogFilter struct {
	SubscriberID string
	From         time.Time
	To           time.Time
	Limit        int
	Offset       int
}

// subscriberLogQuery is shared by the list and the count so the two always agree.
func (r *CredentialRequestRepository) subscriberLogQuery(ctx context.Context, f SubscriberLogFilter) *gorm.DB {
	return r.db.WithContext(ctx).Model(&models.CredentialRequest{}).
		Where("subscriber_id = ?", f.SubscriberID).
		// /verify rows only: a /credential submission always fills both.
		Where("participant_id = '' AND payload_hash = ''").
		Where("created_at >= ? AND created_at < ?", f.From, f.To)
}

// ListBySubscriber returns one page, newest first; the id tiebreak keeps rows sharing
// a timestamp from swapping places between pages.
func (r *CredentialRequestRepository) ListBySubscriber(ctx context.Context, f SubscriberLogFilter) ([]models.CredentialRequest, error) {
	var results []models.CredentialRequest
	err := r.subscriberLogQuery(ctx, f).
		Order("created_at DESC, id DESC").
		Limit(f.Limit).
		Offset(f.Offset).
		Find(&results).Error
	return results, err
}

// CountBySubscriber counts every row in the window, ignoring Limit/Offset.
func (r *CredentialRequestRepository) CountBySubscriber(ctx context.Context, f SubscriberLogFilter) (int64, error) {
	var total int64
	err := r.subscriberLogQuery(ctx, f).Count(&total).Error
	return total, err
}

func (r *CredentialRequestRepository) UpdateVerificationResult(ctx context.Context, id uuid.UUID, statusID uuid.UUID, verifierID *uuid.UUID, evidences json.RawMessage) error {
	return r.db.WithContext(ctx).Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"verifier":            verifierID,
			"evidences":           evidences,
			"updated_at":          time.Now(),
		}).Error
}
