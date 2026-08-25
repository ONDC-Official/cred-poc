package dto

import "encoding/json"

type SubmitCredentialsRequest struct {
	// ParticipantID is the onboarded NP id from the registry service DB.
	// Registry calls credential-service on behalf of this participant; NPs never call directly.
	ParticipantID string           `json:"participant_id" validate:"required"`
	Credentials   []CredentialItem `json:"credentials" validate:"required,min=1,dive"`
}

// CredentialItem matches the flat request shape from the design doc's
// Credential API ("Request Parameters" tables): cred_type, cred_id, plus
// name/dob for types that need them (PAN).
type CredentialItem struct {
	CredType string `json:"cred_type" validate:"required"`
	CredID   string `json:"cred_id" validate:"required"`
	Name     string `json:"name" validate:"required_if=CredType PAN"`
	Dob      string `json:"dob" validate:"required_if=CredType PAN"`
}

type SubmitCredentialsResponse struct {
	RequestID   string                   `json:"request_id"`
	Credentials []CredentialItemResponse `json:"credentials"`
}

type CredentialItemResponse struct {
	ID       string `json:"id"`
	CredType string `json:"cred_type"`
	CredID   string `json:"cred_id"`
	Status   string `json:"status"`
}

type GetCredentialResultsResponse struct {
	RequestID   string                 `json:"request_id"`
	Credentials []CredentialResultItem `json:"credentials"`
}

type CredentialResultItem struct {
	ID                 string          `json:"id"`
	CredType           string          `json:"cred_type"`
	CredID             string          `json:"cred_id"`
	Status             string          `json:"status"`
	DigioResponse      json.RawMessage `json:"digio_response,omitempty"`
	VerificationErrors json.RawMessage `json:"verification_errors,omitempty"`
}
