package repository

import (
	"context"
	"credential-service/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CredentialRepository struct {
	db *gorm.DB
}

func NewCredentialRepository(db *gorm.DB) *CredentialRepository {
	return &CredentialRepository{db: db}
}

func (r *CredentialRepository) Create(ctx context.Context, cred *models.Credential) error {
	return r.db.WithContext(ctx).Create(cred).Error
}

func (r *CredentialRepository) FindByCredentialID(ctx context.Context, credID string) ([]models.Credential, error) {
	var results []models.Credential
	err := r.db.WithContext(ctx).Where("cred_id = ?", credID).Find(&results).Error
	return results, err
}

func (r *CredentialRepository) FindByCredentialType(ctx context.Context, credTypeID uuid.UUID) ([]models.Credential, error) {
	var results []models.Credential
	err := r.db.WithContext(ctx).Where("cred_type = ?", credTypeID).Find(&results).Error
	return results, err
}

func (r *CredentialRepository) Update(ctx context.Context, cred *models.Credential) error {
	return r.db.WithContext(ctx).Save(cred).Error
}
