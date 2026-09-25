package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CredentialRequest struct {
	ID                 uuid.UUID        `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	RequestID          uuid.UUID        `gorm:"type:uuid;not null;index"`
	ParticipantID      string           `gorm:"type:text;not null"`
	PayloadHash        string           `gorm:"type:text;not null"`
	// SubscriberID is the ONDC caller from the Authorization keyId; '' on /credential rows.
	SubscriberID string `gorm:"type:text;not null;default:''"`
	// Verbatim HTTP bodies of a /verify call, nil on /credential rows. Text,
	// not jsonb: jsonb reorders keys and reformats spacing.
	RequestBody  *string `gorm:"type:text"`
	ResponseBody *string `gorm:"type:text"`
	CredType           uuid.UUID        `gorm:"type:uuid;not null"`
	VerificationStatus uuid.UUID        `gorm:"type:uuid;not null"`
	RetryCount         int              `gorm:"not null;default:0"`
	MaxRetries         int              `gorm:"not null;default:3"`
	CredData           json.RawMessage  `gorm:"type:jsonb;not null"`
	Evidences          *json.RawMessage `gorm:"type:jsonb"`
	VerificationErrors *json.RawMessage `gorm:"type:jsonb"`
	Issuer             *uuid.UUID       `gorm:"type:uuid"`
	Verifier           *uuid.UUID       `gorm:"type:uuid"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (CredentialRequest) TableName() string { return "credential_requests" }

type Evidence struct {
	Data   any    `json:"data"`
	Digest string `json:"digest"`
	Type   string `json:"type"`
}
