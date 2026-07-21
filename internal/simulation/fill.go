package simulation

import (
	"fmt"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
)

type simulatedFill struct {
	Quantity domain.Quantity
	Price    domain.Price
	Notional domain.Notional
}

func filterQuantity(
	leg execution.PlannedLeg,
	owned domain.Balance,
	metadata domain.InstrumentMetadata,
) (domain.Quantity, error) {
	if metadata.Validate() != nil || metadata.Instrument != leg.Instrument {
		return domain.Quantity{}, simulationError("metadata_invalid")
	}
	roundedPrice, err := domain.RoundLimitPrice(leg.Side, leg.LimitPrice, metadata.PriceTick)
	if err != nil || roundedPrice.Compare(leg.LimitPrice) != 0 {
		return domain.Quantity{}, simulationError("limit_filter_invalid")
	}
	var quantity domain.Quantity
	if leg.Side == domain.SideBuy {
		quantity, err = domain.RoundBuyQuantity(leg.Quantity, metadata.QuantityStep)
	} else if leg.Side == domain.SideSell {
		quantity, err = domain.RoundSellQuantity(leg.Quantity, owned, metadata.QuantityStep)
	} else {
		return domain.Quantity{}, simulationError("side_invalid")
	}
	if err != nil || quantity.Compare(metadata.MinimumQuantity) < 0 {
		return domain.Quantity{}, simulationError("quantity_filter_invalid")
	}
	notional, err := domain.CalculateNotional(leg.LimitPrice, quantity, 8)
	if err != nil || notional.Compare(metadata.MinimumNotional) < 0 {
		return domain.Quantity{}, simulationError("notional_filter_invalid")
	}
	return quantity, nil
}

func (broker *SimulatedBroker) consume(
	namespace string,
	leg execution.PlannedLeg,
	state BookState,
	maximum domain.Quantity,
) (simulatedFill, error) {
	levels := state.Asks
	if leg.Side == domain.SideSell {
		levels = state.Bids
	}
	zero, _ := domain.ParseQuantity("0")
	filled := zero
	total, _ := domain.ParseNotional("0")
	for _, level := range levels {
		remaining, err := maximum.Subtract(filled)
		if err != nil || remaining.String() == "0" {
			break
		}
		effective, err := broker.models.Price.Apply(level.Price, leg.Side)
		if err != nil || !insideLimit(effective, leg.LimitPrice, leg.Side) {
			break
		}
		claimed, err := broker.liquidity.Consume(liquidityLevelKey(namespace, leg, state, level), level.Quantity, remaining)
		if err != nil {
			return simulatedFill{}, err
		}
		if claimed.String() == "0" {
			continue
		}
		notional, err := domain.CalculateNotional(effective, claimed, 8)
		if err != nil {
			return simulatedFill{}, simulationError("fill_notional_invalid")
		}
		filled, err = filled.Add(claimed)
		if err == nil {
			total, err = total.Add(notional)
		}
		if err != nil {
			return simulatedFill{}, simulationError("fill_overflow")
		}
	}
	if filled.String() == "0" {
		return simulatedFill{Quantity: filled, Notional: total}, nil
	}
	vwap, err := domain.CalculateVWAP(total, filled, 8)
	if err != nil {
		return simulatedFill{}, simulationError("fill_vwap_invalid")
	}
	return simulatedFill{Quantity: filled, Price: vwap, Notional: total}, nil
}

func liquidityLevelKey(
	namespace string,
	leg execution.PlannedLeg,
	state BookState,
	level exchangecontracts.PriceLevel,
) LiquidityKey {
	return LiquidityKey{Namespace: namespace, Exchange: state.Exchange, Instrument: state.Instrument,
		MarketVersion: state.Version, Side: leg.Side, Price: level.Price.String()}
}

func insideLimit(price, limit domain.Price, side domain.Side) bool {
	if side == domain.SideBuy {
		return price.Compare(limit) <= 0
	}
	return side == domain.SideSell && price.Compare(limit) >= 0
}

func terminalWithoutFill(leg execution.PlannedLeg, state execution.OrderState, logical uint64) []execution.OrderEvent {
	zero, _ := domain.ParseQuantity("0")
	return []execution.OrderEvent{{ID: fmt.Sprintf("%s-%s", leg.OrderID.Value(), state), OrderID: leg.OrderID,
		ClientOrderID: leg.ClientOrderID, State: state, ExchangeStatus: string(state),
		CumulativeQuantity: zero, OccurredAt: eventTime(logical), Ordinal: logical}}
}
