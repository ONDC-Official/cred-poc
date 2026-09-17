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

// traceHandler adds trace_id and span_id to records whose context carries a valid span. The
// OTLP bridge does not need it: exported log records carry the span context natively.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}
