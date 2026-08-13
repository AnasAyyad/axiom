package crossarb

import "testing"

func TestSingleInstrumentInventoryInitializationIsSeparateAndVersioned(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	balances, evidence, err := InitializeSingleInstrumentInventory(input.Markets, balance("250"))
	if err != nil || len(balances) != 2 || len(evidence) != 2 {
		t.Fatalf("balances=%#v evidence=%#v error=%v", balances, evidence, err)
	}
	for _, item := range evidence {
		if item.ModelVersion != SingleInstrumentInventoryModel || item.TargetBaseValue.String() != "62.5" ||
			item.BaseQuantity.String() == "0" || item.AvailableUSDT.String() == "0" ||
			balances[item.Exchange][asset("BTC")].Compare(item.BaseQuantity) != 0 {
			t.Fatalf("initialization=%#v", item)
		}
	}
}
