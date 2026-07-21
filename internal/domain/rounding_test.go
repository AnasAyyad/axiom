package domain

import "testing"

func TestExchangeRoundingDirections(t *testing.T) {
	step := mustQuantity(t, "0.01")
	buy, err := RoundBuyQuantity(mustQuantity(t, "1.239"), step)
	if err != nil || buy.String() != "1.23" {
		t.Fatalf("buy = %q, %v", buy.String(), err)
	}
	owned, _ := ParseBalance("1.234")
	sell, err := RoundSellQuantity(mustQuantity(t, "2"), owned, step)
	if err != nil || sell.String() != "1.23" {
		t.Fatalf("sell = %q, %v", sell.String(), err)
	}
	tick := mustPrice(t, "0.05")
	requested := mustPrice(t, "101.237")
	buyPrice, err := RoundLimitPrice(SideBuy, requested, tick)
	if err != nil || buyPrice.String() != "101.2" {
		t.Fatalf("buy price = %q, %v", buyPrice.String(), err)
	}
	sellPrice, err := RoundLimitPrice(SideSell, requested, tick)
	if err != nil || sellPrice.String() != "101.25" {
		t.Fatalf("sell price = %q, %v", sellPrice.String(), err)
	}
}

func TestMarketableLimitRoundingUsesSideSafeDirections(t *testing.T) {
	requested := mustPrice(t, "100.001")
	tick := mustPrice(t, "0.01")
	buy, err := RoundMarketableLimitPrice(SideBuy, requested, tick)
	if err != nil || buy.String() != "100.01" {
		t.Fatalf("marketable buy = %s %v", buy.String(), err)
	}
	sell, err := RoundMarketableLimitPrice(SideSell, requested, tick)
	if err != nil || sell.String() != "100" {
		t.Fatalf("marketable sell = %s %v", sell.String(), err)
	}
}

func TestNotionalAndFeeRounding(t *testing.T) {
	notional, err := CalculateNotional(mustPrice(t, "12.345"), mustQuantity(t, "2"), 2)
	if err != nil || notional.String() != "24.69" {
		t.Fatalf("notional = %q, %v", notional.String(), err)
	}
	rate, _ := ParseRate("0.00123")
	feeBase, _ := ParseNotional("10")
	fee, err := CalculateFee(feeBase, rate, 2)
	if err != nil || fee.String() != "0.02" {
		t.Fatalf("fee = %q, %v", fee.String(), err)
	}
}

func TestSellRoundingNeverExceedsInventory(t *testing.T) {
	step := mustQuantity(t, "0.001")
	for ownedUnits := int64(0); ownedUnits < 1000; ownedUnits++ {
		ownedText := "0." + threeDigits(ownedUnits)
		owned, _ := ParseBalance(ownedText)
		sell, err := RoundSellQuantity(mustQuantity(t, "10"), owned, step)
		if err != nil {
			t.Fatal(err)
		}
		if sell.Compare(Quantity(owned)) > 0 {
			t.Fatalf("sell %s exceeds owned %s", sell.String(), owned.String())
		}
	}
}

func threeDigits(value int64) string {
	if value < 10 {
		return "00" + string(rune('0'+value))
	}
	if value < 100 {
		return "0" + string(rune('0'+value/10)) + string(rune('0'+value%10))
	}
	return string(rune('0'+value/100)) + string(rune('0'+value/10%10)) + string(rune('0'+value%10))
}
