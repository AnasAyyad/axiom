package triangular

import (
	"strings"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
)

// NewRecordedSagaClaimSet constructs the atomic claim capacity for one
// canonical decision from its recorded portfolio and full-depth market facts.
// It deliberately derives capacities before candidate evaluation, so a
// candidate cannot grant itself balance, fee, recovery, or book liquidity.
// The resulting set is input-scoped; a durable worker must checkpoint it
// before using it across recovery boundaries.
func NewRecordedSagaClaimSet(input Input, owner string) (*portfolio.AtomicClaimSet, error) {
	evaluation, err := input.EvaluationInput()
	if err != nil || owner == "" {
		return nil, strategyError("recorded_claims_input_invalid")
	}
	set := portfolio.NewAtomicClaimSet()
	if err = openRecordedClaim(set, portfolio.ClaimBalance, owner, evaluation.Exchange, "usdt", evaluation.AvailableSettlement); err != nil {
		return nil, err
	}
	if err = openRecordedClaim(set, portfolio.ClaimRecovery, owner, evaluation.Exchange, "usdt", evaluation.RecoveryAllowance); err != nil {
		return nil, err
	}
	for asset, amount := range evaluation.FeeBalances {
		if err = openRecordedClaim(set, portfolio.ClaimFeeBuffer, owner, evaluation.Exchange, string(asset), amount); err != nil {
			return nil, err
		}
	}
	for _, market := range evaluation.Markets {
		instrument := market.Book.Instrument()
		version := uintString(market.Book.Version())
		for _, side := range []struct {
			name   string
			levels []exchangecontracts.PriceLevel
		}{{"sell", market.Book.Bids()}, {"buy", market.Book.Asks()}} {
			capacity, capacityErr := recordedDepthCapacity(side.levels)
			if capacityErr != nil || openRecordedClaim(set, portfolio.ClaimLiquidity, owner, evaluation.Exchange,
				strings.ToLower(instrument.Symbol()+"/"+side.name+"/v"+version), capacity) != nil {
				return nil, strategyError("recorded_claims_input_invalid")
			}
		}
	}
	return set, nil
}

func openRecordedClaim(
	set *portfolio.AtomicClaimSet,
	kind portfolio.ClaimKind,
	owner, exchange, resource string,
	amount domain.Balance,
) error {
	zero, _ := domain.ParseBalance("0")
	if set == nil || amount.Compare(zero) <= 0 || set.OpenResource(portfolio.ClaimKey{Kind: kind,
		Owner: strings.ToLower(owner), Exchange: strings.ToLower(exchange), Resource: strings.ToLower(resource)}, amount) != nil {
		return strategyError("recorded_claims_input_invalid")
	}
	return nil
}

func recordedDepthCapacity(levels []exchangecontracts.PriceLevel) (domain.Balance, error) {
	total, _ := domain.ParseBalance("0")
	for _, level := range levels {
		amount, err := domain.ParseBalance(level.Quantity.String())
		if err != nil {
			return domain.Balance{}, strategyError("recorded_claims_input_invalid")
		}
		total, err = total.Add(amount)
		if err != nil {
			return domain.Balance{}, strategyError("recorded_claims_input_invalid")
		}
	}
	return total, nil
}
