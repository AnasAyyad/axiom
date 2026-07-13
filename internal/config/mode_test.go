package config

import "testing"

func TestParseExecutionMode(t *testing.T) {
	for _, mode := range []string{"backtest", "replay", "paper", "shadow"} {
		if _, err := ParseExecutionMode(mode); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	for _, mode := range []string{"", "live", "testnet", "demo", "Shadow", " shadow", "shadow "} {
		if _, err := ParseExecutionMode(mode); err == nil {
			t.Fatalf("mode %q accepted", mode)
		}
	}
}

func FuzzParseExecutionMode(f *testing.F) {
	for _, seed := range []string{"shadow", "live", "testnet", "demo", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		mode, err := ParseExecutionMode(value)
		allowed := value == "backtest" || value == "replay" || value == "paper" || value == "shadow"
		if allowed != (err == nil) {
			t.Fatalf("value %q produced mode %q and error %v", value, mode, err)
		}
	})
}
