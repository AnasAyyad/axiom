package bootstrap

import "testing"

func TestParseCommandExactSurface(t *testing.T) {
	accepted := [][]string{
		{"api"}, {"trader", "--mode", "shadow"}, {"recorder"},
		{"worker"}, {"admin", "migrate"}, {"healthcheck"},
		{"egress-proxy", "--exchange", "binance"}, {"egress-proxy", "--exchange", "bybit"},
		{"egress-proxy", "--exchange", "bybit-public"},
		{"sandbox-engine", "--exchange", "binance"},
		{"sandbox-engine", "--exchange", "bybit"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "prepare",
			"--input-file", "/run/secrets/binance_canary_request"},
		{"sandbox-canary", "--exchange", "bybit", "--phase", "verify",
			"--canary-id", "plan-canary", "--evidence-dir", "/evidence"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "recover",
			"--canary-id", "plan-canary"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "abort",
			"--canary-id", "plan-canary"},
	}
	for _, arguments := range accepted {
		if _, err := parseCommand(arguments); err != nil {
			t.Fatalf("command %v rejected: %v", arguments, err)
		}
	}
}

func TestParseCommandRejectsLaterModesAndExtraArguments(t *testing.T) {
	rejected := [][]string{
		{"trader", "--mode", "testnet"}, {"trader", "--mode", "demo"},
		{"trader", "--mode", "live"}, {"trader", "--mode", "paper"},
		{"admin", "migrate", "up"}, {"api", "extra"}, {"unknown"},
		{"sandbox-engine", "--exchange", "production"},
		{"egress-proxy", "--exchange", "production"},
		{"sandbox-engine", "--exchange", "binance", "extra"},
		{"sandbox-canary", "--exchange", "production", "--phase", "prepare",
			"--input-file", "/run/secrets/request"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "prepare"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "verify",
			"--canary-id", "plan-canary"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "prepare",
			"--input-file", "/run/secrets/request", "--canary-id", "mixed"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "abort",
			"--canary-id", "plan-canary", "--evidence-dir", "/evidence"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "abort",
			"--canary-id", "plan-canary", "--input-file", "/run/secrets/request"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "recover",
			"--canary-id", "plan-canary", "--evidence-dir", "/evidence"},
		{"sandbox-canary", "--exchange", "binance", "--phase", "recover",
			"--canary-id", "plan-canary", "--input-file", "/run/secrets/request"},
	}
	for _, arguments := range rejected {
		if _, err := parseCommand(arguments); err == nil {
			t.Fatalf("command %v accepted", arguments)
		}
	}
}
