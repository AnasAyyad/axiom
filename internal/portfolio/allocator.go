package portfolio

import (
	"sort"
	"sync"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
	runtimecore "axiom/internal/runtime"
)

// ScoreComponent retains each audited allocation ranking input.
type ScoreComponent struct {
	Name  string
	Value domain.PnL
}

// Settle applies one final fill and permanently consumes its displayed-liquidity claim.
func (allocator *Allocator) Settle(allocation Allocation, fill execution.FillFact) error {
	_, err := allocator.ApplyFill(allocation, fill, true)
	return err
}

// ApplyFill applies one fill and returns the revised exclusive claims for the next partial fill.
func (allocator *Allocator) ApplyFill(
	allocation Allocation,
	fill execution.FillFact,
	final bool,
) (Allocation, error) {
	allocator.mutex.Lock()
	defer allocator.mutex.Unlock()
	if !allocator.active(allocation) || fill.Quantity.Compare(allocation.Liquidity.Remaining) > 0 ||
		(!final && fill.Quantity.Compare(allocation.Liquidity.Remaining) == 0) {
		return Allocation{}, portfolioError("allocation_settlement_rejected")
	}
	funds, err := allocator.portfolio.ApplyFill(allocation, fill, final)
	if err != nil {
		return Allocation{}, err
	}
	liquidity, err := allocator.liquidity.Consume(allocation.Liquidity.ID, allocation.Liquidity.Revision,
		allocation.Liquidity.Fence, fill.Quantity, final)
	if err != nil {
		return Allocation{}, portfolioError("liquidity_settlement_invalid")
	}
	allocation.Funds, allocation.Liquidity = funds, liquidity
	return allocation, nil
}

// Close releases, expires, or quarantines both exclusive claims under CAS and fencing.
func (allocator *Allocator) Close(allocation Allocation, state accounting.ReservationState) error {
	allocator.mutex.Lock()
	defer allocator.mutex.Unlock()
	if !allocator.active(allocation) || (state != accounting.ReservationReleased &&
		state != accounting.ReservationExpired && state != accounting.ReservationQuarantined) {
		return portfolioError("allocation_close_rejected")
	}
	var err error
	switch state {
	case accounting.ReservationReleased:
		err = allocator.portfolio.ledger.Release(allocation.Funds.ID, allocation.Funds.Revision, allocation.Funds.Fence)
	case accounting.ReservationExpired:
		err = allocator.portfolio.ledger.Expire(allocation.Funds.ID, allocation.Funds.Revision, allocation.Funds.Fence)
	case accounting.ReservationQuarantined:
		err = allocator.portfolio.ledger.Quarantine(allocation.Funds.ID, allocation.Funds.Revision, allocation.Funds.Fence)
	}
	if err != nil {
		return portfolioError("allocation_close_rejected")
	}
	if err = allocator.liquidity.Transition(allocation.Liquidity.ID, allocation.Liquidity.Revision,
		allocation.Liquidity.Fence, string(state)); err != nil {
		return portfolioError("allocation_close_inconsistent")
	}
	return nil
}

func (allocator *Allocator) active(allocation Allocation) bool {
	funds, fundsExists := allocator.portfolio.ledger.Reservation(allocation.Funds.ID)
	liquidity, liquidityExists := allocator.liquidity.Reservation(allocation.Liquidity.ID)
	return fundsExists && liquidityExists && funds == allocation.Funds && liquidity == allocation.Liquidity &&
		funds.State == accounting.ReservationActive && liquidity.State == "active"
}

// Candidate is one spot-only allocation request with current eligibility evidence.
type Candidate struct {
	ID                   string
	Strategy             string
	Instrument           domain.Instrument
	Side                 domain.Side
	Quantity             domain.Quantity
	Notional             domain.Money
	Score                domain.PnL
	ScoreComponents      []ScoreComponent
	BaseEligibility      uint64
	QuoteEligibility     uint64
	LiquidityDomain      string
	LiquidityReservation domain.ReservationID
	FundsReservation     domain.ReservationID
	Fence                runtimecore.FencingToken
}

// Allocation retains exclusive funds, inventory, and displayed-liquidity claims.
type Allocation struct {
	Candidate Candidate
	Funds     accounting.Reservation
	Liquidity LiquidityReservation
}

// Allocator ranks and atomically reserves the isolated V1A Trend portfolio.
type Allocator struct {
	mutex        sync.Mutex
	portfolio    *Portfolio
	registry     AssetRegistry
	liquidity    *LiquidityPool
	reserveFloor domain.Balance
	reservedCap  domain.Balance
	tradeBudget  domain.Balance
}

// NewAllocator constructs the conservative V1A allocation boundary.
func NewAllocator(portfolio *Portfolio, registry AssetRegistry, liquidity *LiquidityPool) (*Allocator, error) {
	if portfolio == nil || registry == nil || liquidity == nil {
		return nil, portfolioError("allocator_configuration_invalid")
	}
	reserve, _ := domain.ParseBalance("75.00")
	reserved, _ := domain.ParseBalance("425.00")
	budget, _ := domain.ParseBalance("150.00")
	return &Allocator{portfolio: portfolio, registry: registry, liquidity: liquidity,
		reserveFloor: reserve, reservedCap: reserved, tradeBudget: budget}, nil
}

// Allocate ranks candidates and returns every successful exclusive reservation.
func (allocator *Allocator) Allocate(candidates []Candidate) ([]Allocation, error) {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(left, right int) bool {
		comparison := ordered[left].Score.Compare(ordered[right].Score)
		return comparison > 0 || (comparison == 0 && ordered[left].ID < ordered[right].ID)
	})
	allocations := make([]Allocation, 0, len(ordered))
	for _, candidate := range ordered {
		allocation, err := allocator.reserve(candidate)
		if err != nil {
			continue
		}
		allocations = append(allocations, allocation)
	}
	if len(candidates) > 0 && len(allocations) == 0 {
		return nil, portfolioError("all_candidates_rejected")
	}
	return allocations, nil
}

func (allocator *Allocator) reserve(candidate Candidate) (Allocation, error) {
	allocator.mutex.Lock()
	defer allocator.mutex.Unlock()
	return allocator.reserveLocked(candidate)
}

func (allocator *Allocator) reserveLocked(candidate Candidate) (Allocation, error) {
	if err := allocator.validate(candidate); err != nil {
		return Allocation{}, err
	}
	asset, quantity, err := allocator.fundsRequirement(candidate)
	if err != nil {
		return Allocation{}, err
	}
	key, exists := allocator.portfolio.BalanceKey(asset)
	if !exists || allocator.capacityRejected(candidate, key, quantity) {
		return Allocation{}, portfolioError("allocation_capacity_rejected")
	}
	funds, err := allocator.portfolio.ledger.Reserve(candidate.FundsReservation, key, quantity, candidate.Fence)
	if err != nil {
		return Allocation{}, portfolioError("funds_reservation_rejected")
	}
	liquidity, err := allocator.liquidity.Reserve(candidate.LiquidityReservation,
		candidate.LiquidityDomain, candidate.Quantity, candidate.Fence)
	if err != nil {
		_ = allocator.portfolio.ledger.Release(funds.ID, funds.Revision, funds.Fence)
		return Allocation{}, err
	}
	return Allocation{Candidate: cloneCandidate(candidate), Funds: funds, Liquidity: liquidity}, nil
}

func (allocator *Allocator) validate(candidate Candidate) error {
	if candidate.ID == "" || candidate.Strategy == "" || candidate.Strategy != allocator.portfolio.ownership.Strategy ||
		candidate.Fence == 0 || candidate.LiquidityDomain == "" ||
		candidate.FundsReservation.Value() == "" || candidate.LiquidityReservation.Value() == "" ||
		candidate.FundsReservation == candidate.LiquidityReservation || candidate.Quantity.String() == "0" ||
		candidate.Notional.String() == "0" || len(candidate.ScoreComponents) == 0 ||
		(candidate.Side != domain.SideBuy && candidate.Side != domain.SideSell) {
		return portfolioError("candidate_invalid")
	}
	validated, err := domain.NewSpotInstrument(candidate.Instrument.Base, candidate.Instrument.Quote)
	if err != nil || validated != candidate.Instrument {
		return portfolioError("candidate_invalid")
	}
	if err = requireApproved(allocator.registry, candidate.Instrument.Base, candidate.BaseEligibility); err != nil {
		return err
	}
	if err = requireApproved(allocator.registry, candidate.Instrument.Quote, candidate.QuoteEligibility); err != nil {
		return err
	}
	if candidate.Side == domain.SideBuy && allocator.portfolio.hasOwnedPosition(candidate.Instrument) {
		return portfolioError("position_increase_rejected")
	}
	return nil
}

func (allocator *Allocator) fundsRequirement(candidate Candidate) (domain.AssetSymbol, domain.Balance, error) {
	if candidate.Side == domain.SideBuy {
		quantity, err := domain.ParseBalance(candidate.Notional.String())
		return candidate.Instrument.Quote, quantity, err
	}
	quantity, err := domain.ParseBalance(candidate.Quantity.String())
	return candidate.Instrument.Base, quantity, err
}

func (allocator *Allocator) capacityRejected(
	candidate Candidate,
	key accounting.BalanceKey,
	quantity domain.Balance,
) bool {
	balance, exists := allocator.portfolio.ledger.Balance(key)
	if !exists {
		return true
	}
	if candidate.Side == domain.SideSell {
		return balance.Available.Compare(quantity) < 0
	}
	available, err := balance.Available.Subtract(quantity)
	reserved, reservedErr := balance.Reserved.Add(quantity)
	return err != nil || reservedErr != nil || quantity.Compare(allocator.tradeBudget) > 0 ||
		available.Compare(allocator.reserveFloor) < 0 || reserved.Compare(allocator.reservedCap) > 0
}

func cloneCandidate(candidate Candidate) Candidate {
	candidate.ScoreComponents = append([]ScoreComponent(nil), candidate.ScoreComponents...)
	return candidate
}
