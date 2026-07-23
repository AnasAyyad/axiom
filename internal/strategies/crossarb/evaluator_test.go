package crossarb

import (
	"strings"
	"testing"
)

func TestEvaluateBothDirectionsBTCAndETHWithClosedCycleEconomics(t *testing.T) {
	btc := evaluationFixture(t, "BTC", false)
	eth := evaluationFixture(t, "ETH", true)
	candidates, err := EvaluateUniverse([]EvaluationInput{eth, btc})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 ||
		candidates[0].Direction != BuyBinanceSellBybit ||
		candidates[1].Direction != BuyBybitSellBinance {
		t.Fatalf("candidates = %#v", candidates)
	}
	for _, candidate := range candidates {
		zero := money("0")
		if candidate.CoherentViewID == "" || len(candidate.ViewMembers) != 2 ||
			len(candidate.Claims) != 7 || candidate.Inventory.State != BandPreferred ||
			!candidate.Inventory.NaturalReverse ||
			candidate.Economics.MarginalInventoryReplacement.Compare(zero) <= 0 ||
			candidate.Economics.ExchangeConcentrationPenalty.Compare(zero) <= 0 ||
			candidate.Economics.USDTVenueConcentrationPenalty.Compare(zero) <= 0 ||
			candidate.Economics.ExpectedClosedCycleProfit.String()[0] == '-' ||
			candidate.Economics.WorstClosedCycleProfit.String()[0] == '-' {
			t.Fatalf("incomplete candidate = %#v", candidate)
		}
	}
}

func TestEvaluateRejectsFalseImmediateSpreadAndMismatchedCoherentBook(t *testing.T) {
	input := evaluationFixture(t, "BTC", false)
	input.Restoration.MarginalInventoryReplacement = money("100")
	if _, err := Evaluate(input); err == nil || !strings.Contains(err.Error(), "no_eligible_direction") {
		t.Fatalf("restoration-cost false profit accepted: %v", err)
	}

	input = evaluationFixture(t, "BTC", false)
	input.Markets[0] = testMarket(t, "binance", input.Markets[0].Book.Instrument(), "99", "100", 3)
	if _, err := Evaluate(input); err == nil || !strings.Contains(err.Error(), "coherent_member_mismatch") {
		t.Fatalf("mismatched executable book accepted: %v", err)
	}
}

func TestInventoryBandsExactThirtyFiftySeventy(t *testing.T) {
	configuration := DefaultConfiguration()
	tests := []struct {
		owned string
		want  BandState
	}{
		{"30", BandPaused},
		{"30.000001", BandReduced},
		{"49.999999", BandReduced},
		{"50", BandNormal},
		{"70", BandNormal},
		{"70.000001", BandPreferred},
	}
	for _, test := range tests {
		t.Run(test.owned, func(t *testing.T) {
			buy := testInventory("binance", asset("BTC"), "50")
			sell := testInventory("bybit", asset("BTC"), test.owned)
			control, err := inventoryControl(buy, sell, configuration)
			if err != nil || control.State != test.want {
				t.Fatalf("control = %#v, %v; want %s", control, err, test.want)
			}
		})
	}
}

func TestEvaluatePermutationAndRunDeterminism(t *testing.T) {
	input := evaluationFixture(t, "BTC", false)
	first, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Markets[0], input.Markets[1] = input.Markets[1], input.Markets[0]
	input.Inventory[0], input.Inventory[1] = input.Inventory[1], input.Inventory[0]
	second, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("permutation changed candidate identity: %s != %s", first[0].ID, second[0].ID)
	}
}
