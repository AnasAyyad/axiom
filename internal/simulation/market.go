package simulation

import (
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
)

// BookState is one immutable executable public-book observation.
type BookState struct {
	Exchange    string
	Instrument  domain.Instrument
	Version     uint64
	LogicalTime uint64
	Bids        []exchangecontracts.PriceLevel
	Asks        []exchangecontracts.PriceLevel
}

// MarketTimeline returns the first market state at or after simulated arrival.
type MarketTimeline interface {
	AtOrAfter(domain.Instrument, uint64) (BookState, bool, error)
}

// MetadataSource returns the instrument filters applicable to an arrival state.
type MetadataSource interface {
	Metadata(BookState) (domain.InstrumentMetadata, error)
}

// BoundaryGuard rechecks eligibility and owned inventory at broker arrival.
// A9 supplies the operational implementation; A8 qualification uses explicit
// test-only guards and production composition fails closed when this is absent.
type BoundaryGuard interface {
	Authorize(execution.PlannedLeg, BookState) (domain.Balance, error)
}

func cloneBook(state BookState) BookState {
	state.Bids = append([]exchangecontracts.PriceLevel(nil), state.Bids...)
	state.Asks = append([]exchangecontracts.PriceLevel(nil), state.Asks...)
	return state
}
