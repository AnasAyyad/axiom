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
		[]string{"binance"}, []string{""},
	)
	if err != nil || prepared.DisplayName != "Trend Following · Binance Spot Testnet · BTCUSDT" ||
		prepared.WaitingReason == nil || !strings.Contains(*prepared.WaitingReason, "reauthentication") {
		t.Fatalf("prepared projection=%#v error=%v", prepared, err)
	}
	blockingReason := "arm_expired_or_revoked"
	blocked, err := generatedSandboxStrategySession(
		"session", "cross-exchange-arbitrage", &instrument, "blocked", now, nil, nil,
		&blockingReason, 2, []string{"binance", "bybit"}, []string{"", ""},
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
		[]string{"bybit"}, []string{""},
	)
	if err != nil || item.Instrument != nil || item.DisplayName != "Triangular Arbitrage · Bybit Demo" {
		t.Fatalf("legacy projection=%#v error=%v", item, err)
	}
}

func TestSandboxStrategySessionProjectionUsesLatestEvaluationForEveryAccount(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	instrument := "BTCUSDT"
	item, err := generatedSandboxStrategySession(
		"session", "cross-exchange-arbitrage", &instrument, "running", now, &now, nil, nil, 2,
		[]string{"binance", "bybit"}, []string{"strategy_plan_approved", "waiting_for_binance_coordinator"},
	)
	if err != nil || item.WaitingReason == nil ||
		!strings.Contains(*item.WaitingReason, "Binance Spot Testnet: a safe execution plan was approved") ||
		!strings.Contains(*item.WaitingReason, "Bybit Demo: waiting for the Binance coordinator") {
		t.Fatalf("running projection=%#v error=%v", item, err)
	}
}

func TestSandboxStrategySessionProjectionRejectsUnpairedEvaluationReasons(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	instrument := "BTCUSDT"
	if _, err := generatedSandboxStrategySession(
		"session", "trend", &instrument, "running", now, &now, nil, nil, 2,
		[]string{"binance"}, nil,
	); err == nil {
		t.Fatal("projection accepted an evaluation array that did not match its account topology")
	}
}
