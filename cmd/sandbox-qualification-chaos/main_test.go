package main

import (
	"context"
	"strings"
	"testing"
)

func TestSandboxQualificationChaosIsDefaultOffAndFormalOnly(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled string
		mode    string
	}{
		{name: "disabled"},
		{name: "wrong mode", enabled: "1", mode: "smoke"},
		{name: "wrong enable value", enabled: "true", mode: "formal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AXIOM_SANDBOX_QUALIFICATION_CHAOS_ENABLED", test.enabled)
			t.Setenv("AXIOM_SANDBOX_QUALIFICATION_CHAOS_MODE", test.mode)
			err := run(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), "default-off") {
				t.Fatalf("manual controller was not closed: %v", err)
			}
		})
	}
}

func TestQualificationEnvironmentDropsCredentialsAndDatabaseAccess(t *testing.T) {
	environment := qualificationEnvironment([]string{
		"PATH=/usr/bin",
		"GOTOOLCHAIN=local",
		"DB_PASSWORD_FILE=/secret/db",
		"BINANCE_API_KEY_FILE=/secret/binance",
		"BYBIT_API_SECRET_FILE=/secret/bybit",
		"AXIOM_SANDBOX_QUALIFICATION_RUN_ID=run",
	})
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{
		"DB_PASSWORD_FILE", "BINANCE", "BYBIT",
		"AXIOM_SANDBOX_QUALIFICATION_RUN_ID",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unsafe child environment retained %s", forbidden)
		}
	}
	for _, required := range []string{
		"PATH=/usr/bin",
		"GOTOOLCHAIN=local",
		"AXIOM_SANDBOX_RUNTIME_TEST_DSN=",
		"AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN=",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("child environment omitted %s", required)
		}
	}
}

func TestFixedHarnessRejectsUnknownScenario(t *testing.T) {
	_, _, err := (fixedHarness{passed: true, fact: "fact"}).Exercise(
		context.Background(), "not-a-scenario", "ignored",
	)
	if err == nil {
		t.Fatal("unknown chaos scenario accepted")
	}
}
