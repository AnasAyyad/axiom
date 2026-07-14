package observability

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type testSpanExporter struct {
	exports atomic.Uint64
	err     error
	block   <-chan struct{}
}

func (exporter *testSpanExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	exporter.exports.Add(1)
	if exporter.block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exporter.block:
		}
	}
	return exporter.err
}

func (*testSpanExporter) Shutdown(context.Context) error { return nil }

func tracingFixture(exporter sdktrace.SpanExporter) *Tracing {
	return newTracingWithExporter(TracingConfiguration{
		Enabled: true, Endpoint: "https://collector.example.invalid/v1/traces",
		Service: "engine-shadow", InstanceID: "shadow-01",
	}, exporter)
}

func TestDisabledTracingIsNoop(t *testing.T) {
	tracing, err := NewTracing(context.Background(), TracingConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	_, span := tracing.Tracer("axiom/test").Start(context.Background(), "disabled")
	span.End()
	if span.IsRecording() {
		t.Fatal("disabled tracing recorded a span")
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTracingRejectsUnsafeExporterConfiguration(t *testing.T) {
	for _, configuration := range []TracingConfiguration{
		{Enabled: true, Endpoint: "http://collector.invalid/v1/traces", Service: "api", InstanceID: "api-1"},
		{Enabled: true, Endpoint: "https://user:secret@collector.invalid/v1/traces", Service: "api", InstanceID: "api-1"},
		{Enabled: true, Endpoint: "https://collector.invalid/v1/traces?token=secret", Service: "api", InstanceID: "api-1"},
		{Enabled: true, Endpoint: "https://collector.invalid/v1/traces", Service: "api\nunsafe", InstanceID: "api-1"},
	} {
		if _, err := NewTracing(context.Background(), configuration); err == nil {
			t.Fatalf("unsafe configuration accepted: %#v", configuration)
		}
	}
}

func TestTraceExportFailureCannotBlockProducer(t *testing.T) {
	var logOutput bytes.Buffer
	ConfigureTraceErrorHandler(NewLogger(&logOutput, "trace-test"))
	release := make(chan struct{})
	exporter := &testSpanExporter{block: release, err: errors.New("fixture-sensitive-detail")}
	tracing := tracingFixture(exporter)
	started := time.Now()
	for index := 0; index < traceQueueSize*2; index++ {
		_, span := tracing.Tracer("axiom/hot-path").Start(context.Background(), "bounded-work")
		span.End()
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("span producers blocked for %s", elapsed)
	}
	close(release)
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tracing.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if exporter.exports.Load() == 0 {
		t.Fatal("test exporter was never exercised")
	}
	if output := logOutput.String(); output != "" && (contains(output, "fixture-sensitive-detail") || !contains(output, `"event_code":"trace_export_failed"`)) {
		t.Fatalf("trace failure log was unsafe or unstructured: %s", output)
	}
}

func contains(value, fragment string) bool {
	return len(fragment) <= len(value) && strings.Contains(value, fragment)
}
