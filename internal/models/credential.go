package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Credential struct {
	ID             uuid.UUID        `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	CredID         string           `gorm:"type:text;not null;index"`
	CredType       uuid.UUID        `gorm:"type:uuid;not null;index"`
	VerifiedData   json.RawMessage  `gorm:"column:cred_data;type:jsonb;not null"`
	Evidences      *json.RawMessage `gorm:"type:jsonb"`
	Issuer         *uuid.UUID       `gorm:"type:uuid"`
	Verifier       *uuid.UUID       `gorm:"type:uuid"`
	ValidFrom      *time.Time       `gorm:"type:timestamptz"`
	ValidUntil     *time.Time       `gorm:"type:timestamptz"`
	LastVerifiedAt *time.Time       `gorm:"type:timestamptz"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (Credential) TableName() string { return "credentials" }
