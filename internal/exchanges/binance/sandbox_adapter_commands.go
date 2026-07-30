package binance

import (
	"context"
	"errors"

	"axiom/internal/sandbox"
)

// Query resolves the immutable submission and loads any cumulative fills.
func (adapter *SandboxAdapter) Query(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) ([]sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx, account, epoch, clientOrderID,
	)
	if err != nil || !found {
		return nil, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(), 12, sandboxRequestReconcile,
	); err != nil {
		return nil, err
	}
	body, err := adapter.client.query(ctx, submission)
	if errors.Is(err, ErrSandboxOrderNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	event, err := adapter.normalizeOrderWithFills(ctx, body, submission)
	if err != nil {
		return nil, err
	}
	return []sandbox.PrivateEvent{event}, nil
}

// Cancel remains available independently from entry arm and public health.
func (adapter *SandboxAdapter) Cancel(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx, account, epoch, clientOrderID,
	)
	if err != nil || !found {
		return sandbox.PrivateEvent{}, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(), 2, sandboxRequestCancel,
	); err != nil {
		return sandbox.PrivateEvent{}, err
	}
	body, err := adapter.client.cancel(ctx, submission)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	return adapter.normalizeOrderWithFills(ctx, body, submission)
}

// Reconcile compares the exchange-authoritative snapshot with one exact
// durable local expectation and quarantines every mismatch.
func (adapter *SandboxAdapter) Reconcile(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) (sandbox.ReconciliationResult, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch {
		return sandbox.ReconciliationResult{}, ErrSandboxRequest
	}
	actual, err := adapter.Snapshot(ctx)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	return adapter.ReconcileSnapshot(ctx, account, epoch, actual)
}

// ReconcileSnapshot compares one already loaded authoritative Testnet
// snapshot with durable local state without repeating the high-weight history
// read.
func (adapter *SandboxAdapter) ReconcileSnapshot(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	actual sandbox.AccountSnapshot,
) (sandbox.ReconciliationResult, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch ||
		actual.Validate() != nil || actual.AccountID != account ||
		actual.Epoch != epoch {
		return sandbox.ReconciliationResult{}, ErrSandboxRequest
	}
	expected, found, err := adapter.expectations.SnapshotExpectation(
		ctx, account, epoch,
	)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	differences := sandboxSnapshotDifferences(expected, found, actual)
	state := "clean"
	if len(differences) != 0 {
		state = "quarantined"
	}
	reconciledAt := adapter.client.now().UTC()
	evidenceHash := canonicalHash(struct {
		AccountID   sandbox.AccountID           `json:"account_id"`
		Epoch       uint64                      `json:"epoch"`
		Expected    sandbox.SnapshotExpectation `json:"expected"`
		Found       bool                        `json:"found"`
		Actual      sandbox.AccountSnapshot     `json:"actual"`
		Differences []sandbox.Difference        `json:"differences"`
	}{
		AccountID: account, Epoch: epoch, Expected: expected, Found: found,
		Actual: actual, Differences: differences,
	})
	result := sandbox.ReconciliationResult{
		ID:        "binance-reconciliation-" + evidenceHash[:20],
		AccountID: account, AccountEpoch: epoch, State: state,
		Differences: differences, EvidenceHash: evidenceHash,
		ReconciledAt: reconciledAt,
	}
	if result.Validate() != nil {
		return sandbox.ReconciliationResult{}, ErrSandboxPayload
	}
	return result, nil
}
