package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const (
	traceQueueSize      = 2048
	traceBatchSize      = 256
	traceExportInterval = 5 * time.Second
	traceExportTimeout  = 5 * time.Second
)

// TracingConfiguration is the closed runtime contract for optional OTLP/HTTP
// tracing. The endpoint may not contain credentials, query data, or fragments.
type TracingConfiguration struct {
	Enabled    bool
	Endpoint   string
	Service    string
	InstanceID string
}

// Tracing owns one OpenTelemetry-compatible provider and its bounded exporter.
// The SDK batch processor drops on a full queue instead of blocking producers.
type Tracing struct {
	provider trace.TracerProvider
	shutdown func(context.Context) error
}

// ConfigureTraceErrorHandler routes SDK/exporter failures through the
// structured redacting logger without exposing collector or transport detail.
func ConfigureTraceErrorHandler(logger *slog.Logger) {
	if logger == nil {
		return
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		logger.Warn("trace export failed", "event_code", "trace_export_failed")
	}))
}

// NewTracing constructs a true no-op when disabled and a bounded asynchronous
// OTLP/HTTP exporter when enabled.
func NewTracing(ctx context.Context, configuration TracingConfiguration) (*Tracing, error) {
	if !configuration.Enabled {
		return &Tracing{provider: tracenoop.NewTracerProvider(), shutdown: func(context.Context) error { return nil }}, nil
	}
	if err := validateTracingConfiguration(configuration); err != nil {
		return nil, err
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(configuration.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("trace_exporter_initialization_failed")
	}
	return newTracingWithExporter(configuration, exporter), nil
}

func newTracingWithExporter(configuration TracingConfiguration, exporter sdktrace.SpanExporter) *Tracing {
	wrapped := redactingSpanExporter{SpanExporter: exporter}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", configuration.Service),
			attribute.String("service.instance.id", configuration.InstanceID),
		)),
		sdktrace.WithBatcher(
			wrapped,
			sdktrace.WithMaxQueueSize(traceQueueSize),
			sdktrace.WithMaxExportBatchSize(traceBatchSize),
			sdktrace.WithBatchTimeout(traceExportInterval),
			sdktrace.WithExportTimeout(traceExportTimeout),
		),
	)
	return &Tracing{provider: provider, shutdown: provider.Shutdown}
}

// Tracer returns the provider-owned tracer without installing mutable global
// state. Call sites therefore keep their dependency explicit.
func (tracing *Tracing) Tracer(name string) trace.Tracer {
	return tracing.provider.Tracer(name)
}

// Shutdown drains the bounded queue within the caller's lifecycle deadline.
func (tracing *Tracing) Shutdown(ctx context.Context) error {
	return tracing.shutdown(ctx)
}

func validateTracingConfiguration(configuration TracingConfiguration) error {
	endpoint, err := url.Parse(configuration.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || !safeTraceIdentity(configuration.Service) ||
		!safeTraceIdentity(configuration.InstanceID) {
		return fmt.Errorf("trace_configuration_invalid")
	}
	return nil
}

func safeTraceIdentity(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	return !strings.ContainsAny(value, "\r\n\t /@?#")
}

type redactingSpanExporter struct{ sdktrace.SpanExporter }

// ExportSpans replaces exporter details with one stable reason before the SDK
// error handler sends the failure through structured logging.
func (exporter redactingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := exporter.SpanExporter.ExportSpans(ctx, spans); err != nil {
		return errors.New("trace_export_failed")
	}
	return nil
}
