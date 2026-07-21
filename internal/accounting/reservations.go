package accounting

import (
	"sort"
	"sync"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

// ReservationState is the persisted exclusive-funds lifecycle.
type ReservationState string

// Supported reservation states.
const (
	ReservationActive      ReservationState = "active"
	ReservationConsumed    ReservationState = "consumed"
	ReservationReleased    ReservationState = "released"
	ReservationExpired     ReservationState = "expired"
	ReservationQuarantined ReservationState = "quarantined"
)

// BalanceKey identifies one virtual owned commodity balance.
type BalanceKey struct {
	Account domain.VirtualAccountID
	Asset   domain.AssetSymbol
}

// BalanceSnapshot contains exact available and reserved ownership.
type BalanceSnapshot struct {
	Available domain.Balance
	Reserved  domain.Balance
	Revision  uint64
}

// Reservation is immutable reservation state at one revision.
type Reservation struct {
	ID        domain.ReservationID
	Balance   BalanceKey
	Quantity  domain.Balance
	Remaining domain.Balance
	State     ReservationState
	Fence     runtimecore.FencingToken
	Revision  uint64
}

// OwnedBalanceState is one canonical ledger-owned balance projection.
type OwnedBalanceState struct {
	Key      BalanceKey
	Snapshot BalanceSnapshot
}

// ReservationLedgerState is a canonical restart-safe ownership checkpoint.
type ReservationLedgerState struct {
	Balances     []OwnedBalanceState
	Reservations []Reservation
}

// ReservationLedger serializes virtual ownership and reservation lifecycle.
type ReservationLedger struct {
	mutex        sync.Mutex
	balances     map[BalanceKey]BalanceSnapshot
	reservations map[string]Reservation
}

// NewReservationLedger constructs an empty fail-closed ledger.
func NewReservationLedger() *ReservationLedger {
	return &ReservationLedger{
		balances: make(map[BalanceKey]BalanceSnapshot), reservations: make(map[string]Reservation),
	}
}

// State returns deterministically ordered owned balances and reservation facts.
func (ledger *ReservationLedger) State() ReservationLedgerState {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	state := ReservationLedgerState{Balances: make([]OwnedBalanceState, 0, len(ledger.balances)),
		Reservations: make([]Reservation, 0, len(ledger.reservations))}
	for key, snapshot := range ledger.balances {
		state.Balances = append(state.Balances, OwnedBalanceState{Key: key, Snapshot: snapshot})
	}
	for _, reservation := range ledger.reservations {
		state.Reservations = append(state.Reservations, reservation)
	}
	sort.Slice(state.Balances, func(left, right int) bool {
		leftKey := state.Balances[left].Key.Account.String() + "/" + string(state.Balances[left].Key.Asset)
		rightKey := state.Balances[right].Key.Account.String() + "/" + string(state.Balances[right].Key.Asset)
		return leftKey < rightKey
	})
	sort.Slice(state.Reservations, func(left, right int) bool {
		return state.Reservations[left].ID.String() < state.Reservations[right].ID.String()
	})
	return state
}

// RestoreReservationLedger validates and restores one complete ownership checkpoint.
func RestoreReservationLedger(state ReservationLedgerState) (*ReservationLedger, error) {
	if len(state.Balances) == 0 {
		return nil, accountingError("reservation_state_invalid")
	}
	ledger := NewReservationLedger()
	zero, _ := domain.ParseBalance("0")
	held := make(map[BalanceKey]domain.Balance)
	for _, item := range state.Balances {
		if !validBalanceKey(item.Key) || item.Snapshot.Revision == 0 {
			return nil, accountingError("reservation_state_invalid")
		}
		if _, exists := ledger.balances[item.Key]; exists {
			return nil, accountingError("reservation_state_invalid")
		}
		ledger.balances[item.Key] = item.Snapshot
		held[item.Key] = zero
	}
	for _, reservation := range state.Reservations {
		if reservation.ID.Value() == "" || reservation.Revision == 0 || reservation.Fence == 0 ||
			!positive(reservation.Quantity) || !knownReservationState(reservation.State) ||
			reservation.Remaining.Compare(reservation.Quantity) > 0 {
			return nil, accountingError("reservation_state_invalid")
		}
		open := reservation.State == ReservationActive || reservation.State == ReservationQuarantined
		if (open && !positive(reservation.Remaining)) || (!open && reservation.Remaining.Compare(zero) != 0) {
			return nil, accountingError("reservation_state_invalid")
		}
		if _, exists := ledger.balances[reservation.Balance]; !exists {
			return nil, accountingError("reservation_state_invalid")
		}
		if _, exists := ledger.reservations[reservation.ID.String()]; exists {
			return nil, accountingError("reservation_state_invalid")
		}
		if open {
			quantity, err := held[reservation.Balance].Add(reservation.Remaining)
			if err != nil {
				return nil, accountingError("reservation_state_invalid")
			}
			held[reservation.Balance] = quantity
		}
		ledger.reservations[reservation.ID.String()] = reservation
	}
	for key, balance := range ledger.balances {
		if balance.Reserved.Compare(held[key]) != 0 {
			return nil, accountingError("reservation_state_invalid")
		}
	}
	return ledger, nil
}

func knownReservationState(state ReservationState) bool {
	return state == ReservationActive || state == ReservationConsumed || state == ReservationReleased ||
		state == ReservationExpired || state == ReservationQuarantined
}

// OpenBalance creates one unique virtual balance with no reserved amount.
func (ledger *ReservationLedger) OpenBalance(key BalanceKey, available domain.Balance) error {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	if !validBalanceKey(key) {
		return accountingError("invalid_balance")
	}
	if _, exists := ledger.balances[key]; exists {
		return accountingError("duplicate_balance")
	}
	zero, _ := domain.ParseBalance("0")
	ledger.balances[key] = BalanceSnapshot{Available: available, Reserved: zero, Revision: 1}
	return nil
}

// Reserve atomically moves owned availability into one exclusive reservation.
func (ledger *ReservationLedger) Reserve(id domain.ReservationID, key BalanceKey, quantity domain.Balance, fence runtimecore.FencingToken) (Reservation, error) {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	if id.Value() == "" || fence == 0 || !positive(quantity) {
		return Reservation{}, accountingError("invalid_reservation")
	}
	if _, exists := ledger.reservations[id.String()]; exists {
		return Reservation{}, accountingError("duplicate_reservation")
	}
	balance, exists := ledger.balances[key]
	if !exists {
		return Reservation{}, accountingError("balance_missing")
	}
	available, err := balance.Available.Subtract(quantity)
	if err != nil {
		return Reservation{}, accountingError("insufficient_owned_balance")
	}
	reserved, err := balance.Reserved.Add(quantity)
	if err != nil {
		return Reservation{}, accountingError("balance_overflow")
	}
	balance.Available, balance.Reserved, balance.Revision = available, reserved, balance.Revision+1
	reservation := Reservation{
		ID: id, Balance: key, Quantity: quantity, Remaining: quantity,
		State: ReservationActive, Fence: fence, Revision: 1,
	}
	ledger.balances[key], ledger.reservations[id.String()] = balance, reservation
	return reservation, nil
}

// Consume permanently removes a currently active reserved amount.
func (ledger *ReservationLedger) Consume(id domain.ReservationID, expectedRevision uint64, fence runtimecore.FencingToken) error {
	return ledger.transition(id, expectedRevision, fence, ReservationConsumed, true, false)
}

// Release returns a currently active amount to availability.
func (ledger *ReservationLedger) Release(id domain.ReservationID, expectedRevision uint64, fence runtimecore.FencingToken) error {
	return ledger.transition(id, expectedRevision, fence, ReservationReleased, true, true)
}

// Expire returns only a still-active reservation under the current fence.
func (ledger *ReservationLedger) Expire(id domain.ReservationID, expectedRevision uint64, fence runtimecore.FencingToken) error {
	return ledger.transition(id, expectedRevision, fence, ReservationExpired, true, true)
}

// Quarantine closes uncertain ownership while keeping its quantity unavailable.
func (ledger *ReservationLedger) Quarantine(id domain.ReservationID, expectedRevision uint64, fence runtimecore.FencingToken) error {
	return ledger.transition(id, expectedRevision, fence, ReservationQuarantined, false, false)
}

// Settle atomically consumes reserved source ownership and credits acquired proceeds.
func (ledger *ReservationLedger) Settle(
	id domain.ReservationID,
	expectedRevision uint64,
	fence runtimecore.FencingToken,
	debitQuantity domain.Balance,
	creditKey BalanceKey,
	creditQuantity domain.Balance,
) error {
	_, err := ledger.SettleFill(id, expectedRevision, fence, debitQuantity, creditKey, creditQuantity, true)
	return err
}

// SettleFill atomically settles one fill while retaining unused ownership for later partial fills.
func (ledger *ReservationLedger) SettleFill(
	id domain.ReservationID,
	expectedRevision uint64,
	fence runtimecore.FencingToken,
	debitQuantity domain.Balance,
	creditKey BalanceKey,
	creditQuantity domain.Balance,
	final bool,
) (Reservation, error) {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	reservation, exists := ledger.reservations[id.String()]
	credit, creditExists := ledger.balances[creditKey]
	if !exists || !creditExists || reservation.State != ReservationActive || reservation.Revision != expectedRevision ||
		reservation.Fence != fence || reservation.Balance == creditKey || !positive(debitQuantity) || !positive(creditQuantity) {
		return Reservation{}, accountingError("reservation_settlement_rejected")
	}
	return ledger.settleFillLocked(id, reservation, creditKey, credit, debitQuantity, creditQuantity, final)
}

func (ledger *ReservationLedger) settleFillLocked(
	id domain.ReservationID,
	reservation Reservation,
	creditKey BalanceKey,
	credit BalanceSnapshot,
	debitQuantity domain.Balance,
	creditQuantity domain.Balance,
	final bool,
) (Reservation, error) {
	source := ledger.balances[reservation.Balance]
	remaining, err := reservation.Remaining.Subtract(debitQuantity)
	if err != nil {
		return Reservation{}, accountingError("reservation_settlement_rejected")
	}
	zero, _ := domain.ParseBalance("0")
	if !final && remaining.Compare(zero) == 0 {
		return Reservation{}, accountingError("reservation_settlement_rejected")
	}
	reserved, err := source.Reserved.Subtract(debitQuantity)
	if err != nil {
		return Reservation{}, accountingError("reservation_projection_invalid")
	}
	availableSource := source.Available
	if final {
		reserved, err = reserved.Subtract(remaining)
		if err == nil {
			availableSource, err = availableSource.Add(remaining)
		}
		if err != nil {
			return Reservation{}, accountingError("reservation_projection_invalid")
		}
	}
	available, err := credit.Available.Add(creditQuantity)
	if err != nil {
		return Reservation{}, accountingError("balance_overflow")
	}
	source.Available, source.Reserved, source.Revision = availableSource, reserved, source.Revision+1
	credit.Available, credit.Revision = available, credit.Revision+1
	reservation.Remaining, reservation.Revision = remaining, reservation.Revision+1
	if final {
		reservation.Remaining, reservation.State = zero, ReservationConsumed
	}
	ledger.balances[reservation.Balance], ledger.balances[creditKey] = source, credit
	ledger.reservations[id.String()] = reservation
	return reservation, nil
}

// Balance returns a consistent exact ownership snapshot.
func (ledger *ReservationLedger) Balance(key BalanceKey) (BalanceSnapshot, bool) {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	balance, exists := ledger.balances[key]
	return balance, exists
}

// Reservation returns one immutable lifecycle snapshot.
func (ledger *ReservationLedger) Reservation(id domain.ReservationID) (Reservation, bool) {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	reservation, exists := ledger.reservations[id.String()]
	return reservation, exists
}

func (ledger *ReservationLedger) transition(
	id domain.ReservationID,
	revision uint64,
	fence runtimecore.FencingToken,
	next ReservationState,
	removeReserved bool,
	release bool,
) error {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	reservation, exists := ledger.reservations[id.String()]
	if !exists || reservation.State != ReservationActive || reservation.Revision != revision || reservation.Fence != fence {
		return accountingError("reservation_transition_rejected")
	}
	balance := ledger.balances[reservation.Balance]
	if removeReserved {
		reserved, err := balance.Reserved.Subtract(reservation.Remaining)
		if err != nil {
			return accountingError("reservation_projection_invalid")
		}
		balance.Reserved = reserved
		if release {
			balance.Available, err = balance.Available.Add(reservation.Remaining)
			if err != nil {
				return accountingError("balance_overflow")
			}
		}
		balance.Revision++
	}
	reservation.State, reservation.Revision = next, reservation.Revision+1
	if removeReserved {
		reservation.Remaining, _ = domain.ParseBalance("0")
	}
	ledger.balances[reservation.Balance], ledger.reservations[id.String()] = balance, reservation
	return nil
}

func validBalanceKey(key BalanceKey) bool {
	_, err := domain.ParseAssetSymbol(string(key.Asset))
	return key.Account.Value() != "" && err == nil
}

func positive(value domain.Balance) bool {
	zero, _ := domain.ParseBalance("0")
	return value.Compare(zero) > 0
}
