package observability

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsCanariesAndSensitiveAttributes(t *testing.T) {
	const canary = "canary-secret-value-123"
	var output bytes.Buffer
	logger := NewLogger(&output, "engine-shadow", canary)
	logger.Error("failed "+canary,
		"event_code", "dependency_failed", "authorization", "Bearer "+canary,
		"api_token", canary, "cause", errors.New("wrapped "+canary),
		slog.Group("request", "cookie", canary, "correlation_id", "correlation-a"),
		"unsafe_object", map[string]string{"password": canary})
	encoded := output.String()
	if strings.Contains(encoded, canary) || strings.Contains(encoded, "Bearer") {
		t.Fatalf("secret leaked: %s", encoded)
	}
	if !strings.Contains(encoded, `"service":"engine-shadow"`) ||
		!strings.Contains(encoded, `"event_code":"dependency_failed"`) ||
		!strings.Contains(encoded, `"correlation_id":"correlation-a"`) {
		t.Fatalf("required structured fields missing: %s", encoded)
	}
}

func TestLoggerPreservesSafeScalars(t *testing.T) {
	var output bytes.Buffer
	NewLogger(&output, "api").Info("ready", "event_code", "service_ready", "attempt", 2, "healthy", true)
	encoded := output.String()
	for _, required := range []string{`"event_code":"service_ready"`, `"attempt":2`, `"healthy":true`} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("safe field missing from %s", encoded)
		}
	}
}
