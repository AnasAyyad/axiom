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

func clearRuntimeEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_FAIL_CLOSED", "APP_SHUTDOWN_TIMEOUT", "RISK_INITIAL_STATE",
		"RISK_AUTO_UNPAUSE", "RISK_FAIL_CLOSED", "BINANCE_PUBLIC_ENDPOINT_SET",
		"EXECUTION_MODE", "DB_PORT", "DB_MAX_OPEN_CONNECTIONS",
		"DB_CONNECTION_MAX_LIFETIME",
		"DB_CONNECTION_TIMEOUT", "DB_STATEMENT_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	for _, key := range []string{
		"APP_FAIL_CLOSED", "APP_SHUTDOWN_TIMEOUT", "RISK_INITIAL_STATE",
		"RISK_AUTO_UNPAUSE", "RISK_FAIL_CLOSED", "BINANCE_PUBLIC_ENDPOINT_SET",
		"EXECUTION_MODE", "DB_PORT", "DB_MAX_OPEN_CONNECTIONS",
		"DB_CONNECTION_MAX_LIFETIME",
		"DB_CONNECTION_TIMEOUT", "DB_STATEMENT_TIMEOUT",
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
	}
	return defaults[key]
}
