package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"credential-service/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
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
	ctx, span := telemetry.Tracer().Start(ctx, "credential.verify",
		trace.WithAttributes(telemetry.AttrCredType.String(b.credType)))
	defer span.End()

	if err := b.validateFn(data); err != nil {
		b.finish(ctx, span, "", telemetry.OutcomeValidationFailed)
		return &VerificationResult{Success: false, Error: err.Error()}, nil
	}

	payload, err := b.buildRequestFn(data)
	if err != nil {
		b.fail(ctx, span, "", telemetry.OutcomeBuildError, err)
		return nil, fmt.Errorf("build provider request: %w", err)
	}

	requestJSON, err := json.Marshal(payload)
	if err != nil {
		b.fail(ctx, span, "", telemetry.OutcomeBuildError, err)
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	var lastResult *VerificationResult
	lastOutcome := ""
	for _, p := range b.providers {
		callCtx, callSpan := telemetry.Tracer().Start(ctx, "provider.invoke",
			trace.WithAttributes(telemetry.AttrCredType.String(b.credType), telemetry.AttrProvider.String(p.Name)))
		start := time.Now()
		body, statusCode, err := p.Invoke(callCtx, payload)
		elapsed := time.Since(start)
		if err != nil {
			endProviderCall(callCtx, callSpan, b.credType, p.Name, telemetry.OutcomeTransportError, elapsed, err)
			lastResult = &VerificationResult{
				Success:  false,
				Error:    fmt.Sprintf("provider call failed: %s", err.Error()),
				Provider: p.Name,
			}
			lastOutcome = telemetry.OutcomeTransportError
			continue
		}
		callSpan.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))

		evidences := []Evidence{
			{Type: "request", Data: json.RawMessage(requestJSON)},
			{Type: "response", Data: json.RawMessage(body)},
		}

		if statusCode >= http.StatusBadRequest {
			endProviderCall(callCtx, callSpan, b.credType, p.Name, telemetry.OutcomeHTTPError, elapsed, nil)
			lastResult = &VerificationResult{
				Success:   false,
				Error:     fmt.Sprintf("provider returned status %d: %s", statusCode, string(body)),
				Evidences: evidences,
				Provider:  p.Name,
			}
			lastOutcome = telemetry.OutcomeHTTPError
			continue
		}

		result, err := b.parseResponseFn(body)
		if err != nil {
			endProviderCall(callCtx, callSpan, b.credType, p.Name, telemetry.OutcomeParseError, elapsed, err)
			b.fail(ctx, span, p.Name, telemetry.OutcomeParseError, err)
			return nil, err
		}
		result.Evidences = evidences
		result.Provider = p.Name

		if !result.Success {
			endProviderCall(callCtx, callSpan, b.credType, p.Name, telemetry.OutcomeSoftFailure, elapsed, nil)
			lastResult = result
			lastOutcome = telemetry.OutcomeSoftFailure
			continue
		}
		endProviderCall(callCtx, callSpan, b.credType, p.Name, telemetry.OutcomeSuccess, elapsed, nil)
		b.finish(ctx, span, p.Name, telemetry.OutcomeSuccess)
		return result, nil
	}

	provider := ""
	if lastResult != nil {
		provider = lastResult.Provider
	}
	b.finish(ctx, span, provider, lastOutcome)
	return lastResult, nil
}

// finish records a verification that produced a result. Anything but success marks the
// span as an error so failed chains stand out in a trace view.
func (b *ConfigVerifier) finish(ctx context.Context, span trace.Span, provider, outcome string) {
	span.SetAttributes(telemetry.AttrProvider.String(provider), telemetry.AttrOutcome.String(outcome))
	if outcome != telemetry.OutcomeSuccess {
		span.SetStatus(codes.Error, outcome)
	}
	telemetry.RecordVerification(ctx, b.credType, provider, outcome)
}

// fail records a verification that ended in a Go error rather than a result.
func (b *ConfigVerifier) fail(ctx context.Context, span trace.Span, provider, outcome string, err error) {
	span.RecordError(err)
	b.finish(ctx, span, provider, outcome)
}

// endProviderCall closes one provider attempt's span and records its metrics. err is
// recorded on the span only; provider response bodies are never put on telemetry.
func endProviderCall(ctx context.Context, span trace.Span, credType, provider, outcome string, elapsed time.Duration, err error) {
	span.SetAttributes(telemetry.AttrOutcome.String(outcome))
	if outcome != telemetry.OutcomeSuccess {
		if err != nil {
			span.RecordError(err)
		}
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
	telemetry.RecordProviderCall(ctx, credType, provider, outcome, elapsed)
}
