package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ConfigVerifier owns the shared processing pipeline for any provider.
// Credential-type YAML supplies validation/mapping; the provider gateway
// resolves name+capability to the vendor HTTP call (Digio today).
type ConfigVerifier struct {
	credType string
	invoke   func(ctx context.Context, payload any) ([]byte, int, error)

	validateFn      func(json.RawMessage) error
	buildRequestFn  func(json.RawMessage) (any, error)
	parseResponseFn func([]byte) (*VerificationResult, error)
}

type ConfigVerifierConfig struct {
	CredType string
	Invoke   func(ctx context.Context, payload any) ([]byte, int, error)

	ValidateFn      func(json.RawMessage) error
	BuildRequestFn  func(json.RawMessage) (any, error)
	ParseResponseFn func([]byte) (*VerificationResult, error)
}

func NewConfigVerifier(cfg ConfigVerifierConfig) *ConfigVerifier {
	return &ConfigVerifier{
		credType:        cfg.CredType,
		invoke:          cfg.Invoke,
		validateFn:      cfg.ValidateFn,
		buildRequestFn:  cfg.BuildRequestFn,
		parseResponseFn: cfg.ParseResponseFn,
	}
}

func (b *ConfigVerifier) CredType() string {
	return b.credType
}

func (b *ConfigVerifier) ValidateCredData(data json.RawMessage) error {
	return b.validateFn(data)
}

func (b *ConfigVerifier) BuildProviderRequest(data json.RawMessage) (any, error) {
	return b.buildRequestFn(data)
}

func (b *ConfigVerifier) ParseProviderResponse(body []byte) (*VerificationResult, error) {
	return b.parseResponseFn(body)
}

func (b *ConfigVerifier) Process(ctx context.Context, data json.RawMessage) (*VerificationResult, error) {
	if err := b.validateFn(data); err != nil {
		return &VerificationResult{Success: false, Error: err.Error()}, nil
	}

	payload, err := b.buildRequestFn(data)
	if err != nil {
		return nil, fmt.Errorf("build provider request: %w", err)
	}

	body, statusCode, err := b.invoke(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("provider call failed: %w", err)
	}

	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}
	evidences := []Evidence{
		{Type: "request", Data: json.RawMessage(requestJSON)},
		{Type: "response", Data: json.RawMessage(body)},
	}

	if statusCode >= http.StatusBadRequest {
		return &VerificationResult{
			Success:   false,
			Error:     fmt.Sprintf("provider returned status %d: %s", statusCode, string(body)),
			Evidences: evidences,
		}, nil
	}

	result, err := b.parseResponseFn(body)
	if err != nil {
		return nil, err
	}
	result.Evidences = evidences
	return result, nil
}
