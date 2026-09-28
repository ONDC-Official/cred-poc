package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns the service logger: JSON lines on stdout carrying trace_id and span_id
// whenever the record's context holds a span and, when export is enabled, a copy of every
// record sent to the global OTel LoggerProvider.
//
// Log with slog.InfoContext / ErrorContext and pass the request or worker ctx; that ctx is
// what ties a log line to its trace.
func NewLogger(export bool) *slog.Logger {
	return slog.New(newHandler(os.Stdout, export))
}

func newHandler(w io.Writer, export bool) slog.Handler {
	stdout := traceHandler{slog.NewJSONHandler(w, nil)}
	if !export {
		return stdout
	}
	return slog.NewMultiHandler(stdout, otelslog.NewHandler(ScopeName))
}

type contextKey string

const subscriberIDContextKey contextKey = "subscriber_id"

// WithSubscriberID attaches subscriberID to ctx.
func WithSubscriberID(ctx context.Context, subscriberID string) context.Context {
	if subscriberID == "" {
		return ctx
	}
	return context.WithValue(ctx, subscriberIDContextKey, subscriberID)
}

// SubscriberIDFromContext retrieves subscriberID from ctx if set.
func SubscriberIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(subscriberIDContextKey).(string); ok {
		return v
	}
	return ""
}

// traceHandler adds trace_id, span_id, and subscriber_id to records whose context carries them.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	hasTrace := false
	var sc trace.SpanContext
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		hasTrace = true
		sc = spanCtx
	}
	subID := SubscriberIDFromContext(ctx)

	if hasTrace || subID != "" {
		r = r.Clone()
		if hasTrace {
			r.AddAttrs(
				slog.String("trace_id", sc.TraceID().String()),
				slog.String("span_id", sc.SpanID().String()),
			)
		}
		if subID != "" {
			alreadyHasSubID := false
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "subscriber_id" {
					alreadyHasSubID = true
					return false
				}
				return true
			})
			if !alreadyHasSubID {
				r.AddAttrs(slog.String("subscriber_id", subID))
			}
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

