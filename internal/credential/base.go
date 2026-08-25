package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"credential-service/internal/service/client"
)

// DigioVerifier owns the shared processing pipeline.
// Concrete credential types (PAN, GST) compose this and provide type-specific behavior.
type DigioVerifier struct {
	client   *client.DigioClient
	endpoint string
	credType string

	validateFn      func(json.RawMessage) error
	buildRequestFn  func(json.RawMessage) (any, error)
	parseResponseFn func([]byte) (*VerificationResult, error)
}

type DigioVerifierConfig struct {
	Client   *client.DigioClient
	Endpoint string
	CredType string

	ValidateFn      func(json.RawMessage) error
	BuildRequestFn  func(json.RawMessage) (any, error)
	ParseResponseFn func([]byte) (*VerificationResult, error)
}

func NewDigioVerifier(cfg DigioVerifierConfig) *DigioVerifier {
	return &DigioVerifier{
		client:          cfg.Client,
		endpoint:        cfg.Endpoint,
		credType:        cfg.CredType,
		validateFn:      cfg.ValidateFn,
		buildRequestFn:  cfg.BuildRequestFn,
		parseResponseFn: cfg.ParseResponseFn,
	}
}

func (b *DigioVerifier) CredType() string {
	return b.credType
}

func (b *DigioVerifier) ValidateCredData(data json.RawMessage) error {
	return b.validateFn(data)
}

func (b *DigioVerifier) BuildDigioRequest(data json.RawMessage) (any, error) {
	return b.buildRequestFn(data)
}

func (b *DigioVerifier) ParseDigioResponse(body []byte) (*VerificationResult, error) {
	return b.parseResponseFn(body)
}

func (b *DigioVerifier) Process(ctx context.Context, data json.RawMessage) (*VerificationResult, error) {
	if err := b.validateFn(data); err != nil {
		return &VerificationResult{Success: false, Error: err.Error()}, nil
	}

	payload, err := b.buildRequestFn(data)
	if err != nil {
		return nil, fmt.Errorf("build digio request: %w", err)
	}

	body, statusCode, err := b.client.Post(ctx, b.endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("digio call failed: %w", err)
	}

	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal digio request: %w", err)
	}
	evidences := []Evidence{
		{Type: "request", Data: json.RawMessage(requestJSON)},
		{Type: "response", Data: json.RawMessage(body)},
	}

	if statusCode >= http.StatusBadRequest {
		return &VerificationResult{
			Success:   false,
			Error:     fmt.Sprintf("digio returned status %d: %s", statusCode, string(body)),
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
