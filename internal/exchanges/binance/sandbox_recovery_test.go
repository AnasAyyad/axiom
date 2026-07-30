package binance

import (
	"context"
	"errors"
	"testing"
	"time"

	"axiom/internal/sandbox"
)

type recoveryExchange struct {
	snapshot   sandbox.AccountSnapshot
	result     sandbox.ReconciliationResult
	snapshots  int
	reconciles int
}

func (exchange *recoveryExchange) Capabilities(
	context.Context,
) (sandbox.CapabilityDescriptor, error) {
	return sandbox.CapabilityDescriptor{}, nil
}

func (exchange *recoveryExchange) Identity(
	context.Context,
) (sandbox.AccountIdentity, error) {
	return sandbox.AccountIdentity{}, nil
}

func (exchange *recoveryExchange) Snapshot(
	context.Context,
) (sandbox.AccountSnapshot, error) {
	exchange.snapshots++
	return exchange.snapshot, nil
}

func (exchange *recoveryExchange) Reconcile(
	context.Context,
	sandbox.AccountID,
	uint64,
) (sandbox.ReconciliationResult, error) {
	panic("recovery must reconcile the already loaded snapshot")
}

func (exchange *recoveryExchange) ReconcileSnapshot(
	_ context.Context,
	_ sandbox.AccountID,
	_ uint64,
	snapshot sandbox.AccountSnapshot,
) (sandbox.ReconciliationResult, error) {
	if snapshot.SnapshotHash != exchange.snapshot.SnapshotHash {
		return sandbox.ReconciliationResult{}, ErrSandboxRequest
	}
	exchange.reconciles++
	return exchange.result, nil
}

type recoveryStore struct {
	previous sandbox.AccountSnapshot
	found    bool
	recorded []sandbox.AccountSnapshot
	resets   []sandbox.AccountResetIncident
}

func (store *recoveryStore) LatestAccountSnapshot(
	context.Context,
	sandbox.AccountID,
	uint64,
) (sandbox.AccountSnapshot, bool, error) {
	return store.previous, store.found, nil
}

func (store *recoveryStore) RecordAccountSnapshot(
	_ context.Context,
	_ string,
	snapshot sandbox.AccountSnapshot,
) error {
	store.recorded = append(store.recorded, snapshot)
	return nil
}

func (store *recoveryStore) RecordAccountReset(
	_ context.Context,
	incident sandbox.AccountResetIncident,
) error {
	store.resets = append(store.resets, incident)
	return nil
}

func TestBinanceRecoveryRecordsBaselineThenRequiresCleanSecondRead(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	current := resetSnapshot(t, now, "100")
	exchange := &recoveryExchange{
		snapshot: current,
		result: sandbox.ReconciliationResult{
			ID:           "reconciliation-clean",
			AccountID:    current.AccountID,
			AccountEpoch: current.Epoch,
			State:        "clean",
			EvidenceHash: canonicalHash("clean"),
			ReconciledAt: now.Add(time.Second),
		},
	}
	store := &recoveryStore{}
	coordinator, err := NewSandboxRecoveryCoordinator(
		exchange,
		exchange,
		store,
		current.AccountID,
		current.Epoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Recover(context.Background())
	if err != nil || result.State != "clean" ||
		len(store.recorded) != 1 || len(store.resets) != 0 ||
		exchange.snapshots != 1 || exchange.reconciles != 1 {
		t.Fatalf(
			"result=%#v stored=%d reads=%d resets=%d reconciles=%d err=%v",
			result,
			len(store.recorded),
			exchange.snapshots,
			len(store.resets),
			exchange.reconciles,
			err,
		)
	}
}

func TestBinanceRecoveryPersistsResetAndStopsStaleEpoch(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	previous := resetSnapshot(t, now, "50")
	previous.OrdersHash = canonicalHash("prior-orders")
	previous.SnapshotHash = canonicalHash(previous)
	current := resetSnapshot(t, now.Add(time.Minute), "100")
	exchange := &recoveryExchange{snapshot: current}
	store := &recoveryStore{previous: previous, found: true}
	coordinator, err := NewSandboxRecoveryCoordinator(
		exchange,
		exchange,
		store,
		current.AccountID,
		current.Epoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Recover(context.Background())
	if !errors.Is(err, ErrSandboxResetDetected) ||
		len(store.resets) != 1 || len(store.recorded) != 0 ||
		exchange.reconciles != 0 {
		t.Fatalf(
			"snapshots=%d resets=%d reconciles=%d err=%v",
			len(store.recorded),
			len(store.resets),
			exchange.reconciles,
			err,
		)
	}
}
