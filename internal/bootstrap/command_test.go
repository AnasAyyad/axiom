package bootstrap

import "testing"

func TestParseCommandExactSurface(t *testing.T) {
	accepted := [][]string{
		{"api"}, {"trader", "--mode", "shadow"}, {"recorder"},
		{"worker"}, {"admin", "migrate"}, {"healthcheck"},
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
	}
	for _, arguments := range rejected {
		if _, err := parseCommand(arguments); err == nil {
			t.Fatalf("command %v accepted", arguments)
		}
	}
}
