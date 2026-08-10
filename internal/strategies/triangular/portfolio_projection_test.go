package triangular

import (
	"testing"

	"axiom/internal/domain"
)

func TestProjectAvailableBalancesAppliesFullCycleAndRetainsStrandedInventory(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	before := map[domain.AssetSymbol]domain.Balance{
		asset("USDT"): balance("500"), asset("BTC"): balance("0"), asset("ETH"): balance("0"),
	}
	completed, err := Simulate(candidate, &scriptedTimeline{markets: input.Markets}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	projected, err := ProjectAvailableBalances(before, candidate, completed)
	if err != nil || projected["USDT"].Compare(before["USDT"]) <= 0 ||
		projected["BTC"].Compare(balance("0")) < 0 || projected["ETH"].Compare(balance("0")) < 0 {
		t.Fatalf("completed projection=%#v error=%v", projected, err)
	}
	for _, leg := range completed.Legs {
		if leg.SourceDust.Compare(quantity("0")) > 0 &&
			projected[leg.Source].Compare(balance("0")) <= 0 {
			t.Fatalf("source dust for %s was not retained: projection=%#v leg=%#v",
				leg.Source, projected, leg)
		}
	}

	stranded, err := Simulate(candidate, &scriptedTimeline{markets: input.Markets,
		failures: map[string]bool{"BTC/ETH": true, "BTC/USDT": true}}, testLatency())
	if err != nil || !stranded.Recovery.Quarantined {
		t.Fatalf("stranded simulation=%#v error=%v", stranded, err)
	}
	projected, err = ProjectAvailableBalances(before, candidate, stranded)
	if err != nil || projected["USDT"].Compare(before["USDT"]) >= 0 ||
		projected["BTC"].Compare(balance("0")) <= 0 {
		t.Fatalf("stranded projection=%#v error=%v", projected, err)
	}
}

func TestPreferredCandidateMatchesWorstCaseOrdering(t *testing.T) {
	candidates, err := Evaluate(profitableInput(t, false))
	if err != nil {
		t.Fatal(err)
	}
	winner, err := PreferredCandidate(candidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.WorstCaseNet.Compare(winner.WorstCaseNet) > 0 ||
			(candidate.WorstCaseNet.Compare(winner.WorstCaseNet) == 0 &&
				candidate.ExpectedNet.Compare(winner.ExpectedNet) > 0) {
			t.Fatalf("winner=%#v was outranked by %#v", winner, candidate)
		}
	}
}
