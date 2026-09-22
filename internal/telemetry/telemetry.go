// Package telemetry sets up OpenTelemetry traces, metrics and logs for the service.
//
// Export is off unless CREDENTIAL_SERVICE_OTEL_ENABLED=true. Off, the global providers stay
// the SDK's no-ops, so instrumentation throughout the code costs next to nothing and tests
// need no setup. On, all three signals go over OTLP/HTTP to wherever the standard OTEL_*
// env vars point (OTEL_EXPORTER_OTLP_ENDPOINT defaults to http://localhost:4318).
//
// Never put a credential id, name, dob, request or response body, or an Authorization header
// on a span, metric or log attribute. They are PII or secrets, and telemetry backends are
// neither access-controlled nor retained like the database is.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"credential-service/internal/config"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// ScopeName is the instrumentation scope for the service's own spans, metrics and logs.
const ScopeName = "credential-service"

// Shutdown flushes and stops whatever Setup started. It is always safe to call.
type Shutdown func(context.Context) error

// Setup installs the global propagator and logger and, when telemetry is enabled, the OTLP
// tracer, meter and logger providers. Call it before anything else that logs or opens
// connections, and call the returned Shutdown last.
func Setup(ctx context.Context, cfg *config.Config) (Shutdown, error) {
	// Set unconditionally: a traceparent from the ONDC Registry is honoured, and forwarded on
	// outbound calls, whether or not this service exports anything itself.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Telemetry.Enabled {
		slog.SetDefault(NewLogger(false))
		return func(context.Context) error { return nil }, nil
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	var shutdowns []Shutdown
	shutdown := func(ctx context.Context) error {
		var errs error
		// Reverse order: logs and metrics emitted while tracing shuts down still get out.
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = errors.Join(errs, shutdowns[i](ctx))
		}
		return errs
	}
	fail := func(err error) (Shutdown, error) {
		return nil, errors.Join(err, shutdown(ctx))
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return fail(fmt.Errorf("create OTLP trace exporter: %w", err))
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	shutdowns = append(shutdowns, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	metricsEnabled := os.Getenv("OTEL_METRICS_EXPORTER") != "none"
	if metricsEnabled {
		metricExporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return fail(fmt.Errorf("create OTLP metric exporter: %w", err))
		}
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		)
		shutdowns = append(shutdowns, meterProvider.Shutdown)
		otel.SetMeterProvider(meterProvider)

		if err := runtime.Start(); err != nil {
			return fail(fmt.Errorf("start runtime metrics: %w", err))
		}
	}

	logsEnabled := os.Getenv("OTEL_LOGS_EXPORTER") != "none"
	if logsEnabled {
		logExporter, err := otlploghttp.New(ctx)
		if err != nil {
			return fail(fmt.Errorf("create OTLP log exporter: %w", err))
		}
		loggerProvider := sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		)
		shutdowns = append(shutdowns, loggerProvider.Shutdown)
		global.SetLoggerProvider(loggerProvider)
	}

	slog.SetDefault(NewLogger(logsEnabled))
	slog.InfoContext(ctx, "telemetry enabled; exporting traces over OTLP/HTTP")
	return shutdown, nil
}

func newResource(ctx context.Context, cfg *config.Config) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithHost(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.App.Name),
			semconv.DeploymentEnvironmentNameKey.String(cfg.App.Env),
		),
		// Last, so OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES override the values above.
		resource.WithFromEnv(),
	)
	// A partial resource is still usable; only a hard failure should stop boot.
	if errors.Is(err, resource.ErrPartialResource) || errors.Is(err, resource.ErrSchemaURLConflict) {
		return res, nil
	}
	return res, err
}
