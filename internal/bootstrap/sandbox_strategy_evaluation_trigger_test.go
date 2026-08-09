package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
)

func TestSandboxStrategyEvaluationTriggerRestoresMeanReversionDualTimeframeIdentity(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyMeanReversion, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	primary := readinessCandles(instrument, "1h", now, price, 28)
	higher := readinessCandles(instrument, "4h", now, price, 210)
	input := meanreversion.Input{Ordinal: 7, LogicalTime: 11, Instrument: instrument,
		PrimaryCandles: primary, HigherCandles: higher}
	canonical, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	fromJournal, err := sandboxStrategyEvaluationTriggerFromCanonicalInput(work, canonical)
	if err != nil {
		t.Fatal(err)
	}
	fromMarket, err := sandboxStrategyEvaluationTrigger(work, sandbox.StrategyMarketInput{
		Instrument: instrument,
		Candles: map[string][]exchangecontracts.Candle{
			"1h": primary,
			"4h": higher,
		},
	})
	if err != nil || fromJournal != fromMarket || !projectorHash256(fromJournal) {
		t.Fatalf("journal=%q market=%q error=%v", fromJournal, fromMarket, err)
	}
	changed := append([]exchangecontracts.Candle(nil), primary...)
	changed[len(changed)-1].RawPayloadHash = strings.Repeat("d", 64)
	changedTrigger, err := sandboxStrategyEvaluationTrigger(work, sandbox.StrategyMarketInput{
		Instrument: instrument,
		Candles: map[string][]exchangecontracts.Candle{
			"1h": changed,
			"4h": higher,
		},
	})
	if err != nil || changedTrigger == fromMarket {
		t.Fatalf("changed=%q prior=%q error=%v", changedTrigger, fromMarket, err)
	}
}
