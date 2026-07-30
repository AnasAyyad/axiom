package bybit

import (
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func normalizeDemoExecutions(
	native []demoExecutionPayload,
	orderID string,
	submission sandbox.Submission,
) ([]execution.FillFact, []execution.FeeFact, string, time.Time, error) {
	sort.Slice(native, func(left, right int) bool {
		if native[left].Sequence != native[right].Sequence {
			return native[left].Sequence < native[right].Sequence
		}
		return native[left].ExecutionID < native[right].ExecutionID
	})
	fills := make([]execution.FillFact, 0, len(native))
	feeByAsset := make(map[domain.AssetSymbol]domain.Fee)
	seen := make(map[string]struct{}, len(native))
	latest := time.Time{}
	for _, item := range native {
		if _, duplicate := seen[item.ExecutionID]; duplicate {
			continue
		}
		seen[item.ExecutionID] = struct{}{}
		fill, asset, fee, at, err := normalizeDemoExecution(
			item, orderID, submission,
		)
		if err != nil {
			return nil, nil, "", time.Time{}, err
		}
		fills = append(fills, fill)
		total := fee
		if prior, exists := feeByAsset[asset]; exists {
			added, addErr := prior.Add(fee)
			if addErr != nil {
				return nil, nil, "", time.Time{}, ErrDemoPayload
			}
			total = added
		}
		feeByAsset[asset] = total
		if at.After(latest) {
			latest = at
		}
	}
	return fills, demoFeeFacts(feeByAsset),
		canonicalDemoHash(native), latest, nil
}

func normalizeDemoExecution(
	item demoExecutionPayload,
	orderID string,
	submission sandbox.Submission,
) (
	execution.FillFact,
	domain.AssetSymbol,
	domain.Fee,
	time.Time,
	error,
) {
	if item.Category != "spot" ||
		item.Symbol != submission.Instrument.Symbol() ||
		item.OrderID != orderID ||
		item.OrderLinkID != submission.ClientOrderID ||
		item.ExecutionID == "" || item.ExecutionType != "Trade" ||
		item.Side != demoSide(submission.Side) ||
		item.OrderType != "Limit" ||
		(item.IsLeverage != "" && item.IsLeverage != "0") ||
		(item.MarketUnit != "" && item.MarketUnit != "baseCoin") ||
		(item.ExecutionFeeV2 != "" &&
			item.ExecutionFeeV2 != item.ExecutionFee) {
		return execution.FillFact{}, "", domain.Fee{}, time.Time{},
			ErrDemoPayload
	}
	quantity, quantityErr := domain.ParseQuantity(item.ExecutionQty)
	price, priceErr := domain.ParsePrice(item.ExecutionPrice)
	fee, feeErr := domain.ParseFee(item.ExecutionFee)
	asset, assetErr := domain.ParseAssetSymbol(item.FeeCurrency)
	atMillis, atErr := strconv.ParseInt(item.ExecutionTime, 10, 64)
	fillID, idErr := domain.NewVirtualFillID(
		"bybit-" + hashString(item.ExecutionID)[:32],
	)
	if quantityErr != nil || priceErr != nil || feeErr != nil ||
		assetErr != nil || atErr != nil || atMillis <= 0 || idErr != nil {
		return execution.FillFact{}, "", domain.Fee{}, time.Time{},
			ErrDemoPayload
	}
	ordinal := uint64(atMillis)
	if item.Sequence > 0 {
		ordinal = uint64(item.Sequence)
	}
	zero, _ := domain.ParseFee("0")
	return execution.FillFact{
		ID: fillID, Quantity: quantity, Price: price, Fee: fee,
		Rebate: zero, FeeAsset: asset, Ordinal: ordinal,
	}, asset, fee, time.UnixMilli(atMillis).UTC(), nil
}

func demoFeeFacts(
	feeByAsset map[domain.AssetSymbol]domain.Fee,
) []execution.FeeFact {
	fees := make([]execution.FeeFact, 0, len(feeByAsset))
	assets := make([]string, 0, len(feeByAsset))
	for asset := range feeByAsset {
		assets = append(assets, string(asset))
	}
	sort.Strings(assets)
	for _, text := range assets {
		asset := domain.AssetSymbol(text)
		zero, _ := domain.ParseFee("0")
		fees = append(fees, execution.FeeFact{
			Asset: asset, Total: feeByAsset[asset], Rebate: zero,
		})
	}
	return fees
}

func sumDemoFillQuantity(fills []execution.FillFact) domain.Quantity {
	total, _ := domain.ParseQuantity("0")
	for _, fill := range fills {
		total, _ = total.Add(fill.Quantity)
	}
	return total
}
