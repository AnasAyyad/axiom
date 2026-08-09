package sandbox

import (
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestFilledSubmissionOutputUsesNetAssetProducedBySpotFill(t *testing.T) {
	tests := []struct {
		name      string
		side      domain.Side
		feeAsset  domain.AssetSymbol
		fee       string
		rebate    string
		wantAsset domain.AssetSymbol
		want      string
	}{
		{name: "buy base fee", side: domain.SideBuy, feeAsset: "BTC", fee: "0.01", rebate: "0.002", wantAsset: "BTC", want: "0.992"},
		{name: "sell quote fee", side: domain.SideSell, feeAsset: "USDT", fee: "0.1", rebate: "0.02", wantAsset: "USDT", want: "99.92"},
		{name: "third asset fee", side: domain.SideBuy, feeAsset: "ETH", fee: "0.01", rebate: "0", wantAsset: "BTC", want: "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			submission, order := filledOutputFixture(t, test.side, test.feeAsset, test.fee, test.rebate)
			output, err := FilledSubmissionOutput(submission, order)
			if err != nil || output.Asset != test.wantAsset || output.Quantity.String() != test.want {
				t.Fatalf("output=%#v want=%s/%s error=%v", output, test.wantAsset, test.want, err)
			}
		})
	}
}

func TestFilledSubmissionOutputRejectsUnreconciledFacts(t *testing.T) {
	submission, order := filledOutputFixture(t, domain.SideBuy, "BTC", "0.01", "0")
	wrong, _ := domain.ParseFee("0.02")
	order.Fees[0].Total = wrong
	if _, err := FilledSubmissionOutput(submission, order); err == nil {
		t.Fatal("cumulative fee mismatch exposed dependent inventory")
	}
	order = filledOutputFixtureOrder(t, submission, "BTC", "0.01", "0")
	otherPlan, _ := domain.NewExecutionPlanID("other-output-plan")
	order.Identity.PlanID = otherPlan
	if _, err := FilledSubmissionOutput(submission, order); err == nil {
		t.Fatal("immutable order identity mismatch exposed dependent inventory")
	}
}

func filledOutputFixture(
	t *testing.T,
	side domain.Side,
	feeAsset domain.AssetSymbol,
	feeText, rebateText string,
) (Submission, execution.Order) {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("filled-output-plan")
	orderID, _ := domain.NewVirtualOrderID("filled-output-order")
	strategyID, _ := domain.NewStrategyID(StrategyTriangular)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	price, _ := domain.ParsePrice("100")
	notional, _ := domain.CalculateNotional(price, quantity, 18)
	submission := Submission{PlanID: planID, OrderID: orderID,
		AccountID: "binance-output", AccountEpoch: 1,
		ClientOrderID: "ax-filled-output", StrategyID: strategyID,
		Instrument: instrument, Side: side, Quantity: quantity, LimitPrice: price,
		Notional: notional, Style: OrderStyleLimitIOC, Action: IntentEntry,
		RequestHash: testHash("filled-output-request"),
		PolicyHash:  testHash("filled-output-policy"),
		ApprovedAt:  time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)}
	return submission, filledOutputFixtureOrder(t, submission, feeAsset, feeText, rebateText)
}

func filledOutputFixtureOrder(
	t *testing.T,
	submission Submission,
	feeAsset domain.AssetSymbol,
	feeText, rebateText string,
) execution.Order {
	t.Helper()
	fillID, _ := domain.NewVirtualFillID("filled-output-fill")
	fee, _ := domain.ParseFee(feeText)
	rebate, _ := domain.ParseFee(rebateText)
	fill := execution.FillFact{ID: fillID, Quantity: submission.Quantity,
		Price: submission.LimitPrice, Fee: fee, Rebate: rebate,
		FeeAsset: feeAsset, Ordinal: 1}
	fees := []execution.FeeFact(nil)
	zero, _ := domain.ParseFee("0")
	if fee.Compare(zero) > 0 || rebate.Compare(zero) > 0 {
		fees = []execution.FeeFact{{Asset: feeAsset, Total: fee, Rebate: rebate}}
	}
	return execution.Order{Identity: execution.OrderIdentity{
		ID: submission.OrderID, PlanID: submission.PlanID,
		ClientOrderID: submission.ClientOrderID, Instrument: submission.Instrument,
		Side: submission.Side, Quantity: submission.Quantity,
	}, State: execution.OrderFilled, ExchangeStatus: "FILLED",
		CumulativeQuantity: submission.Quantity, Fees: fees,
		Fills: []execution.FillFact{fill}, Revision: 2}
}
