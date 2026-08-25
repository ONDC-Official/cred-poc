package credential

import (
	"context"
	"encoding/json"
)

// Verifier defines the contract for async credential processing.
// Each credential type (PAN, GST, etc.) implements the type-specific methods,
// while DigioVerifier owns the shared pipeline.
type Verifier interface {
	CredType() string
	ValidateCredData(data json.RawMessage) error
	BuildDigioRequest(data json.RawMessage) (any, error)
	ParseDigioResponse(body []byte) (*VerificationResult, error)
	Process(ctx context.Context, data json.RawMessage) (*VerificationResult, error)
}

type VerificationResult struct {
	Success      bool
	CredID       string
	VerifiedData map[string]any
	Evidences    []Evidence
	Error        string
}

type Evidence struct {
	Data   any    `json:"data"`
	Digest string `json:"digest"`
	Type   string `json:"type"`
}
