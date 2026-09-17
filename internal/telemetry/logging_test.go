package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceHandlerAddsTraceIDsFromContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(newHandler(&buf, false))

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03},
		SpanID:     trace.SpanID{0x04, 0x05},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.With("component", "test").InfoContext(ctx, "with span", "key", "value")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v: %s", err, buf.String())
	}
	if line["trace_id"] != sc.TraceID().String() {
		t.Fatalf("trace_id = %v, want %s", line["trace_id"], sc.TraceID())
	}
	if line["span_id"] != sc.SpanID().String() {
		t.Fatalf("span_id = %v, want %s", line["span_id"], sc.SpanID())
	}
	if line["component"] != "test" || line["key"] != "value" {
		t.Fatalf("attributes lost: %v", line)
	}
}

func TestTraceHandlerOmitsTraceIDsWithoutSpan(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	slog.New(newHandler(&buf, false)).InfoContext(context.Background(), "no span")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v: %s", err, buf.String())
	}
	if _, ok := line["trace_id"]; ok {
		t.Fatalf("unexpected trace_id without a span: %v", line)
	}
}
