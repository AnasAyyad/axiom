package operationalReadiness

import (
	"strings"
	"testing"
)

func TestProductionTargetPredicateUsesCanonicalSandboxPairs(t *testing.T) {
	got := strings.Join(strings.Fields(productionTargetPredicate), " ")
	want := "NOT ( (exchange='binance' AND environment='spot_testnet') OR (exchange='bybit' AND environment='demo') )"
	if got != want {
		t.Fatalf("production target predicate = %q, want canonical sandbox pairs", got)
	}
}
