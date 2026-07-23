package crossarb

import (
	"axiom/internal/domain"
	"axiom/internal/execution"
)

func classifyOutcome(legs []LegSimulation) SimulationOutcome {
	buy, sell := legs[0], legs[1]
	if buy.FinalState == execution.OrderUnknown || sell.FinalState == execution.OrderUnknown {
		return OutcomeDelayedUnknown
	}
	buyFilled, sellFilled := buy.Result != nil, sell.Result != nil
	buyPartial := buy.FinalState == execution.OrderPartiallyFilled
	sellPartial := sell.FinalState == execution.OrderPartiallyFilled
	switch {
	case buy.FinalState == execution.OrderFilled && sell.FinalState == execution.OrderFilled:
		return OutcomeBothFilled
	case buyPartial && sellPartial:
		return OutcomePartialBoth
	case buyPartial:
		return OutcomePartialBuy
	case sellPartial:
		return OutcomePartialSell
	case buyFilled:
		return OutcomeBuyOnly
	case sellFilled:
		return OutcomeSellOnly
	default:
		return OutcomeBothMissed
	}
}

func venueExposures(candidate Candidate, legs []LegSimulation) []VenueExposure {
	exposures := make([]VenueExposure, 0, 2)
	if legs[0].Result != nil {
		exposures = append(exposures, VenueExposure{
			Exchange: candidate.BuyExchange, Asset: candidate.Instrument.Base,
			Kind: "base_acquired", Quantity: balance(legs[0].Result.NetOutput.String()),
		})
	}
	if legs[1].Result != nil {
		removed, err := legs[1].Result.Input.Subtract(legs[1].Result.SourceDust)
		if err == nil {
			exposures = append(exposures, VenueExposure{
				Exchange: candidate.SellExchange, Asset: candidate.Instrument.Base,
				Kind: "base_depleted", Quantity: balance(removed.String()),
			})
		}
	}
	return exposures
}

func actualUSDTNet(legs []LegSimulation) domain.PnL {
	buySpent := quantity("0")
	sellProceeds := quantity("0")
	if legs[0].Result != nil {
		buySpent, _ = legs[0].Result.Input.Subtract(legs[0].Result.SourceDust)
	}
	if legs[1].Result != nil {
		sellProceeds = legs[1].Result.NetOutput
	}
	value, err := quantityDifference(sellProceeds, buySpent)
	if err != nil {
		value, _ = domain.ParsePnL("0")
	}
	return value
}

func requiresRecovery(result SimulationResult) bool {
	return result.Outcome != OutcomeBothFilled && result.Outcome != OutcomeBothMissed &&
		result.Outcome != OutcomeNegativeBeforeArrival
}
