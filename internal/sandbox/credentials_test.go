package sandbox

import (
	"errors"
	"testing"
)

func TestRejectUnsafeSandboxEnvironment(t *testing.T) {
	valid := []string{
		BinanceAPIKeyFileEnvironment + "=/run/secrets/binance-key",
		BybitAPISecretFileEnvironment + "=/run/secrets/bybit-secret",
		TOTPSeedFileEnvironment + "=/run/secrets/totp",
	}
	if err := RejectUnsafeSandboxEnvironment(valid); err != nil {
		t.Fatalf("approved file references rejected: %v", err)
	}
	cases := [][]string{
		{"AXIOM_BINANCE_TESTNET_API_KEY=value"},
		{"AXIOM_BYBIT_DEMO_API_SECRET=value"},
		{"AXIOM_BYBIT_DEMO_BASE_URL=https://example.invalid"},
		{"AXIOM_BINANCE_TESTNET_PROXY=http://proxy.invalid"},
	}
	for _, environment := range cases {
		if err := RejectUnsafeSandboxEnvironment(environment); err == nil {
			t.Fatalf("unsafe environment accepted: %v", environment)
		}
	}
}

func TestUnsupportedCredentialAccountFailsClosed(t *testing.T) {
	_, err := LoadCredentialPair(Exchange("production"))
	if !errors.Is(err, ErrUnsupportedAccount) {
		t.Fatalf("unexpected error: %v", err)
	}
}
