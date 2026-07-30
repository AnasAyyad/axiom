package binance

import (
	"context"
	"errors"
	"strconv"

	"axiom/internal/sandbox"
)

// ErrSandboxResetDetected forces the stale-epoch engine to stop after the
// reset transaction locks entry and advances the account epoch.
var ErrSandboxResetDetected = errors.New("binance_testnet_reset_detected")

// SandboxRecoveryCoordinator binds authoritative snapshots, reset detection,
// immutable history, and reconciliation into one fail-closed startup step.
type SandboxRecoveryCoordinator struct {
	reader     sandbox.AccountReader
	reconciler sandbox.SnapshotReconciler
	store      sandbox.AccountRecoveryStore
	account    sandbox.AccountID
	epoch      uint64
}

// NewSandboxRecoveryCoordinator constructs one exact account-epoch recovery
// boundary.
func NewSandboxRecoveryCoordinator(
	reader sandbox.AccountReader,
	reconciler sandbox.SnapshotReconciler,
	store sandbox.AccountRecoveryStore,
	account sandbox.AccountID,
	epoch uint64,
) (*SandboxRecoveryCoordinator, error) {
	if reader == nil || reconciler == nil || store == nil ||
		account == "" || epoch == 0 {
		return nil, ErrSandboxRequest
	}
	return &SandboxRecoveryCoordinator{
		reader:     reader,
		reconciler: reconciler,
		store:      store,
		account:    account,
		epoch:      epoch,
	}, nil
}

// Recover loads one coherent authoritative snapshot after durable reducer,
// journal, and reservation recovery, records it, and reconciles that exact
// snapshot. A coherent reset is persisted atomically and terminates the
// stale-epoch process.
func (coordinator *SandboxRecoveryCoordinator) Recover(
	ctx context.Context,
) (sandbox.ReconciliationResult, error) {
	current, err := coordinator.reader.Snapshot(ctx)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	if current.AccountID != coordinator.account ||
		current.Epoch != coordinator.epoch {
		return sandbox.ReconciliationResult{}, ErrSandboxRequest
	}
	reset, err := coordinator.detectAndRecordReset(ctx, current)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	if reset {
		return sandbox.ReconciliationResult{}, ErrSandboxResetDetected
	}
	return coordinator.recordAndReconcile(ctx, current)
}

func (coordinator *SandboxRecoveryCoordinator) detectAndRecordReset(
	ctx context.Context,
	current sandbox.AccountSnapshot,
) (bool, error) {
	previous, found, err := coordinator.store.LatestAccountSnapshot(
		ctx,
		coordinator.account,
		coordinator.epoch,
	)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	incident, reset, err := DetectSandboxReset(previous, current)
	if err != nil || !reset {
		return reset, err
	}
	if err = coordinator.store.RecordAccountReset(ctx, incident); err != nil {
		return false, err
	}
	return true, nil
}

func (coordinator *SandboxRecoveryCoordinator) recordAndReconcile(
	ctx context.Context,
	current sandbox.AccountSnapshot,
) (sandbox.ReconciliationResult, error) {
	if err := coordinator.store.RecordAccountSnapshot(
		ctx,
		"binance-testnet-snapshot-"+
			strconv.FormatInt(current.ObservedAt.UnixNano(), 10)+"-"+
			current.SnapshotHash,
		current,
	); err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	result, err := coordinator.reconciler.ReconcileSnapshot(
		ctx,
		coordinator.account,
		coordinator.epoch,
		current,
	)
	if err != nil || result.State != "clean" {
		if err != nil {
			return sandbox.ReconciliationResult{}, err
		}
		return sandbox.ReconciliationResult{}, ErrSandboxRequest
	}
	return result, nil
}
