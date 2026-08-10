package postgres

import (
	"testing"
	"time"
)

func TestPublicShadowActivityRequiresCompleteExactHealthyScope(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	next := now.Add(2 * time.Hour)
	claim := PublicShadowClaim{ID: "shadow-test", MarketScopeRequired: true,
		MarketScopes: []PublicShadowMarketScope{{
			Ordinal: 1, ExchangeID: "bybit", InstrumentID: "BTCUSDT", Purpose: "primary",
		}}}
	activity := PublicShadowActivity{State: "waiting", ReasonCode: "waiting_for_finalized_candle",
		Summary: "The next finalized candle is not usable yet.", NextEvaluationAt: &next,
		TriggerCondition: "After the next finalized four-hour candle and finalization delay.",
		ObservedAt:       now, Inputs: []PublicShadowInputHealth{{ExchangeID: "bybit", InstrumentID: "BTCUSDT",
			State: "HEALTHY", Reason: "The selected public book is fresh.", Fresh: true,
			BookVersion: 4, Age: 20 * time.Millisecond, ObservedAt: now}}}
	if !validPublicShadowActivity(claim, activity) {
		t.Fatal("complete exact activity observation was rejected")
	}
	activity.Inputs[0].ExchangeID = "binance"
	if validPublicShadowActivity(claim, activity) {
		t.Fatal("out-of-scope input health was accepted")
	}
	activity.Inputs[0].ExchangeID = "bybit"
	activity.Inputs[0].Fresh = false
	if validPublicShadowActivity(claim, activity) {
		t.Fatal("healthy input with contradictory freshness was accepted")
	}
}
