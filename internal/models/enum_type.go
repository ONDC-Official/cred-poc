package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EnumType struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Category    string    `gorm:"type:text;not null"`
	Value       string    `gorm:"type:text;not null"`
	Label       string    `gorm:"type:text;not null"`
	JSONSchema  *string   `gorm:"type:jsonb"`
	Description string    `gorm:"type:text;not null;default:''"`
	Active      bool      `gorm:"type:boolean;not null;default:true"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (EnumType) TableName() string { return "enum_types" }

const (
	CategoryCredType         = "CRED_TYPE"
	CategoryCredVerification = "CRED_VERIFICATION"
	CategoryCredVerifier     = "CRED_VERIFIER"
	CategoryCredIssuer       = "CRED_ISSUER"

	CredTypePAN   = "PAN"
	CredTypeGST   = "GST"
	CredTypeFSSAI = "FSSAI"
	CredTypeUdyam = "UDYAM"

	VerificationPending  = "PENDING"
	VerificationVerified = "VERIFIED"
	VerificationFailed   = "FAILED"
	VerificationRejected = "REJECTED"

	VerifierDigio = "DIGIO"

	IssuerIncomeTaxDept = "INCOME_TAX_DEPT"
	IssuerGSTN          = "GSTN"
	IssuerFSSAI         = "FSSAI_AUTHORITY"
	IssuerMSME          = "MSME"
)

// IssuerForCredType maps a credential type to its real-world issuing
// authority (per Credential Service Implementation Plan.md: "Issuer of the
// credential (PAN: Income Tax Department)"). Digio never tells us this —
// the doc's own TBD section flags it as a value we must assign ourselves.
// Returns "" for any credential type without a known issuer.
func IssuerForCredType(credType string) string {
	switch credType {
	case CredTypePAN:
		return IssuerIncomeTaxDept
	case CredTypeGST:
		return IssuerGSTN
	case CredTypeFSSAI:
		return IssuerFSSAI
	case CredTypeUdyam:
		return IssuerMSME
	default:
		return ""
	}
}

type EnumCache struct {
	byID            map[uuid.UUID]*EnumType
	byCategoryValue map[string]uuid.UUID
}

// NewEnumCache builds a cache from already-loaded enum rows. Pure (no DB
// access) so it can be used directly in tests.
func NewEnumCache(entries []EnumType) *EnumCache {
	c := &EnumCache{
		byID:            make(map[uuid.UUID]*EnumType, len(entries)),
		byCategoryValue: make(map[string]uuid.UUID, len(entries)),
	}

	for i := range entries {
		e := &entries[i]
		c.byID[e.ID] = e
		c.byCategoryValue[e.Category+":"+e.Value] = e.ID
	}

	return c
}

// LoadEnumCache loads all enum_types rows from the database and builds a cache.
func LoadEnumCache(db *gorm.DB) (*EnumCache, error) {
	var enums []EnumType
	if err := db.Find(&enums).Error; err != nil {
		return nil, err
	}
	return NewEnumCache(enums), nil
}

func (c *EnumCache) ID(category, value string) uuid.UUID {
	return c.byCategoryValue[category+":"+value]
}

func (c *EnumCache) Get(id uuid.UUID) *EnumType {
	return c.byID[id]
}

func (c *EnumCache) CredTypeID(value string) uuid.UUID {
	return c.ID(CategoryCredType, value)
}

func (c *EnumCache) VerificationStatusID(value string) uuid.UUID {
	return c.ID(CategoryCredVerification, value)
}

func (c *EnumCache) VerifierID(value string) uuid.UUID {
	return c.ID(CategoryCredVerifier, value)
}

func (c *EnumCache) IssuerID(value string) uuid.UUID {
	return c.ID(CategoryCredIssuer, value)
}
