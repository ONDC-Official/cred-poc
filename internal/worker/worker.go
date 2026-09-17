package worker

import (
	"context"
	"log/slog"
	"time"

	"credential-service/internal/models"
	"credential-service/internal/repository"
	"credential-service/internal/service"
	"credential-service/internal/telemetry"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type CredentialWorker struct {
	credService *service.CredentialService
	credReqRepo *repository.CredentialRequestRepository
	enumCache   *models.EnumCache
	jobCh       <-chan uuid.UUID
	pollTicker  *time.Ticker
	stopCh      chan struct{}
	// unregisterGauge removes the queue-depth gauge callback; nil if registration failed.
	unregisterGauge func() error
}

func NewCredentialWorker(
	credService *service.CredentialService,
	credReqRepo *repository.CredentialRequestRepository,
	enumCache *models.EnumCache,
) *CredentialWorker {
	return &CredentialWorker{
		credService: credService,
		credReqRepo: credReqRepo,
		enumCache:   enumCache,
		jobCh:       credService.JobChannel(),
		pollTicker:  time.NewTicker(30 * time.Second),
		stopCh:      make(chan struct{}),
	}
}

func (w *CredentialWorker) Start(ctx context.Context) {
	unregister, err := telemetry.RegisterQueueDepth(func() int64 { return int64(len(w.jobCh)) })
	if err != nil {
		slog.WarnContext(ctx, "credential worker queue-depth gauge not registered", "error", err)
	}
	w.unregisterGauge = unregister

	slog.InfoContext(ctx, "credential worker started")
	go w.run(ctx)
}

func (w *CredentialWorker) Stop() {
	close(w.stopCh)
	w.pollTicker.Stop()
	if w.unregisterGauge != nil {
		_ = w.unregisterGauge()
	}
	slog.Info("credential worker stopped")
}

func (w *CredentialWorker) run(ctx context.Context) {
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case id := <-w.jobCh:
			w.processOne(ctx, id)
		case <-w.pollTicker.C:
			w.pollPending(ctx)
		}
	}
}

// processOne runs one credential request in its own root trace. It is not linked to the
// POST /credential trace that queued it: the row does not store a traceparent.
func (w *CredentialWorker) processOne(ctx context.Context, id uuid.UUID) {
	ctx, span := telemetry.Tracer().Start(ctx, "credential.worker.process",
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(telemetry.AttrRequestID.String(id.String())),
	)
	defer span.End()

	slog.InfoContext(ctx, "processing credential request", "credential_request_id", id)
	if err := w.credService.ProcessCredentialRequest(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "credential request processing failed")
		telemetry.RecordWorkerProcessed(ctx, "", telemetry.WorkerError)
		slog.ErrorContext(ctx, "error processing credential request", "credential_request_id", id, "error", err)
	}
}

func (w *CredentialWorker) pollPending(ctx context.Context) {
	pollCtx, span := telemetry.Tracer().Start(ctx, "credential.worker.poll", trace.WithNewRoot())
	pendingID := w.enumCache.VerificationStatusID(models.VerificationPending)
	requests, err := w.credReqRepo.FindPending(pollCtx, pendingID, 50)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "poll pending requests failed")
		span.End()
		slog.ErrorContext(pollCtx, "error polling pending requests", "error", err)
		return
	}
	span.SetAttributes(attribute.Int("credential.worker.batch_size", len(requests)))
	span.End()

	// Each request gets its own trace; hanging a whole batch off the poll span would make
	// one trace per 30s tick that can run for minutes.
	for _, req := range requests {
		w.processOne(ctx, req.ID)
	}
}
