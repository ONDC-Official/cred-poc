package credential_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"credential-service/internal/credential"
	"credential-service/internal/telemetry"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Global providers can only usefully be installed once per test binary, and other tests in
// this package run in parallel and emit spans too, so every assertion below filters on the
// trace or on the credential type these tests own.
var (
	telemetryOnce  sync.Once
	spanRecorder   *tracetest.SpanRecorder
	tracerProvider *sdktrace.TracerProvider
	metricReader   *sdkmetric.ManualReader
)

func installTestTelemetry() {
	telemetryOnce.Do(func() {
		spanRecorder = tracetest.NewSpanRecorder()
		tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
		otel.SetTracerProvider(tracerProvider)
		metricReader = sdkmetric.NewManualReader()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader)))
	})
}

const secretCredID = "ABCDE1234F"

func TestProcessTracesEachProviderAttempt(t *testing.T) {
	installTestTelemetry()

	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "OTEL_CHAIN",
		Providers: []credential.ProviderInvoker{
			{Name: "http-fails", Invoke: func(context.Context, any) ([]byte, int, error) {
				return []byte(`{"error":"down"}`), http.StatusBadGateway, nil
			}},
			{Name: "transport-fails", Invoke: func(context.Context, any) ([]byte, int, error) {
				return nil, 0, errors.New("dial tcp: connection refused")
			}},
			{Name: "succeeds", Invoke: func(context.Context, any) ([]byte, int, error) {
				return []byte(`{"ok":true}`), http.StatusOK, nil
			}},
		},
		ValidateFn: func(json.RawMessage) error { return nil },
		BuildRequestFn: func(data json.RawMessage) (any, error) {
			return map[string]string{"raw": string(data)}, nil
		},
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) {
			return &credential.VerificationResult{Success: true, CredID: secretCredID}, nil
		},
	})

	ctx, parent := tracerProvider.Tracer("test").Start(context.Background(), "test-parent")
	result, err := verifier.Process(ctx, json.RawMessage(`{"id_no":"`+secretCredID+`"}`))
	parent.End()
	if err != nil || !result.Success || result.Provider != "succeeds" {
		t.Fatalf("unexpected result %+v, err %v", result, err)
	}

	spans := spansInTrace(parent.SpanContext().TraceID().String())

	verify := findSpans(spans, "credential.verify")
	if len(verify) != 1 {
		t.Fatalf("want 1 credential.verify span, got %d", len(verify))
	}
	assertAttr(t, verify[0], telemetry.AttrCredType, "OTEL_CHAIN")
	assertAttr(t, verify[0], telemetry.AttrProvider, "succeeds")
	assertAttr(t, verify[0], telemetry.AttrOutcome, telemetry.OutcomeSuccess)
	if verify[0].Status().Code == codes.Error {
		t.Fatal("successful verification span should not be marked as an error")
	}

	invokes := findSpans(spans, "provider.invoke")
	if len(invokes) != 3 {
		t.Fatalf("want 3 provider.invoke spans, got %d", len(invokes))
	}
	wantOutcome := map[string]string{
		"http-fails":      telemetry.OutcomeHTTPError,
		"transport-fails": telemetry.OutcomeTransportError,
		"succeeds":        telemetry.OutcomeSuccess,
	}
	for _, s := range invokes {
		if s.Parent().SpanID() != verify[0].SpanContext().SpanID() {
			t.Fatalf("provider.invoke span %q is not a child of credential.verify", s.Name())
		}
		provider := attrValue(s, telemetry.AttrProvider)
		assertAttr(t, s, telemetry.AttrOutcome, wantOutcome[provider])
		if (provider == "succeeds") == (s.Status().Code == codes.Error) {
			t.Fatalf("provider %q has status %v", provider, s.Status().Code)
		}
	}

	assertNoAttributeContains(t, spans, secretCredID)

	calls := counterByAttrs(t, "credential.provider.calls", "OTEL_CHAIN")
	for provider, outcome := range wantOutcome {
		if calls[provider+"|"+outcome] != 1 {
			t.Fatalf("credential.provider.calls{%s,%s} = %d, want 1 (all: %v)", provider, outcome, calls[provider+"|"+outcome], calls)
		}
	}
	if got := counterByAttrs(t, "credential.verifications", "OTEL_CHAIN")["succeeds|success"]; got != 1 {
		t.Fatalf("credential.verifications{succeeds,success} = %d, want 1", got)
	}
}

func TestProcessRecordsValidationFailureWithoutProviderSpans(t *testing.T) {
	installTestTelemetry()

	verifier := credential.NewConfigVerifier(credential.ConfigVerifierConfig{
		CredType: "OTEL_INVALID",
		Providers: []credential.ProviderInvoker{
			{Name: "never", Invoke: func(context.Context, any) ([]byte, int, error) {
				t.Error("provider must not be called when validation fails")
				return nil, 0, nil
			}},
		},
		ValidateFn:      func(json.RawMessage) error { return errors.New("id_no is required") },
		BuildRequestFn:  func(json.RawMessage) (any, error) { return nil, nil },
		ParseResponseFn: func([]byte) (*credential.VerificationResult, error) { return nil, nil },
	})

	ctx, parent := tracerProvider.Tracer("test").Start(context.Background(), "test-parent")
	if _, err := verifier.Process(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parent.End()

	spans := spansInTrace(parent.SpanContext().TraceID().String())
	if n := len(findSpans(spans, "provider.invoke")); n != 0 {
		t.Fatalf("want no provider.invoke spans, got %d", n)
	}
	verify := findSpans(spans, "credential.verify")
	if len(verify) != 1 {
		t.Fatalf("want 1 credential.verify span, got %d", len(verify))
	}
	assertAttr(t, verify[0], telemetry.AttrOutcome, telemetry.OutcomeValidationFailed)
	if got := counterByAttrs(t, "credential.verifications", "OTEL_INVALID")["|validation_failed"]; got != 1 {
		t.Fatalf("credential.verifications{validation_failed} = %d, want 1", got)
	}
}

func spansInTrace(traceID string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range spanRecorder.Ended() {
		if s.SpanContext().TraceID().String() == traceID {
			out = append(out, s)
		}
	}
	return out
}

func findSpans(spans []sdktrace.ReadOnlySpan, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

func attrValue(s sdktrace.ReadOnlySpan, key attribute.Key) string {
	for _, kv := range s.Attributes() {
		if kv.Key == key {
			return kv.Value.String()
		}
	}
	return ""
}

func assertAttr(t *testing.T, s sdktrace.ReadOnlySpan, key attribute.Key, want string) {
	t.Helper()
	if got := attrValue(s, key); got != want {
		t.Fatalf("span %q attribute %s = %q, want %q", s.Name(), key, got, want)
	}
}

func assertNoAttributeContains(t *testing.T, spans []sdktrace.ReadOnlySpan, secret string) {
	t.Helper()
	for _, s := range spans {
		kvs := append([]attribute.KeyValue{}, s.Attributes()...)
		for _, e := range s.Events() {
			kvs = append(kvs, e.Attributes...)
		}
		for _, kv := range kvs {
			if strings.Contains(kv.Value.String(), secret) {
				t.Fatalf("span %q attribute %s leaks the credential id: %q", s.Name(), kv.Key, kv.Value.String())
			}
		}
	}
}

// counterByAttrs sums an Int64 counter's data points for one credential type, keyed by
// "provider|outcome".
func counterByAttrs(t *testing.T, name, credType string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s is %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				if v, _ := dp.Attributes.Value(telemetry.AttrCredType); v.AsString() != credType {
					continue
				}
				provider, _ := dp.Attributes.Value(telemetry.AttrProvider)
				outcome, _ := dp.Attributes.Value(telemetry.AttrOutcome)
				out[provider.AsString()+"|"+outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}
