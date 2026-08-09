package crossarb

import (
	"strings"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
)

// NewRecordedSagaClaimSet constructs the atomic claim capacity for one
// canonical decision from its recorded ownership, fee, recovery, and
// full-depth market facts. It deliberately derives capacities before candidate
// evaluation, so a candidate cannot grant itself a two-venue resource.
// The resulting set is input-scoped; a durable worker must checkpoint it
// before using it across recovery boundaries.
func NewRecordedSagaClaimSet(input Input) (*portfolio.AtomicClaimSet, error) {
	evaluation, err := input.EvaluationInput()
	if err != nil || len(evaluation.Inventory) == 0 {
		return nil, strategyError("recorded_claims_input_invalid")
	}
	set := portfolio.NewAtomicClaimSet()
	owner := ""
	for _, inventory := range evaluation.Inventory {
		if owner == "" {
			owner = inventory.Owner
		}
		if inventory.Owner != owner || inventory.Exchange == "" || inventory.BaseAsset == "" {
			return nil, strategyError("recorded_claims_input_invalid")
		}
		if err = openPositiveRecordedClaim(set, portfolio.ClaimBalance, owner, inventory.Exchange, "usdt", inventory.OwnedUSDT); err != nil {
			return nil, err
		}
		if err = openPositiveRecordedClaim(set, portfolio.ClaimBalance, owner, inventory.Exchange, string(inventory.BaseAsset), inventory.OwnedBase); err != nil {
			return nil, err
		}
	}
	if err = openRecordedClaim(set, portfolio.ClaimRecovery, owner, "portfolio", "cross-exchange-usdt",
		domainBalance(input.Restoration.RecoveryAllowance)); err != nil {
		return nil, err
	}
	for key, amount := range evaluation.FeeBalances {
		exchange, asset, found := strings.Cut(key, ":")
		if !found || exchange == "" || asset == "" || openPositiveRecordedClaim(set, portfolio.ClaimFeeBuffer, owner, exchange, asset, amount) != nil {
			return nil, strategyError("recorded_claims_input_invalid")
		}
	}
	for _, market := range evaluation.Markets {
		instrument := market.Book.Instrument()
		version := market.Book.Version()
		for _, side := range []struct {
			name   string
			levels []exchangecontracts.PriceLevel
		}{{"bid", market.Book.Bids()}, {"ask", market.Book.Asks()}} {
			capacity, capacityErr := recordedDepthCapacity(side.levels)
			if capacityErr != nil || openRecordedClaim(set, portfolio.ClaimLiquidity, owner, market.Book.Exchange(),
				strings.ToLower(instrument.Symbol()+"-"+side.name+"-v"+uintString(version)), capacity) != nil {
				return nil, strategyError("recorded_claims_input_invalid")
			}
		}
	}
	return set, nil
}

func domainBalance(value domain.Money) domain.Balance {
	result, _ := domain.ParseBalance(value.String())
	return result
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

// openPositiveRecordedClaim omits a zero recorded capacity. A candidate that
// needs that resource will fail its atomic claim, while a valid opposite
// direction remains eligible from the same complete ownership snapshot.
func openPositiveRecordedClaim(
	set *portfolio.AtomicClaimSet,
	kind portfolio.ClaimKind,
	owner, exchange, resource string,
	amount domain.Balance,
) error {
	zero, _ := domain.ParseBalance("0")
	if amount.Compare(zero) == 0 {
		return nil
	}
	return openRecordedClaim(set, kind, owner, exchange, resource, amount)
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

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
