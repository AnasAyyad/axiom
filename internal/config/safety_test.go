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
		"BINANCE_PRIVATE_PROXY=http://proxy.invalid",
		"BYBIT_ENDPOINT_URL=https://example.invalid",
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
	for _, key := range []string{
		"AXIOM_BINANCE_TESTNET_API_KEY_FILE=/run/secrets/binance_testnet_api_key",
		"AXIOM_BINANCE_TESTNET_API_SECRET_FILE=/run/secrets/binance_testnet_api_secret",
		"AXIOM_BYBIT_DEMO_API_KEY_FILE=/run/secrets/bybit_demo_api_key",
		"AXIOM_BYBIT_DEMO_API_SECRET_FILE=/run/secrets/bybit_demo_api_secret",
		"AXIOM_TOTP_SEED_FILE=/run/secrets/totp_seed",
	} {
		if err := ValidateEnvironment([]string{key}); err != nil {
			t.Fatalf("reviewed file reference rejected for %q: %v", key, err)
		}
	}
	if err := ValidateEnvironment([]string{
		"AXIOM_TOTP_SEED_FILE=/tmp/axiom-platform-smoke/totp_seed",
	}); err != nil {
		t.Fatalf("local TOTP seed file reference rejected: %v", err)
	}
	for _, key := range []string{
		"AXIOM_TOTP_SEED_FILE=relative",
		"AXIOM_BINANCE_TESTNET_API_KEY_FILE=/tmp/axiom-platform-smoke/binance_key",
	} {
		if err := ValidateEnvironment([]string{key}); err == nil {
			t.Fatalf("unsafe secret file reference accepted: %s", key)
		}
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
