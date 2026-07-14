package accounting

import (
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
	ID       domain.ReservationID
	Balance  BalanceKey
	Quantity domain.Balance
	State    ReservationState
	Fence    runtimecore.FencingToken
	Revision uint64
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
		ID: id, Balance: key, Quantity: quantity, State: ReservationActive, Fence: fence, Revision: 1,
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
		reserved, err := balance.Reserved.Subtract(reservation.Quantity)
		if err != nil {
			return accountingError("reservation_projection_invalid")
		}
		balance.Reserved = reserved
		if release {
			balance.Available, err = balance.Available.Add(reservation.Quantity)
			if err != nil {
				return accountingError("balance_overflow")
			}
		}
		balance.Revision++
	}
	reservation.State, reservation.Revision = next, reservation.Revision+1
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
