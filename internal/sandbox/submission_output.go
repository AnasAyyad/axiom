package sandbox

import (
	"axiom/internal/domain"
	"axiom/internal/execution"
)

// FilledOutput is the exact asset made available by one fully filled spot
// submission after fees and rebates charged in that same output asset.
type FilledOutput struct {
	Asset    domain.AssetSymbol
	Quantity domain.Balance
}

// FilledSubmissionOutput derives the only inventory that may activate a
// dependent sequential leg. It validates the immutable order identity and
// reconciles cumulative fee facts against the fill facts before exposing the
// output. Fees charged in a third asset remain accounted for but cannot alter
// this output asset's quantity.
func FilledSubmissionOutput(
	submission Submission,
	order execution.Order,
) (FilledOutput, error) {
	if !validFilledSubmissionIdentity(submission, order) {
		return FilledOutput{}, contractError("filled_submission_output_invalid")
	}
	totals, err := filledSubmissionTotals(submission, order)
	if err != nil || totals.filled.Compare(order.CumulativeQuantity) != 0 ||
		!sameOutputFeeTotals(order.Fees, totals.fees, totals.rebates) {
		return FilledOutput{}, contractError("filled_submission_output_invalid")
	}
	outputAsset := submission.Instrument.Quote
	if submission.Side == domain.SideBuy {
		outputAsset = submission.Instrument.Base
		totals.gross, err = domain.ParseBalance(order.CumulativeQuantity.String())
		if err != nil {
			return FilledOutput{}, contractError("filled_submission_output_invalid")
		}
	}
	grossWithRebate, err := totals.gross.Add(totals.rebates[outputAsset])
	if err != nil {
		return FilledOutput{}, contractError("filled_submission_output_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	net, err := grossWithRebate.Subtract(totals.fees[outputAsset])
	if err != nil || net.Compare(zero) <= 0 {
		return FilledOutput{}, contractError("filled_submission_output_invalid")
	}
	return FilledOutput{Asset: outputAsset, Quantity: net}, nil
}

func validFilledSubmissionIdentity(submission Submission, order execution.Order) bool {
	return order.State == execution.OrderFilled && len(order.Fills) > 0 &&
		order.Identity.ID == submission.OrderID && order.Identity.PlanID == submission.PlanID &&
		order.Identity.ClientOrderID == submission.ClientOrderID &&
		order.Identity.Instrument == submission.Instrument && order.Identity.Side == submission.Side &&
		order.Identity.Quantity.Compare(submission.Quantity) == 0 &&
		order.CumulativeQuantity.Compare(submission.Quantity) == 0
}

type filledOutputTotals struct {
	filled        domain.Quantity
	gross         domain.Balance
	fees, rebates map[domain.AssetSymbol]domain.Balance
}

func filledSubmissionTotals(submission Submission, order execution.Order) (filledOutputTotals, error) {
	filled, _ := domain.ParseQuantity("0")
	zeroBalance, _ := domain.ParseBalance("0")
	gross := zeroBalance
	feeTotals := make(map[domain.AssetSymbol]domain.Balance)
	rebateTotals := make(map[domain.AssetSymbol]domain.Balance)
	for _, fill := range order.Fills {
		var err error
		filled, err = filled.Add(fill.Quantity)
		if err != nil {
			return filledOutputTotals{}, contractError("filled_submission_output_invalid")
		}
		if submission.Side == domain.SideSell {
			notional, notionalErr := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
			value, valueErr := domain.ParseBalance(notional.String())
			if notionalErr != nil || valueErr != nil {
				return filledOutputTotals{}, contractError("filled_submission_output_invalid")
			}
			gross, err = gross.Add(value)
			if err != nil {
				return filledOutputTotals{}, contractError("filled_submission_output_invalid")
			}
		}
		fee, feeErr := domain.ParseBalance(fill.Fee.String())
		rebate, rebateErr := domain.ParseBalance(fill.Rebate.String())
		feeAssetValid := true
		if fee.Compare(zeroBalance) > 0 || rebate.Compare(zeroBalance) > 0 {
			_, feeAssetErr := domain.ParseAssetSymbol(string(fill.FeeAsset))
			feeAssetValid = feeAssetErr == nil
		}
		if feeErr != nil || rebateErr != nil ||
			!feeAssetValid ||
			addOutputTotal(feeTotals, fill.FeeAsset, fee) != nil ||
			addOutputTotal(rebateTotals, fill.FeeAsset, rebate) != nil {
			return filledOutputTotals{}, contractError("filled_submission_output_invalid")
		}
	}
	return filledOutputTotals{filled: filled, gross: gross, fees: feeTotals, rebates: rebateTotals}, nil
}

func addOutputTotal(
	totals map[domain.AssetSymbol]domain.Balance,
	asset domain.AssetSymbol,
	value domain.Balance,
) error {
	zero, _ := domain.ParseBalance("0")
	if value.Compare(zero) == 0 {
		return nil
	}
	total := totals[asset]
	if total.String() == "" {
		total = zero
	}
	next, err := total.Add(value)
	if err == nil {
		totals[asset] = next
	}
	return err
}

func sameOutputFeeTotals(
	facts []execution.FeeFact,
	fees map[domain.AssetSymbol]domain.Balance,
	rebates map[domain.AssetSymbol]domain.Balance,
) bool {
	seen := make(map[domain.AssetSymbol]struct{}, len(facts))
	zero, _ := domain.ParseBalance("0")
	for _, fact := range facts {
		if _, err := domain.ParseAssetSymbol(string(fact.Asset)); err != nil {
			return false
		}
		if _, duplicate := seen[fact.Asset]; duplicate {
			return false
		}
		seen[fact.Asset] = struct{}{}
		fee, feeErr := domain.ParseBalance(fact.Total.String())
		rebate, rebateErr := domain.ParseBalance(fact.Rebate.String())
		if feeErr != nil || rebateErr != nil ||
			fee.Compare(fees[fact.Asset]) != 0 ||
			rebate.Compare(rebates[fact.Asset]) != 0 {
			return false
		}
	}
	for asset, fee := range fees {
		if fee.Compare(zero) > 0 {
			if _, exists := seen[asset]; !exists {
				return false
			}
		}
	}
	for asset, rebate := range rebates {
		if rebate.Compare(zero) > 0 {
			if _, exists := seen[asset]; !exists {
				return false
			}
		}
	}
	return true
}
