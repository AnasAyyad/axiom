package bybit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// Snapshot returns the authoritative Demo wallet/order/execution snapshot.
func (adapter *SandboxAdapter) Snapshot(
	ctx context.Context,
) (sandbox.AccountSnapshot, error) {
	if err := adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		20,
		demoRequestReconcile,
	); err != nil {
		return sandbox.AccountSnapshot{}, err
	}
	balances, allOrders, allExecutions, err :=
		adapter.loadDemoSnapshotFacts(ctx)
	if err != nil {
		return sandbox.AccountSnapshot{}, err
	}
	return adapter.buildDemoSnapshot(balances, allOrders, allExecutions)
}

func (adapter *SandboxAdapter) loadDemoSnapshotFacts(
	ctx context.Context,
) (
	[]sandbox.Balance,
	[]demoOrderPayload,
	[]demoExecutionPayload,
	error,
) {
	walletBody, err := retryDemoSnapshotRead(ctx, func() ([]byte, error) {
		return adapter.client.walletBalance(ctx)
	})
	if err != nil {
		return nil, nil, nil,
			fmt.Errorf("bybit_demo_snapshot_wallet_request_failed: %w", err)
	}
	balances, err := normalizeDemoBalances(walletBody)
	if err != nil {
		return nil, nil, nil,
			fmt.Errorf("bybit_demo_snapshot_wallet_invalid: %w", err)
	}
	allOrders := make([]demoOrderPayload, 0)
	allExecutions := make([]demoExecutionPayload, 0)
	for _, instrument := range approvedInstruments() {
		orders, executions, factsErr :=
			adapter.loadDemoInstrumentSnapshotFacts(ctx, instrument)
		if factsErr != nil {
			return nil, nil, nil, factsErr
		}
		allOrders = append(allOrders, orders...)
		allExecutions = append(allExecutions, executions...)
	}
	return balances, allOrders, allExecutions, nil
}

func (adapter *SandboxAdapter) loadDemoInstrumentSnapshotFacts(
	ctx context.Context,
	instrument domain.Instrument,
) ([]demoOrderPayload, []demoExecutionPayload, error) {
	orders, err := retryDemoSnapshotRead(
		ctx,
		func() ([]demoOrderPayload, error) {
			return adapter.completeOrderHistory(ctx, instrument.Symbol())
		},
	)
	if err != nil {
		return nil, nil,
			fmt.Errorf("bybit_demo_snapshot_order_history_invalid: %w", err)
	}
	executions, err := retryDemoSnapshotRead(
		ctx,
		func() ([]demoExecutionPayload, error) {
			return adapter.completeExecutionHistory(
				ctx, instrument.Symbol(), "", "",
			)
		},
	)
	if err != nil {
		return nil, nil,
			fmt.Errorf("bybit_demo_snapshot_execution_history_invalid: %w", err)
	}
	return orders, executions, nil
}

func retryDemoSnapshotRead[T any](
	ctx context.Context,
	read func() (T, error),
) (T, error) {
	delays := [...]time.Duration{
		250 * time.Millisecond,
		time.Second,
	}
	var result T
	var err error
	for attempt := 0; attempt <= len(delays); attempt++ {
		result, err = read()
		if err == nil || !errors.Is(err, ErrDemoRequest) {
			return result, err
		}
		if attempt == len(delays) {
			break
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return result, ErrDemoRequest
		case <-timer.C:
		}
	}
	return result, err
}

func (adapter *SandboxAdapter) buildDemoSnapshot(
	balances []sandbox.Balance,
	allOrders []demoOrderPayload,
	allExecutions []demoExecutionPayload,
) (sandbox.AccountSnapshot, error) {
	ordersHash := canonicalDemoOrdersHash(allOrders)
	fillsHash := canonicalDemoExecutionsHash(allExecutions)
	snapshot := sandbox.AccountSnapshot{
		AccountID: adapter.identity.AccountID, Epoch: adapter.epoch,
		Balances: balances, OrdersHash: ordersHash, FillsHash: fillsHash,
		ObservedAt: adapter.client.now().UTC(),
	}
	snapshot.SnapshotHash = canonicalDemoHash(struct {
		AccountID  sandbox.AccountID `json:"account_id"`
		Epoch      uint64            `json:"epoch"`
		Balances   []sandbox.Balance `json:"balances"`
		OrdersHash string            `json:"orders_hash"`
		FillsHash  string            `json:"fills_hash"`
	}{
		AccountID: snapshot.AccountID, Epoch: snapshot.Epoch,
		Balances: balances, OrdersHash: ordersHash, FillsHash: fillsHash,
	})
	if snapshot.Validate() != nil {
		return sandbox.AccountSnapshot{}, ErrDemoPayload
	}
	return snapshot, nil
}

// Reconcile compares the authoritative Demo snapshot with the durable expected
// state and quarantines any difference.
func (adapter *SandboxAdapter) Reconcile(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) (sandbox.ReconciliationResult, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch {
		return sandbox.ReconciliationResult{}, ErrDemoRequest
	}
	actual, err := adapter.Snapshot(ctx)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	return adapter.ReconcileSnapshot(ctx, account, epoch, actual)
}

// ReconcileSnapshot compares one already loaded authoritative Demo snapshot
// with durable local state without repeating the wallet/history read.
func (adapter *SandboxAdapter) ReconcileSnapshot(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	actual sandbox.AccountSnapshot,
) (sandbox.ReconciliationResult, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch ||
		actual.Validate() != nil || actual.AccountID != account ||
		actual.Epoch != epoch {
		return sandbox.ReconciliationResult{}, ErrDemoRequest
	}
	expected, found, err := adapter.expectations.SnapshotExpectation(
		ctx,
		account,
		epoch,
	)
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	differences := demoSnapshotDifferences(expected, found, actual)
	state := "clean"
	if len(differences) != 0 {
		state = "quarantined"
	}
	evidenceHash := canonicalDemoHash(struct {
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
		ID:        "bybit-reconciliation-" + evidenceHash[:20],
		AccountID: account, AccountEpoch: epoch, State: state,
		Differences: differences, EvidenceHash: evidenceHash,
		ReconciledAt: adapter.client.now().UTC(),
	}
	if result.Validate() != nil {
		return sandbox.ReconciliationResult{}, ErrDemoPayload
	}
	return result, nil
}

func (adapter *SandboxAdapter) resolveSubmission(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.Submission, bool, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch ||
		clientOrderID == "" {
		return sandbox.Submission{}, false, ErrDemoRequest
	}
	submission, found, err := adapter.lookup.SubmissionByClientOrderID(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return sandbox.Submission{}, found, err
	}
	if submission.AccountID != account ||
		submission.AccountEpoch != epoch ||
		submission.ClientOrderID != clientOrderID {
		return sandbox.Submission{}, false, ErrDemoRequest
	}
	return submission, true, nil
}

func (adapter *SandboxAdapter) availableBalance(
	ctx context.Context,
	asset domain.AssetSymbol,
) (domain.Balance, error) {
	body, err := adapter.client.walletBalance(ctx)
	if err != nil {
		return domain.Balance{}, err
	}
	balances, err := normalizeDemoBalances(body)
	if err != nil {
		return domain.Balance{}, err
	}
	for _, balance := range balances {
		if balance.Asset == asset {
			return balance.Available, nil
		}
	}
	return domain.Balance{}, ErrDemoPayload
}

func demoSnapshotDifferences(
	expected sandbox.SnapshotExpectation,
	found bool,
	actual sandbox.AccountSnapshot,
) []sandbox.Difference {
	if !found {
		return []sandbox.Difference{{
			Category:       "snapshot",
			Classification: "local_expectation_missing",
			ExpectedHash:   canonicalDemoHash("missing_local_expectation"),
			ActualHash:     actual.SnapshotHash,
			Critical:       true,
		}}
	}
	result := make([]sandbox.Difference, 0, 3)
	appendDifference := func(category, expectedHash, actualHash string) {
		if expectedHash != actualHash {
			result = append(result, sandbox.Difference{
				Category:       category,
				Classification: "authoritative_mismatch",
				ExpectedHash:   expectedHash,
				ActualHash:     actualHash,
				Critical:       true,
			})
		}
	}
	appendDifference("snapshot", expected.SnapshotHash, actual.SnapshotHash)
	appendDifference("orders", expected.OrdersHash, actual.OrdersHash)
	appendDifference("fills", expected.FillsHash, actual.FillsHash)
	return result
}

func canonicalDemoOrdersHash(orders []demoOrderPayload) string {
	unique := make(map[string]demoOrderPayload, len(orders))
	for _, order := range orders {
		unique[order.Symbol+"|"+order.OrderID] = order
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make([]demoOrderPayload, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, unique[key])
	}
	return canonicalDemoHash(canonical)
}

func canonicalDemoExecutionsHash(
	executions []demoExecutionPayload,
) string {
	unique := make(map[string]demoExecutionPayload, len(executions))
	for _, execution := range executions {
		unique[execution.ExecutionID] = execution
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make([]demoExecutionPayload, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, unique[key])
	}
	return canonicalDemoHash(canonical)
}
