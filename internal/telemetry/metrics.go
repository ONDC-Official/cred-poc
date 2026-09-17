package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Attribute keys shared by the service's spans and metrics. None may carry PII.
const (
	AttrCredType        = attribute.Key("credential.type")
	AttrProvider        = attribute.Key("credential.provider")
	AttrOutcome         = attribute.Key("credential.outcome")
	AttrRequestID       = attribute.Key("credential_request.id")
	AttrWorkerResult    = attribute.Key("credential.worker.result")
	AttrSubmissionReuse = attribute.Key("credential.submission.reused")
	AttrAuthResult      = attribute.Key("auth.result")
)

// Verification and provider-call outcomes, the values of AttrOutcome.
const (
	OutcomeSuccess          = "success"
	OutcomeSoftFailure      = "soft_failure"
	OutcomeHTTPError        = "http_error"
	OutcomeTransportError   = "transport_error"
	OutcomeParseError       = "parse_error"
	OutcomeValidationFailed = "validation_failed"
	OutcomeBuildError       = "build_error"
)

// Worker results, the values of AttrWorkerResult.
const (
	WorkerVerified = "verified"
	WorkerRetry    = "retry"
	WorkerFailed   = "failed"
	WorkerError    = "error"
)

// Tracer returns the service tracer. The global delegate means it is safe to call before
// Setup; spans start flowing once a provider is installed.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}

type instruments struct {
	verifications    metric.Int64Counter
	providerCalls    metric.Int64Counter
	providerDuration metric.Float64Histogram
	submissions      metric.Int64Counter
	workerProcessed  metric.Int64Counter
}

var (
	instrumentsOnce sync.Once
	inst            instruments
)

// meters lazily creates the instruments from the global MeterProvider. Instruments made
// before Setup installs a real provider are forwarded to it by the global delegate.
func meters() *instruments {
	instrumentsOnce.Do(func() {
		m := otel.Meter(ScopeName)
		inst.verifications = must(m.Int64Counter("credential.verifications",
			metric.WithUnit("{verification}"),
			metric.WithDescription("Credential verifications completed, by final outcome of the provider chain")))
		inst.providerCalls = must(m.Int64Counter("credential.provider.calls",
			metric.WithUnit("{call}"),
			metric.WithDescription("Calls to a KYC provider, by outcome")))
		inst.providerDuration = must(m.Float64Histogram("credential.provider.duration",
			metric.WithUnit("s"),
			metric.WithDescription("Duration of one KYC provider call")))
		inst.submissions = must(m.Int64Counter("credential.submissions",
			metric.WithUnit("{submission}"),
			metric.WithDescription("POST /credential batches accepted, and whether an in-flight duplicate was reused")))
		inst.workerProcessed = must(m.Int64Counter("credential.worker.processed",
			metric.WithUnit("{request}"),
			metric.WithDescription("Credential requests processed by the async worker, by result")))
	})
	return &inst
}

// must reports an instrument-creation error to the OTel error handler and keeps the
// returned instrument, which the API guarantees is a usable no-op on error.
func must[T any](instrument T, err error) T {
	if err != nil {
		otel.Handle(err)
	}
	return instrument
}

// RecordProviderCall records one attempt against one provider in the fallback chain.
func RecordProviderCall(ctx context.Context, credType, provider, outcome string, elapsed time.Duration) {
	attrs := metric.WithAttributes(AttrCredType.String(credType), AttrProvider.String(provider), AttrOutcome.String(outcome))
	m := meters()
	m.providerCalls.Add(ctx, 1, attrs)
	m.providerDuration.Record(ctx, elapsed.Seconds(), attrs)
}

// RecordVerification records the final outcome of one credential verification. provider is
// empty when no provider was reached.
func RecordVerification(ctx context.Context, credType, provider, outcome string) {
	meters().verifications.Add(ctx, 1, metric.WithAttributes(
		AttrCredType.String(credType), AttrProvider.String(provider), AttrOutcome.String(outcome)))
}

// RecordSubmission records one accepted POST /credential batch.
func RecordSubmission(ctx context.Context, reused bool) {
	meters().submissions.Add(ctx, 1, metric.WithAttributes(AttrSubmissionReuse.Bool(reused)))
}

// RecordWorkerProcessed records what the worker did with one credential request. credType
// is empty when the request could not be loaded.
func RecordWorkerProcessed(ctx context.Context, credType, result string) {
	meters().workerProcessed.Add(ctx, 1, metric.WithAttributes(
		AttrCredType.String(credType), AttrWorkerResult.String(result)))
}

// RegisterQueueDepth publishes depth() as the credential.worker.queue_depth gauge. The
// returned function unregisters it.
func RegisterQueueDepth(depth func() int64) (func() error, error) {
	m := otel.Meter(ScopeName)
	gauge, err := m.Int64ObservableGauge("credential.worker.queue_depth",
		metric.WithUnit("{request}"),
		metric.WithDescription("Credential request IDs waiting in the in-memory worker channel"))
	if err != nil {
		return nil, err
	}
	reg, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(gauge, depth())
		return nil
	}, gauge)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}
