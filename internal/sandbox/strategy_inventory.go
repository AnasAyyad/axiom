package sandbox

import (
	"context"
	"time"

	"axiom/internal/domain"
)

// StrategyOwnedInventory is the strategy-session-specific base inventory
// available for an exit. It is intentionally separate from the exchange
// account snapshot: an asset held for another strategy or deposited outside
// Axiom must never become sellable by this session.
//
// EvidenceHash is derived from immutable fills belonging to the exact session
// and account epoch. The runtime records the source fills separately; this
// compact value is bound into the plan approval hash rather than copying a
// private account payload into the plan.
type StrategyOwnedInventory struct {
	SessionID    SessionID
	AccountID    AccountID
	AccountEpoch uint64
	Asset        domain.AssetSymbol
	Available    domain.Balance
	EvidenceHash string
	ObservedAt   time.Time
}

// StrategyOwnedInventorySource reads only immutable fills that belong to one
// strategy session. It is deliberately separate from AccountReader, which can
// see account-wide balances and therefore cannot establish strategy ownership.
type StrategyOwnedInventorySource interface {
	StrategyOwnedInventory(context.Context, StrategySessionWork, domain.AssetSymbol, time.Time) (StrategyOwnedInventory, error)
}

// ValidFor proves inventory is a fresh, exact session-owned position view at
// the same decision instant as its strategy-session admission.
func (inventory StrategyOwnedInventory) ValidFor(
	admission StrategySessionAdmission,
	asset domain.AssetSymbol,
) error {
	if inventory.SessionID != admission.Work.SessionID ||
		inventory.AccountID != admission.Work.Account.ID ||
		inventory.AccountEpoch != admission.Work.Account.Epoch ||
		inventory.Asset != asset || !hash256(inventory.EvidenceHash) ||
		inventory.ObservedAt.IsZero() || inventory.ObservedAt.Location() != time.UTC ||
		!inventory.ObservedAt.Equal(admission.ApprovedAt) {
		return contractError("strategy_owned_inventory_invalid")
	}
	if _, err := domain.ParseBalance(inventory.Available.String()); err != nil {
		return contractError("strategy_owned_inventory_invalid")
	}
	return nil
}
