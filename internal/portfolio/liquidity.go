package portfolio

import (
	"sort"
	"sync"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

// LiquidityAvailability is one canonical displayed-depth projection.
type LiquidityAvailability struct {
	Domain   string
	Quantity domain.Quantity
}

// LiquidityPoolState is the complete restart-safe shared-liquidity checkpoint.
type LiquidityPoolState struct {
	Available    []LiquidityAvailability
	Reservations []LiquidityReservation
}

// LiquidityReservation is one exclusive displayed-depth claim.
type LiquidityReservation struct {
	ID        domain.ReservationID
	Domain    string
	Quantity  domain.Quantity
	Remaining domain.Quantity
	State     string
	Fence     runtimecore.FencingToken
	Revision  uint64
}

// LiquidityPool prevents concurrent candidates from reusing displayed depth.
type LiquidityPool struct {
	mutex        sync.Mutex
	available    map[string]domain.Quantity
	reservations map[string]LiquidityReservation
}

// NewLiquidityPool constructs an empty exclusive liquidity owner.
func NewLiquidityPool() *LiquidityPool {
	return &LiquidityPool{available: make(map[string]domain.Quantity), reservations: make(map[string]LiquidityReservation)}
}

// State returns a canonical copy of displayed depth and every reservation lifecycle.
func (pool *LiquidityPool) State() LiquidityPoolState {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	state := LiquidityPoolState{Available: make([]LiquidityAvailability, 0, len(pool.available)),
		Reservations: make([]LiquidityReservation, 0, len(pool.reservations))}
	for domainName, quantity := range pool.available {
		state.Available = append(state.Available, LiquidityAvailability{Domain: domainName, Quantity: quantity})
	}
	for _, reservation := range pool.reservations {
		state.Reservations = append(state.Reservations, reservation)
	}
	sort.Slice(state.Available, func(left, right int) bool {
		return state.Available[left].Domain < state.Available[right].Domain
	})
	sort.Slice(state.Reservations, func(left, right int) bool {
		return state.Reservations[left].ID.String() < state.Reservations[right].ID.String()
	})
	return state
}

// RestoreLiquidityPool validates and restores one complete shared-liquidity checkpoint.
func RestoreLiquidityPool(state LiquidityPoolState) (*LiquidityPool, error) {
	if len(state.Available) == 0 {
		return nil, portfolioError("liquidity_state_invalid")
	}
	pool := NewLiquidityPool()
	zero, _ := domain.ParseQuantity("0")
	for _, item := range state.Available {
		if item.Domain == "" || item.Quantity.Compare(zero) < 0 {
			return nil, portfolioError("liquidity_state_invalid")
		}
		if _, exists := pool.available[item.Domain]; exists {
			return nil, portfolioError("liquidity_state_invalid")
		}
		pool.available[item.Domain] = item.Quantity
	}
	for _, reservation := range state.Reservations {
		if reservation.ID.Value() == "" || reservation.Domain == "" || reservation.Fence == 0 ||
			reservation.Revision == 0 || reservation.Quantity.Compare(zero) <= 0 ||
			reservation.Remaining.Compare(reservation.Quantity) > 0 || !knownLiquidityState(reservation.State) {
			return nil, portfolioError("liquidity_state_invalid")
		}
		open := reservation.State == "active" || reservation.State == "quarantined"
		if (open && reservation.Remaining.Compare(zero) <= 0) || (!open && reservation.Remaining.Compare(zero) != 0) {
			return nil, portfolioError("liquidity_state_invalid")
		}
		if _, exists := pool.available[reservation.Domain]; !exists {
			return nil, portfolioError("liquidity_state_invalid")
		}
		if _, exists := pool.reservations[reservation.ID.String()]; exists {
			return nil, portfolioError("liquidity_state_invalid")
		}
		pool.reservations[reservation.ID.String()] = reservation
	}
	return pool, nil
}

func knownLiquidityState(state string) bool {
	return state == "active" || state == "consumed" || state == "released" ||
		state == "expired" || state == "quarantined"
}

// Open creates one unique combined-portfolio depth domain.
func (pool *LiquidityPool) Open(domainName string, quantity domain.Quantity) error {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if domainName == "" || quantity.String() == "0" {
		return portfolioError("liquidity_domain_invalid")
	}
	if _, exists := pool.available[domainName]; exists {
		return portfolioError("liquidity_domain_duplicate")
	}
	pool.available[domainName] = quantity
	return nil
}

// Reserve atomically claims visible quantity under a fencing token.
func (pool *LiquidityPool) Reserve(
	id domain.ReservationID,
	domainName string,
	quantity domain.Quantity,
	fence runtimecore.FencingToken,
) (LiquidityReservation, error) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	if id.Value() == "" || domainName == "" || quantity.String() == "0" || fence == 0 {
		return LiquidityReservation{}, portfolioError("liquidity_reservation_invalid")
	}
	if _, exists := pool.reservations[id.String()]; exists {
		return LiquidityReservation{}, portfolioError("liquidity_reservation_duplicate")
	}
	available, exists := pool.available[domainName]
	if !exists {
		return LiquidityReservation{}, portfolioError("liquidity_domain_missing")
	}
	remaining, err := available.Subtract(quantity)
	if err != nil {
		return LiquidityReservation{}, portfolioError("liquidity_insufficient")
	}
	reservation := LiquidityReservation{ID: id, Domain: domainName, Quantity: quantity, Remaining: quantity,
		State: "active", Fence: fence, Revision: 1}
	pool.available[domainName], pool.reservations[id.String()] = remaining, reservation
	return reservation, nil
}

// Consume settles one fill while retaining any unfilled displayed-depth claim.
func (pool *LiquidityPool) Consume(
	id domain.ReservationID,
	revision uint64,
	fence runtimecore.FencingToken,
	quantity domain.Quantity,
	final bool,
) (LiquidityReservation, error) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	reservation, exists := pool.reservations[id.String()]
	if !exists || reservation.State != "active" || reservation.Revision != revision ||
		reservation.Fence != fence || quantity.String() == "0" {
		return LiquidityReservation{}, portfolioError("liquidity_settlement_rejected")
	}
	remaining, err := reservation.Remaining.Subtract(quantity)
	if err != nil {
		return LiquidityReservation{}, portfolioError("liquidity_settlement_rejected")
	}
	zero, _ := domain.ParseQuantity("0")
	if !final && remaining.Compare(zero) == 0 {
		return LiquidityReservation{}, portfolioError("liquidity_settlement_rejected")
	}
	reservation.Remaining, reservation.Revision = remaining, reservation.Revision+1
	if final {
		available, addErr := pool.available[reservation.Domain].Add(remaining)
		if addErr != nil {
			return LiquidityReservation{}, portfolioError("liquidity_projection_invalid")
		}
		pool.available[reservation.Domain] = available
		reservation.Remaining, reservation.State = zero, "consumed"
	}
	pool.reservations[id.String()] = reservation
	return reservation, nil
}

// Transition closes one reservation with revision CAS and fencing.
func (pool *LiquidityPool) Transition(
	id domain.ReservationID,
	revision uint64,
	fence runtimecore.FencingToken,
	state string,
) error {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	reservation, exists := pool.reservations[id.String()]
	if !exists || reservation.State != "active" || reservation.Revision != revision || reservation.Fence != fence ||
		(state != "consumed" && state != "released" && state != "expired" && state != "quarantined") {
		return portfolioError("liquidity_transition_rejected")
	}
	if state == "released" || state == "expired" {
		available, err := pool.available[reservation.Domain].Add(reservation.Remaining)
		if err != nil {
			return portfolioError("liquidity_projection_invalid")
		}
		pool.available[reservation.Domain] = available
	}
	reservation.State, reservation.Revision = state, reservation.Revision+1
	if state != "quarantined" {
		reservation.Remaining, _ = domain.ParseQuantity("0")
	}
	pool.reservations[id.String()] = reservation
	return nil
}

// Available returns exact unclaimed displayed liquidity.
func (pool *LiquidityPool) Available(domainName string) (domain.Quantity, bool) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	value, exists := pool.available[domainName]
	return value, exists
}

// Reservation returns one immutable displayed-liquidity lifecycle snapshot.
func (pool *LiquidityPool) Reservation(id domain.ReservationID) (LiquidityReservation, bool) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()
	reservation, exists := pool.reservations[id.String()]
	return reservation, exists
}
