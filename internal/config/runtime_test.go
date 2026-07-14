package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRuntimeUsesSafeDefaults(t *testing.T) {
	clearRuntimeEnvironment(t)
	configuration, err := LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.ShutdownTimeout != 60*time.Second {
		t.Fatalf("shutdown timeout = %s", configuration.ShutdownTimeout)
	}
	if configuration.Database.Port != 5432 || configuration.Database.MaxOpenConnections != 30 {
		t.Fatalf("unexpected database defaults: %#v", configuration.Database)
	}
}

func TestLoadRuntimeRejectsUnsafeSafetyValues(t *testing.T) {
	clearRuntimeEnvironment(t)
	for _, test := range []struct{ key, value string }{
		{key: "APP_FAIL_CLOSED", value: "false"},
		{key: "RISK_INITIAL_STATE", value: "NORMAL"},
		{key: "RISK_AUTO_UNPAUSE", value: "true"},
		{key: "BINANCE_PUBLIC_ENDPOINT_SET", value: "custom"},
		{key: "EXECUTION_MODE", value: "live"},
	} {
		t.Run(test.key, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			_, err := LoadRuntime()
			if err == nil || strings.Contains(err.Error(), test.value) {
				t.Fatalf("expected redacted rejection, got %v", err)
			}
		})
	}
}

func TestLoadRuntimeRejectsShutdownOverCeiling(t *testing.T) {
	clearRuntimeEnvironment(t)
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "61s")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("shutdown timeout above 60 seconds accepted")
	}
}

func TestLoadRuntimeValidatesOptionalAlertWebhook(t *testing.T) {
	clearRuntimeEnvironment(t)
	t.Setenv("ALERT_WEBHOOK_ENABLED", "true")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("enabled webhook without destination accepted")
	}
	t.Setenv("ALERT_WEBHOOK_URL", "https://alerts.example.invalid/axiom")
	t.Setenv("ALERT_WEBHOOK_ALLOWED_HOST", "alerts.example.invalid")
	t.Setenv("ALERT_WEBHOOK_TOKEN_FILE", "/run/secrets/alert_webhook_token")
	configuration, err := LoadRuntime()
	if err != nil || !configuration.AlertWebhook.Enabled {
		t.Fatalf("valid webhook rejected: %#v %v", configuration.AlertWebhook, err)
	}
	for _, value := range []string{"yes", "1", "TRUE"} {
		t.Setenv("ALERT_WEBHOOK_ENABLED", value)
		if _, err = LoadRuntime(); err == nil {
			t.Fatalf("boolean %q accepted", value)
		}
	}
}

func TestLoadRuntimeValidatesOptionalTracing(t *testing.T) {
	clearRuntimeEnvironment(t)
	if configuration, err := LoadRuntime(); err != nil || configuration.Tracing.Enabled {
		t.Fatalf("disabled tracing default invalid: %#v %v", configuration.Tracing, err)
	}
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("enabled tracing without collector accepted")
	}
	for _, endpoint := range []string{
		"http://collector.example.invalid/v1/traces",
		"https://user:pass@collector.example.invalid/v1/traces",
		"https://collector.example.invalid/v1/traces?token=unsafe",
	} {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
		if _, err := LoadRuntime(); err == nil || strings.Contains(err.Error(), endpoint) {
			t.Fatalf("unsafe tracing endpoint accepted or exposed: %q %v", endpoint, err)
		}
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example.invalid/v1/traces")
	configuration, err := LoadRuntime()
	if err != nil || !configuration.Tracing.Enabled {
		t.Fatalf("valid tracing configuration rejected: %#v %v", configuration.Tracing, err)
	}
}

func clearRuntimeEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_FAIL_CLOSED", "APP_SHUTDOWN_TIMEOUT", "RISK_INITIAL_STATE",
		"RISK_AUTO_UNPAUSE", "RISK_FAIL_CLOSED", "BINANCE_PUBLIC_ENDPOINT_SET",
		"EXECUTION_MODE", "DB_PORT", "DB_MAX_OPEN_CONNECTIONS",
		"DB_CONNECTION_MAX_LIFETIME",
		"DB_CONNECTION_TIMEOUT", "DB_STATEMENT_TIMEOUT",
		"ALERT_WEBHOOK_ENABLED", "ALERT_WEBHOOK_URL", "ALERT_WEBHOOK_ALLOWED_HOST", "ALERT_WEBHOOK_TOKEN_FILE",
		"OTEL_TRACING_ENABLED", "OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
	for _, key := range []string{
		"APP_FAIL_CLOSED", "APP_SHUTDOWN_TIMEOUT", "RISK_INITIAL_STATE",
		"RISK_AUTO_UNPAUSE", "RISK_FAIL_CLOSED", "BINANCE_PUBLIC_ENDPOINT_SET",
		"EXECUTION_MODE", "DB_PORT", "DB_MAX_OPEN_CONNECTIONS",
		"DB_CONNECTION_MAX_LIFETIME",
		"DB_CONNECTION_TIMEOUT", "DB_STATEMENT_TIMEOUT",
		"ALERT_WEBHOOK_ENABLED", "ALERT_WEBHOOK_URL", "ALERT_WEBHOOK_ALLOWED_HOST", "ALERT_WEBHOOK_TOKEN_FILE",
		"OTEL_TRACING_ENABLED", "OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		t.Setenv(key, defaultForUnset(key))
	}
}

func defaultForUnset(key string) string {
	defaults := map[string]string{
		"APP_FAIL_CLOSED": "true", "APP_SHUTDOWN_TIMEOUT": "60s",
		"RISK_INITIAL_STATE": "PAUSED", "RISK_AUTO_UNPAUSE": "false",
		"RISK_FAIL_CLOSED": "true", "BINANCE_PUBLIC_ENDPOINT_SET": "market-data-only-v1",
		"EXECUTION_MODE": "shadow", "DB_PORT": "5432", "DB_MAX_OPEN_CONNECTIONS": "30",
		"DB_CONNECTION_MAX_LIFETIME": "30m",
		"DB_CONNECTION_TIMEOUT":      "5s", "DB_STATEMENT_TIMEOUT": "10s",
		"ALERT_WEBHOOK_ENABLED": "false", "ALERT_WEBHOOK_URL": "",
		"ALERT_WEBHOOK_ALLOWED_HOST": "", "ALERT_WEBHOOK_TOKEN_FILE": "",
		"OTEL_TRACING_ENABLED": "false", "OTEL_EXPORTER_OTLP_ENDPOINT": "",
	}
	return defaults[key]
}
