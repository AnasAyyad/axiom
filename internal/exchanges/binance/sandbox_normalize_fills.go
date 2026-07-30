package binance

import (
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func normalizedSandboxEventKind(
	fills []execution.FillFact,
) sandbox.PrivateEventKind {
	if len(fills) != 0 {
		return sandbox.PrivateFillEvent
	}
	return sandbox.PrivateOrderEvent
}

func normalizeSandboxFills(
	body []byte,
	orderID string,
	submission sandbox.Submission,
) ([]execution.FillFact, []execution.FeeFact, string, time.Time, error) {
	if len(body) == 0 {
		return nil, nil, "", time.Time{}, nil
	}
	var native []sandboxFillPayload
	if strictDecode(body, &native) != nil {
		return nil, nil, "", time.Time{},
			sandboxPayloadFailure(errSandboxFillDecode)
	}
	sort.Slice(native, func(left, right int) bool {
		leftID, _ := strconv.ParseUint(native[left].ID.String(), 10, 64)
		rightID, _ := strconv.ParseUint(native[right].ID.String(), 10, 64)
		return leftID < rightID
	})
	fills := make([]execution.FillFact, 0, len(native))
	feeByAsset := make(map[domain.AssetSymbol]domain.Fee)
	latest := time.Time{}
	for _, item := range native {
		fill, asset, fee, at, err := normalizeSandboxFill(
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
				return nil, nil, "", time.Time{},
					sandboxPayloadFailure(errSandboxFillAggregate)
			}
			total = added
		}
		feeByAsset[asset] = total
		if at.After(latest) {
			latest = at
		}
	}
	fees := sandboxFeeFacts(feeByAsset)
	return fills, fees, hashBytes(body), latest, nil
}

func normalizeSandboxFill(
	item sandboxFillPayload,
	orderID string,
	submission sandbox.Submission,
) (
	execution.FillFact,
	domain.AssetSymbol,
	domain.Fee,
	time.Time,
	error,
) {
	if item.Symbol != submission.Instrument.Symbol() ||
		item.OrderID.String() != orderID || item.ID.String() == "" ||
		item.IsBuyer != (submission.Side == domain.SideBuy) || item.Time <= 0 {
		return execution.FillFact{}, "", domain.Fee{}, time.Time{},
			sandboxPayloadFailure(errSandboxFillIdentity)
	}
	quantity, quantityErr := domain.ParseQuantity(item.Quantity)
	price, priceErr := domain.ParsePrice(item.Price)
	fee, feeErr := domain.ParseFee(item.Commission)
	asset, assetErr := domain.ParseAssetSymbol(item.CommissionAsset)
	fillID, idErr := domain.NewVirtualFillID("binance-" + item.ID.String())
	ordinal, ordinalErr := strconv.ParseUint(item.ID.String(), 10, 64)
	if quantityErr != nil || priceErr != nil || feeErr != nil ||
		assetErr != nil || idErr != nil || ordinalErr != nil || ordinal == 0 {
		return execution.FillFact{}, "", domain.Fee{}, time.Time{},
			sandboxPayloadFailure(errSandboxFillValue)
	}
	zero, _ := domain.ParseFee("0")
	return execution.FillFact{
		ID: fillID, Quantity: quantity, Price: price, Fee: fee,
		Rebate: zero, FeeAsset: asset, Ordinal: ordinal,
	}, asset, fee, time.UnixMilli(item.Time).UTC(), nil
}

func sandboxFeeFacts(
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
