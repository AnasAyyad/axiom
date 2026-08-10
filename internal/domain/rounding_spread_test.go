package domain

import "testing"

func TestCalculateRelativeSpreadIsExactAndConservative(t *testing.T) {
	bid, _ := ParsePrice("100")
	ask, _ := ParsePrice("100.1")
	spread, err := CalculateRelativeSpread(bid, ask, 18)
	if err != nil || spread.String() != "0.001" {
		t.Fatalf("spread=%s error=%v", spread.String(), err)
	}
	if _, err = CalculateRelativeSpread(ask, bid, 18); err == nil {
		t.Fatal("crossed book spread accepted")
	}
}

func TestCalculateConservativePercentRoundsRiskUsageUp(t *testing.T) {
	numerator, _ := ParseMoney("1")
	denominator, _ := ParseMoney("3")
	percentage, err := CalculateConservativePercent(numerator, denominator, 18)
	if err != nil || percentage.String() != "0.333333333333333334" {
		t.Fatalf("percentage=%s error=%v", percentage.String(), err)
	}
	if _, err = CalculateConservativePercent(numerator, mustMoneyForRoundingTest(t, "0"), 18); err == nil {
		t.Fatal("zero risk denominator accepted")
	}
}

func TestCalculateReciprocalPriceFloorNeverOverstatesFeeAssetMark(t *testing.T) {
	value, _ := ParsePrice("3")
	mark, err := CalculateReciprocalPriceFloor(value, 18)
	if err != nil || mark.String() != "0.333333333333333333" {
		t.Fatalf("reciprocal mark=%s error=%v", mark, err)
	}
	quantity, _ := ParseQuantity(mark.String())
	product, err := CalculateNotional(value, quantity, 18)
	one, _ := ParseNotional("1")
	if err != nil || product.Compare(one) > 0 {
		t.Fatalf("floored reciprocal product=%s error=%v", product, err)
	}
	zero, _ := ParsePrice("0")
	if _, err = CalculateReciprocalPriceFloor(zero, 18); err == nil {
		t.Fatal("zero reciprocal price accepted")
	}
}

func TestScaleBalanceCeilingNeverUnderstatesReserve(t *testing.T) {
	balance, err := ParseBalance("1")
	if err != nil {
		t.Fatal(err)
	}
	fraction, err := ParsePercent("0.333333333333333333")
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := ScaleBalanceCeiling(balance, fraction, 6)
	if err != nil || reserve.String() != "0.333334" {
		t.Fatalf("reserve=%s error=%v", reserve.String(), err)
	}
	over, _ := ParsePercent("1.000000000000000001")
	if _, err = ScaleBalanceCeiling(balance, over, 18); err == nil {
		t.Fatal("fraction above one accepted")
	}
}

func TestScaleBalanceFloorNeverExceedsExecutableCap(t *testing.T) {
	balance, err := ParseBalance("10")
	if err != nil {
		t.Fatal(err)
	}
	fraction, err := ParsePercent("0.333333333333333333")
	if err != nil {
		t.Fatal(err)
	}
	value, err := ScaleBalanceFloor(balance, fraction, 6)
	if err != nil || value.String() != "3.333333" {
		t.Fatalf("value=%s error=%v", value.String(), err)
	}
}

func mustMoneyForRoundingTest(t *testing.T, value string) Money {
	t.Helper()
	parsed, err := ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
