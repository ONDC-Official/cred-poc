package repository

import (
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

func (r *CredentialRepository) Create(cred *models.Credential) error {
	return r.db.Create(cred).Error
}

func (r *CredentialRepository) FindByCredentialID(credID string) ([]models.Credential, error) {
	var results []models.Credential
	err := r.db.Where("cred_id = ?", credID).Find(&results).Error
	return results, err
}

func (r *CredentialRepository) FindByCredentialType(credTypeID uuid.UUID) ([]models.Credential, error) {
	var results []models.Credential
	err := r.db.Where("cred_type = ?", credTypeID).Find(&results).Error
	return results, err
}

func (r *CredentialRepository) Update(cred *models.Credential) error {
	return r.db.Save(cred).Error
}
