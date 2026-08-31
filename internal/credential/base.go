package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ProviderInvoker is one named, callable provider in a credential type's
// fallback chain — Name is used for VerificationResult.Provider and, at the
// DB layer, to resolve which CRED_VERIFIER enum row a result belongs to.
type ProviderInvoker struct {
	Name   string
	Invoke func(ctx context.Context, payload any) ([]byte, int, error)
}

// ConfigVerifier owns the shared processing pipeline for any provider.
// Credential-type YAML supplies validation/mapping; the provider gateway
// resolves name+capability to the vendor HTTP call. Providers are tried in
// order; a transport error or a failed result advances to the next one.
type ConfigVerifier struct {
	credType  string
	providers []ProviderInvoker

	validateFn      func(json.RawMessage) error
	buildRequestFn  func(json.RawMessage) (any, error)
	parseResponseFn func([]byte) (*VerificationResult, error)
}

type ConfigVerifierConfig struct {
	CredType  string
	Providers []ProviderInvoker

	ValidateFn      func(json.RawMessage) error
	BuildRequestFn  func(json.RawMessage) (any, error)
	ParseResponseFn func([]byte) (*VerificationResult, error)
}

func NewConfigVerifier(cfg ConfigVerifierConfig) *ConfigVerifier {
	return &ConfigVerifier{
		credType:        cfg.CredType,
		providers:       cfg.Providers,
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

// Process validates data, builds one provider request from it, then tries
// each configured provider in order. A transport error or a failed result
// (status >= 400, or a parsed soft-fail) advances to the next provider; the
// first success returns immediately. If every provider fails, the last
// attempt's failure is returned as a normal VerificationResult (never a Go
// error) so the async retry-by-retry_count mechanism can retry the whole
// chain again. A response-parsing error is not fallback-eligible — it
// surfaces immediately as a Go error, same as before multi-provider support.
func (b *ConfigVerifier) Process(ctx context.Context, data json.RawMessage) (*VerificationResult, error) {
	if err := b.validateFn(data); err != nil {
		return &VerificationResult{Success: false, Error: err.Error()}, nil
	}

	payload, err := b.buildRequestFn(data)
	if err != nil {
		return nil, fmt.Errorf("build provider request: %w", err)
	}

	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	var lastResult *VerificationResult
	for _, p := range b.providers {
		body, statusCode, err := p.Invoke(ctx, payload)
		if err != nil {
			lastResult = &VerificationResult{
				Success:  false,
				Error:    fmt.Sprintf("provider call failed: %s", err.Error()),
				Provider: p.Name,
			}
			continue
		}

		evidences := []Evidence{
			{Type: "request", Data: json.RawMessage(requestJSON)},
			{Type: "response", Data: json.RawMessage(body)},
		}

		if statusCode >= http.StatusBadRequest {
			lastResult = &VerificationResult{
				Success:   false,
				Error:     fmt.Sprintf("provider returned status %d: %s", statusCode, string(body)),
				Evidences: evidences,
				Provider:  p.Name,
			}
			continue
		}

		result, err := b.parseResponseFn(body)
		if err != nil {
			return nil, err
		}
		result.Evidences = evidences
		result.Provider = p.Name

		if !result.Success {
			lastResult = result
			continue
		}
		return result, nil
	}

	return lastResult, nil
}
