package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
)

func TestSandboxStrategySessionProjectionExplainsWorkerAndArmBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	instrument := "BTCUSDT"
	prepared, err := generatedSandboxStrategySession(
		"session", "trend", &instrument, "prepared", now, nil, nil, nil, 1,
		[]string{"binance"},
	)
	if err != nil || prepared.DisplayName != "Trend Following · Binance Spot Testnet · BTCUSDT" ||
		prepared.WaitingReason == nil || !strings.Contains(*prepared.WaitingReason, "not installed") {
		t.Fatalf("prepared projection=%#v error=%v", prepared, err)
	}
	blockingReason := "arm_expired_or_revoked"
	blocked, err := generatedSandboxStrategySession(
		"session", "cross-exchange-arbitrage", &instrument, "blocked", now, nil, nil,
		&blockingReason, 2, []string{"binance", "bybit"},
	)
	if err != nil || blocked.State != generated.SandboxStrategySessionStateBlocked ||
		blocked.WaitingReason == nil || !strings.Contains(*blocked.WaitingReason, "expired or was revoked") {
		t.Fatalf("blocked projection=%#v error=%v", blocked, err)
	}
}

func TestSandboxStrategySessionProjectionDoesNotGuessLegacyInstrument(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := generatedSandboxStrategySession(
		"session", "triangular", nil, "stopped", now, nil, &now, nil, 1,
		[]string{"bybit"},
	)
	if err != nil || item.Instrument != nil || item.DisplayName != "Triangular Arbitrage · Bybit Demo" {
		t.Fatalf("legacy projection=%#v error=%v", item, err)
	}
}
