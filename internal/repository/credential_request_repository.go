package repository

import (
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

func (r *CredentialRequestRepository) Create(req *models.CredentialRequest) error {
	return r.db.Create(req).Error
}

func (r *CredentialRequestRepository) CreateMany(requests []models.CredentialRequest) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i := range requests {
			if err := tx.Create(&requests[i]).Error; err != nil {
				return fmt.Errorf("failed to create credential request %d: %w", i, err)
			}
		}
		return nil
	})
}

func (r *CredentialRequestRepository) FindPending(statusID uuid.UUID, limit int) ([]models.CredentialRequest, error) {
	var results []models.CredentialRequest
	err := r.db.
		Where("verification_status = ?", statusID).
		Order("created_at ASC").
		Limit(limit).
		Find(&results).Error
	return results, err
}

func (r *CredentialRequestRepository) FindByID(id uuid.UUID) (*models.CredentialRequest, error) {
	var req models.CredentialRequest
	err := r.db.Where("id = ?", id).First(&req).Error
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *CredentialRequestRepository) FindByRequestID(requestID uuid.UUID) ([]models.CredentialRequest, error) {
	var results []models.CredentialRequest
	err := r.db.Where("request_id = ?", requestID).Find(&results).Error
	return results, err
}

func (r *CredentialRequestRepository) UpdateStatus(id uuid.UUID, statusID uuid.UUID) error {
	return r.db.Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"updated_at":          time.Now(),
		}).Error
}

func (r *CredentialRequestRepository) RecordFailure(id uuid.UUID, statusID uuid.UUID, verificationErrors json.RawMessage) error {
	return r.db.Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"verification_errors": verificationErrors,
			"updated_at":          time.Now(),
		}).Error
}

func (r *CredentialRequestRepository) RecordRetry(id uuid.UUID) error {
	return r.db.Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		UpdateColumn("retry_count", gorm.Expr("retry_count + 1")).
		UpdateColumn("updated_at", time.Now()).Error
}

func (r *CredentialRequestRepository) UpdateVerificationResult(id uuid.UUID, statusID uuid.UUID, evidences json.RawMessage) error {
	return r.db.Model(&models.CredentialRequest{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"verification_status": statusID,
			"evidences":           evidences,
			"updated_at":          time.Now(),
		}).Error
}
