package execution

import (
	"encoding/json"

	"axiom/internal/domain"
)

func (reducer *OrderReducer) validateFacts(event OrderEvent) error {
	seenFees := make(map[domain.AssetSymbol]struct{}, len(event.Fees))
	for _, fee := range event.Fees {
		if _, exists := seenFees[fee.Asset]; exists {
			return executionError("fee_asset_duplicate")
		}
		seenFees[fee.Asset] = struct{}{}
		if prior, exists := feeByAsset(reducer.order.Fees, fee.Asset); exists &&
			(fee.Total.Compare(prior.Total) < 0 || fee.Rebate.Compare(prior.Rebate) < 0) {
			return executionError("cumulative_fee_decreased")
		}
	}
	for _, prior := range reducer.order.Fees {
		if _, exists := feeByAsset(event.Fees, prior.Asset); !exists {
			return executionError("cumulative_fee_decreased")
		}
	}
	seenFills := make(map[string]struct{}, len(event.Fills))
	for _, fill := range event.Fills {
		if fill.ID.Value() == "" || fill.Ordinal == 0 || fill.Quantity.String() == "0" || fill.Price.String() == "0" {
			return executionError("fill_invalid")
		}
		if _, duplicate := seenFills[fill.ID.String()]; duplicate {
			return executionError("fill_duplicate_in_event")
		}
		seenFills[fill.ID.String()] = struct{}{}
		if prior, exists := reducer.fills[fill.ID.String()]; exists && !sameFill(prior, fill) {
			return executionError("fill_identity_conflict")
		}
	}
	return reducer.validateCumulativeFills(event)
}

func (reducer *OrderReducer) validateCumulativeFills(event OrderEvent) error {
	zero, _ := domain.ParseQuantity("0")
	total := zero
	combined := make(map[string]FillFact, len(reducer.fills)+len(event.Fills))
	for identity, fill := range reducer.fills {
		combined[identity] = fill
	}
	for _, fill := range event.Fills {
		combined[fill.ID.String()] = fill
	}
	for _, fill := range combined {
		var err error
		total, err = total.Add(fill.Quantity)
		if err != nil {
			return executionError("fill_quantity_overflow")
		}
	}
	if total.Compare(event.CumulativeQuantity) != 0 {
		return executionError("cumulative_fill_mismatch")
	}
	return nil
}

func (reducer *OrderReducer) factsChanged(event OrderEvent) bool {
	if event.CumulativeQuantity.Compare(reducer.order.CumulativeQuantity) > 0 ||
		event.ExchangeStatus != reducer.order.ExchangeStatus || len(event.Fees) != len(reducer.order.Fees) {
		return true
	}
	for _, fee := range event.Fees {
		prior, exists := feeByAsset(reducer.order.Fees, fee.Asset)
		if !exists || fee.Total.Compare(prior.Total) != 0 || fee.Rebate.Compare(prior.Rebate) != 0 {
			return true
		}
	}
	for _, fill := range event.Fills {
		if _, exists := reducer.fills[fill.ID.String()]; !exists {
			return true
		}
	}
	return false
}

func (reducer *OrderReducer) apply(event OrderEvent, hash string) {
	reducer.order.State = event.State
	reducer.order.ExchangeStatus = event.ExchangeStatus
	reducer.order.CumulativeQuantity = event.CumulativeQuantity
	reducer.order.Fees = append([]FeeFact(nil), event.Fees...)
	for _, fill := range event.Fills {
		if _, exists := reducer.fills[fill.ID.String()]; exists {
			continue
		}
		reducer.fills[fill.ID.String()] = fill
		reducer.order.Fills = append(reducer.order.Fills, fill)
	}
	reducer.order.Revision++
	reducer.events[event.ID] = hash
	if event.Ordinal > reducer.lastOrdinal {
		reducer.lastOrdinal = event.Ordinal
	}
}

func feeByAsset(fees []FeeFact, asset domain.AssetSymbol) (FeeFact, bool) {
	for _, fee := range fees {
		if fee.Asset == asset {
			return fee, true
		}
	}
	return FeeFact{}, false
}

func errorCode(err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return "order_invariant_failed"
	}
	return failure.Code
}

func sameFill(left, right FillFact) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
