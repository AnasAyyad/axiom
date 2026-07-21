package simulation

import (
	"testing"
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

func TestExactFeePriceLatencyAndFillModels(t *testing.T) {
	notional, _ := domain.ParseNotional("100")
	taker, _ := domain.ParseRate("0.001")
	maker, _ := domain.ParseRate("0.0005")
	rebate, _ := domain.ParseRate("0.0001")
	fees, err := (FeeModel{Version: "fee-v1", TakerRate: taker, MakerRate: maker,
		RebateRate: rebate, DecimalScale: 8}).Calculate(notional, false)
	if err != nil || fees.Charge.String() != "0.1" || fees.Rebate.String() != "0.01" {
		t.Fatalf("fees = %#v, %v", fees, err)
	}
	price, _ := domain.ParsePrice("100")
	onePercent, _ := domain.ParsePercent("0.01")
	zero, _ := domain.ParsePercent("0")
	adjusted, err := (PriceModel{Version: "price-v1", Spread: onePercent, Slippage: zero,
		Impact: zero, AdverseSelection: zero, DecimalScale: 8}).Apply(price, domain.SideBuy)
	if err != nil || adjusted.String() != "101" {
		t.Fatalf("adjusted price = %s, %v", adjusted.String(), err)
	}
	randomness, _ := runtimecore.NewRandomness(make([]byte, 32))
	key := runtimecore.RandomKey{RunID: "run", ComponentID: "latency-v1", DecisionID: "decision",
		OrderLegID: "leg", EventID: "event"}
	latency := LatencyModel{Version: "latency-v1", Samples: []time.Duration{time.Millisecond, 2 * time.Millisecond}}
	first, _ := latency.Sample(randomness, key)
	second, _ := latency.Sample(randomness, key)
	if first != second {
		t.Fatalf("keyed latency changed: %s/%s", first, second)
	}
	partialRatio, _ := domain.ParsePercent("0.25")
	fill := FillModel{Version: "fill-v1", PartialPPM: 1_000_000, PartialRatio: partialRatio, QuantityScale: 8}
	disposition, err := fill.Disposition(randomness, key)
	quantity, _ := domain.ParseQuantity("4")
	limited, limitErr := fill.Limit(quantity, disposition)
	if err != nil || limitErr != nil || disposition != FillPartial || limited.String() != "1" {
		t.Fatalf("fill = %s/%s, %v/%v", disposition, limited.String(), err, limitErr)
	}
}

func TestMakerQueueNeverClaimsVisibleQuantityAhead(t *testing.T) {
	ahead, _ := domain.ParsePercent("0.75")
	traded, _ := domain.ParseQuantity("8")
	eligible, err := (MakerQueueModel{Version: "queue-v1", QueueAhead: ahead,
		QuantityScale: 8}).EligibleQuantity(traded)
	if err != nil || eligible.String() != "2" {
		t.Fatalf("eligible = %s, %v", eligible.String(), err)
	}
}
