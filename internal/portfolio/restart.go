package portfolio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"axiom/internal/accounting"
	"axiom/internal/domain"
)

// ProtectedState is the complete A9 portfolio checkpoint needed across restart.
type ProtectedState struct {
	Ownership Ownership
	Numeraire domain.AssetSymbol
	Ledger    accounting.ReservationLedgerState
	Positions []Position
	Revision  uint64
}

// ProtectedState returns a canonical copy of ownership, reservations, and positions.
func (portfolio *Portfolio) ProtectedState() ProtectedState {
	portfolio.mutex.Lock()
	defer portfolio.mutex.Unlock()
	positions := make([]Position, 0, len(portfolio.positions))
	for _, position := range portfolio.positions {
		positions = append(positions, position)
	}
	sortPositions(positions)
	return ProtectedState{Ownership: portfolio.ownership, Numeraire: portfolio.numeraire,
		Ledger: portfolio.ledger.State(), Positions: positions, Revision: portfolio.revision}
}

// CanonicalHash identifies byte-equivalent protected portfolio state.
func (state ProtectedState) CanonicalHash() string {
	encoded, _ := json.Marshal(state)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Restore validates one checkpoint without activating strategy execution.
func Restore(state ProtectedState) (*Portfolio, error) {
	if state.Ownership.PortfolioID.Value() == "" || state.Ownership.AccountID.Value() == "" ||
		state.Ownership.Strategy != V1AStrategy || state.Ownership.Exchange != V1AExchange ||
		state.Numeraire != V1ANumeraire || state.Revision == 0 {
		return nil, portfolioError("portfolio_restore_invalid")
	}
	ledger, err := accounting.RestoreReservationLedger(state.Ledger)
	if err != nil {
		return nil, portfolioError("portfolio_restore_invalid")
	}
	balances, err := restoreBalances(state)
	if err != nil {
		return nil, err
	}
	positions, err := restorePositions(state, ledger, balances)
	if err != nil {
		return nil, err
	}
	return &Portfolio{ownership: state.Ownership, numeraire: state.Numeraire, ledger: ledger,
		balances: balances, positions: positions, revision: state.Revision}, nil
}

func restoreBalances(state ProtectedState) (map[domain.AssetSymbol]accounting.BalanceKey, error) {
	balances := make(map[domain.AssetSymbol]accounting.BalanceKey, 3)
	for _, item := range state.Ledger.Balances {
		if item.Key.Account != state.Ownership.AccountID ||
			(item.Key.Asset != "USDT" && item.Key.Asset != "BTC" && item.Key.Asset != "ETH") {
			return nil, portfolioError("portfolio_restore_invalid")
		}
		balances[item.Key.Asset] = item.Key
	}
	if len(balances) != 3 {
		return nil, portfolioError("portfolio_restore_invalid")
	}
	return balances, nil
}

func restorePositions(
	state ProtectedState,
	ledger *accounting.ReservationLedger,
	balances map[domain.AssetSymbol]accounting.BalanceKey,
) (map[domain.Instrument]Position, error) {
	positions := make(map[domain.Instrument]Position, len(state.Positions))
	for _, position := range state.Positions {
		instrument, instrumentErr := domain.NewSpotInstrument(position.Instrument.Base, position.Instrument.Quote)
		if instrumentErr != nil || instrument != position.Instrument || position.Instrument.Quote != V1ANumeraire ||
			(position.Instrument.Base != "BTC" && position.Instrument.Base != "ETH") || position.Revision == 0 {
			return nil, portfolioError("portfolio_restore_invalid")
		}
		if _, duplicate := positions[position.Instrument]; duplicate {
			return nil, portfolioError("portfolio_restore_invalid")
		}
		key := balances[position.Instrument.Base]
		balance, _ := ledger.Balance(key)
		owned, addErr := balance.Available.Add(balance.Reserved)
		if addErr != nil || owned.Compare(position.Quantity) != 0 {
			return nil, portfolioError("portfolio_restore_invalid")
		}
		positions[position.Instrument] = position
	}
	zero, _ := domain.ParseBalance("0")
	for _, asset := range []domain.AssetSymbol{"BTC", "ETH"} {
		balance, _ := ledger.Balance(balances[asset])
		owned, addErr := balance.Available.Add(balance.Reserved)
		instrument, _ := domain.NewSpotInstrument(asset, V1ANumeraire)
		_, hasPosition := positions[instrument]
		if addErr != nil || (owned.Compare(zero) > 0) != hasPosition {
			return nil, portfolioError("portfolio_restore_invalid")
		}
	}
	return positions, nil
}

func sortPositions(positions []Position) {
	for left := 1; left < len(positions); left++ {
		for right := left; right > 0; right-- {
			current := string(positions[right].Instrument.Base) + "/" + string(positions[right].Instrument.Quote)
			prior := string(positions[right-1].Instrument.Base) + "/" + string(positions[right-1].Instrument.Quote)
			if prior <= current {
				break
			}
			positions[right], positions[right-1] = positions[right-1], positions[right]
		}
	}
}
