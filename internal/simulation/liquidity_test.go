package simulation

import (
	"testing"

	"axiom/internal/domain"
)

func TestSharedLiquidityCannotBeDoubleConsumed(t *testing.T) {
	ledger := NewLiquidityLedger()
	key := liquidityKey(t, "combined")
	displayed, _ := domain.ParseQuantity("1")
	request, _ := domain.ParseQuantity("0.75")
	first, err := ledger.Consume(key, displayed, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Consume(key, displayed, request)
	if err != nil || first.String() != "0.75" || second.String() != "0.25" ||
		ledger.Consumed(key).String() != "1" {
		t.Fatalf("claims = %s/%s total=%s, %v", first.String(), second.String(), ledger.Consumed(key).String(), err)
	}
}

func TestLiquidityNamespacesAreIndependent(t *testing.T) {
	ledger := NewLiquidityLedger()
	displayed, _ := domain.ParseQuantity("1")
	request, _ := domain.ParseQuantity("1")
	for _, namespace := range []string{"counterfactual-a", "counterfactual-b"} {
		claimed, err := ledger.Consume(liquidityKey(t, namespace), displayed, request)
		if err != nil || claimed.String() != "1" {
			t.Fatalf("namespace %s claim = %s, %v", namespace, claimed.String(), err)
		}
	}
}

func TestLiquiditySchedulerIsPermutationInvariant(t *testing.T) {
	first := runScheduledClaims(t, []string{"b", "a"})
	second := runScheduledClaims(t, []string{"a", "b"})
	if first != second || first != "a=0.75,b=0.25" {
		t.Fatalf("scheduled claims = %q/%q", first, second)
	}
}

func runScheduledClaims(t *testing.T, order []string) string {
	t.Helper()
	ledger := NewLiquidityLedger()
	scheduler, _ := NewLiquidityScheduler(ledger)
	displayed, _ := domain.ParseQuantity("1")
	request, _ := domain.ParseQuantity("0.75")
	claims := make(map[string]string)
	work := make([]ScheduledLiquidityWork, 0, len(order))
	for _, identity := range order {
		identity := identity
		work = append(work, ScheduledLiquidityWork{Key: identity, Execute: func(ledger *LiquidityLedger) error {
			claimed, err := ledger.Consume(liquidityKey(t, "combined"), displayed, request)
			claims[identity] = claimed.String()
			return err
		}})
	}
	if err := scheduler.Run(work); err != nil {
		t.Fatal(err)
	}
	return "a=" + claims["a"] + ",b=" + claims["b"]
}

func liquidityKey(t *testing.T, namespace string) LiquidityKey {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	return LiquidityKey{Namespace: namespace, Exchange: "binance", Instrument: instrument,
		MarketVersion: 1, Side: domain.SideBuy, Price: "100"}
}
