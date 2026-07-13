package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateEnvironment(t *testing.T) {
	for _, key := range []string{
		"BINANCE_API_KEY=value",
		"EXCHANGE_SIGNING_KEY=value",
		"ENABLE_LIVE_TRADING=true",
		"ALLOW_WITHDRAWAL=true",
		"FUTURES_ENABLED=true",
	} {
		err := ValidateEnvironment([]string{key})
		if err == nil || strings.Contains(err.Error(), "value") {
			t.Fatalf("expected redacted rejection for %q, got %v", key, err)
		}
	}
	if err := ValidateEnvironment([]string{"DB_PASSWORD_FILE=/run/secrets/db", "EXECUTION_MODE=shadow"}); err != nil {
		t.Fatalf("safe environment rejected: %v", err)
	}
}

func TestExampleEnvironmentContainsNoForbiddenRuntimeKey(t *testing.T) {
	content, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	var environment []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			environment = append(environment, line)
		}
	}
	if err := ValidateEnvironment(environment); err != nil {
		t.Fatalf("committed example environment fails closed: %v", err)
	}
}
