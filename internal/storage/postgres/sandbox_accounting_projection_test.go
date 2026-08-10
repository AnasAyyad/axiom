package postgres

import (
	"testing"

	"axiom/internal/domain"
)

func TestSandboxAccountingProjectionUsesWeightedAverageCostAndNetQuoteFees(t *testing.T) {
	projection := newSandboxAccountingProjectionTestState(t)
	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideBuy,
		"1", "100", "100", "1", "0", "USDT")
	assertSandboxAccountingProjectionValues(t, projection, "1", "101", "101", "0",
		sandboxAccountingValuationComplete)

	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideSell,
		"0.4", "120", "48", "0.5", "0", "USDT")
	assertSandboxAccountingProjectionValues(t, projection, "0.6", "60.6", "101", "7.1",
		sandboxAccountingValuationComplete)

	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideSell,
		"0.6", "90", "54", "0", "0", "")
	assertSandboxAccountingProjectionValues(t, projection, "0", "0", "0", "0.5",
		sandboxAccountingValuationComplete)
	fees := projection.Fees[domain.AssetSymbol("USDT")]
	if fees.Fee.String() != "1.5" || fees.Rebate.String() != "0" {
		t.Fatalf("quote fee projection=%s/%s", fees.Fee.String(), fees.Rebate.String())
	}
}

func TestSandboxAccountingProjectionAdjustsBaseFeeInventory(t *testing.T) {
	projection := newSandboxAccountingProjectionTestState(t)
	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideBuy,
		"1", "100", "100", "0.01", "0", "BTC")
	assertSandboxAccountingProjectionValues(t, projection, "0.99", "100",
		"101.010101010101010101", "0", sandboxAccountingValuationComplete)
}

func TestSandboxAccountingProjectionMarksThirdAssetFeeUnvalued(t *testing.T) {
	projection := newSandboxAccountingProjectionTestState(t)
	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideBuy,
		"1", "100", "100", "0.01", "0", "BNB")
	assertSandboxAccountingProjectionValues(t, projection, "1", "100", "100", "0",
		sandboxAccountingValuationUnvaluedFee)
	fees := projection.Fees[domain.AssetSymbol("BNB")]
	if fees.Fee.String() != "0.01" || fees.Rebate.String() != "0" {
		t.Fatalf("third-asset fee projection=%s/%s", fees.Fee.String(), fees.Rebate.String())
	}
}

func TestSandboxAccountingProjectionRejectsOversell(t *testing.T) {
	projection := newSandboxAccountingProjectionTestState(t)
	applySandboxAccountingProjectionTestFill(t, &projection, domain.SideBuy,
		"1", "100", "100", "0", "0", "")
	err := applySandboxAccountingProjectionFill(&projection, sandboxAccountingProjectionFill{
		Side: domain.SideSell, Quantity: "1.1", Price: "100", Notional: "110",
		Fee: "0", Rebate: "0",
	})
	if err == nil || err.Error() != "sandbox_accounting_projection_oversell" {
		t.Fatalf("oversell error=%v", err)
	}
}

func TestSandboxTriangularAccountingTransfersUSDTBasisAcrossBTCAndETH(t *testing.T) {
	state := newSandboxTriangularAccountingState("BTC")
	applySandboxTriangularProjectionTestFill(t, &state, "BTCUSDT", domain.SideBuy,
		"0.0001", "10000", "1", "0.000001", "0", "USDT")
	applySandboxTriangularProjectionTestFill(t, &state, "ETHBTC", domain.SideBuy,
		"0.01", "0.01", "0.0001", "0", "0", "")
	applySandboxTriangularProjectionTestFill(t, &state, "ETHUSDT", domain.SideSell,
		"0.01", "100", "1", "0.000001", "0", "USDT")
	finalizeSandboxTriangularAccountingState(&state)
	btc := initializedSandboxTriangularLot(state.Lots["BTC"])
	eth := initializedSandboxTriangularLot(state.Lots["ETH"])
	if state.ValuationState != sandboxAccountingValuationComplete ||
		btc.Quantity.String() != "0" || btc.TotalCost.String() != "0" ||
		eth.Quantity.String() != "0" || eth.TotalCost.String() != "0" ||
		state.RealizedPnL.String() != "-0.000002" {
		t.Fatalf("triangular closed projection state=%s btc=%#v eth=%#v pnl=%s",
			state.ValuationState, btc, eth, state.RealizedPnL.String())
	}
	fees := state.Fees["USDT"]
	if fees.Fee.String() != "0.000002" || fees.Rebate.String() != "0" {
		t.Fatalf("triangular fees=%s/%s", fees.Fee.String(), fees.Rebate.String())
	}
}

func TestSandboxTriangularAccountingKeepsIncompleteOrUnfundedCycleFailClosed(t *testing.T) {
	state := newSandboxTriangularAccountingState("BTC")
	applySandboxTriangularProjectionTestFill(t, &state, "BTCUSDT", domain.SideBuy,
		"0.0001", "10000", "1", "0", "0", "")
	applySandboxTriangularProjectionTestFill(t, &state, "ETHBTC", domain.SideBuy,
		"0.01", "0.01", "0.0001", "0", "0", "")
	finalizeSandboxTriangularAccountingState(&state)
	if state.ValuationState != sandboxAccountingValuationCrossAsset {
		t.Fatalf("open cross-asset state=%s", state.ValuationState)
	}

	unfunded := newSandboxTriangularAccountingState("BTC")
	applySandboxTriangularProjectionTestFill(t, &unfunded, "BTCUSDT", domain.SideBuy,
		"0.0001", "10000", "1", "0", "0", "")
	applySandboxTriangularProjectionTestFill(t, &unfunded, "ETHBTC", domain.SideBuy,
		"0.01", "0.01", "0.0001", "0.000001", "0", "BTC")
	if unfunded.ValuationState != sandboxAccountingValuationUnresolved {
		t.Fatalf("unfunded quote-fee state=%s", unfunded.ValuationState)
	}
}

func newSandboxAccountingProjectionTestState(t *testing.T) sandboxAccountingPositionProjection {
	t.Helper()
	base, quote, err := sandboxAccountingInstrumentAssets("BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	quantity, _ := domain.ParseBalance("0")
	cost, _ := domain.ParseMoney("0")
	average, _ := domain.ParsePrice("0")
	pnl, _ := domain.ParsePnL("0")
	return sandboxAccountingPositionProjection{
		Instrument: "BTCUSDT", BaseAsset: base, QuoteAsset: quote,
		Quantity: quantity, TotalCost: cost, WeightedAverageCost: average,
		RealizedPnL: pnl, ValuationState: sandboxAccountingValuationComplete,
		Fees: make(map[domain.AssetSymbol]sandboxAccountingProjectionFee),
	}
}

func applySandboxAccountingProjectionTestFill(
	t *testing.T,
	projection *sandboxAccountingPositionProjection,
	side domain.Side,
	quantity, price, notional, fee, rebate string,
	feeAsset domain.AssetSymbol,
) {
	t.Helper()
	if err := applySandboxAccountingProjectionFill(projection, sandboxAccountingProjectionFill{
		Side: side, Quantity: quantity, Price: price, Notional: notional,
		Fee: fee, Rebate: rebate, FeeAsset: feeAsset,
	}); err != nil {
		t.Fatal(err)
	}
}

func applySandboxTriangularProjectionTestFill(
	t *testing.T,
	state *sandboxTriangularAccountingState,
	instrument string,
	side domain.Side,
	quantity, price, notional, fee, rebate string,
	feeAsset domain.AssetSymbol,
) {
	t.Helper()
	if err := applySandboxTriangularAccountingFill(state, sandboxAccountingProjectionFill{
		Instrument: instrument, Side: side, Quantity: quantity, Price: price,
		Notional: notional, Fee: fee, Rebate: rebate, FeeAsset: feeAsset,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxAccountingProjectionValues(
	t *testing.T,
	projection sandboxAccountingPositionProjection,
	quantity, totalCost, averageCost, realizedPnL, valuationState string,
) {
	t.Helper()
	if projection.Quantity.String() != quantity || projection.TotalCost.String() != totalCost ||
		projection.WeightedAverageCost.String() != averageCost ||
		projection.RealizedPnL.String() != realizedPnL || projection.ValuationState != valuationState {
		t.Fatalf("projection quantity=%s cost=%s average=%s pnl=%s valuation=%s",
			projection.Quantity.String(), projection.TotalCost.String(),
			projection.WeightedAverageCost.String(), projection.RealizedPnL.String(),
			projection.ValuationState)
	}
}
