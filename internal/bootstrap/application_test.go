package bootstrap

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	if code := Main([]string{"help"}, &output, &errorOutput); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if !strings.Contains(output.String(), "platform trader --mode shadow") {
		t.Fatal("help omitted exact trader command")
	}
}

func TestStartupRejectsInvalidProductConfigBeforeDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv("APP_CONFIG_FILE", missing)
	var output, errorOutput bytes.Buffer
	if code := Main([]string{"api"}, &output, &errorOutput); code == 0 {
		t.Fatal("startup accepted a missing product configuration")
	}
	if strings.Contains(errorOutput.String(), missing) {
		t.Fatal("configuration path leaked into startup error")
	}
}

func TestMainRejectsExchangeCredentialKeyWithoutValueLeak(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "fixture-value-that-must-not-appear")
	var output, errorOutput bytes.Buffer
	if code := Main([]string{"healthcheck"}, &output, &errorOutput); code == 0 {
		t.Fatal("credential-bearing environment was accepted")
	}
	if strings.Contains(errorOutput.String(), "fixture-value") {
		t.Fatal("credential value leaked into startup error")
	}
}
