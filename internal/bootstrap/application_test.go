package bootstrap

import (
	"bytes"
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
