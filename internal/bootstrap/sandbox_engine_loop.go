package bootstrap

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/exchanges/binance"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func newSandboxEngineDispatchers(
	account postgresstore.V1CEngineAccount,
	owner string,
	fence uint64,
	store *postgresstore.V1CDispatcherStore,
	adapter sandboxEngineAdapter,
) (
	*sandbox.SandboxDispatcher,
	*sandbox.UnknownRecoveryHarness,
	error,
) {
	dispatcher, err := sandbox.NewSandboxDispatcher(
		account.AccountID,
		account.Epoch,
		owner,
		fence,
		store,
		adapter,
		sandbox.NoKillPoint{},
	)
	if err != nil {
		return nil, nil, err
	}
	recovery, err := sandbox.NewUnknownRecoveryHarness(
		account.AccountID,
		account.Epoch,
		owner,
		fence,
		store,
		adapter,
		adapter,
		sandbox.NoKillPoint{},
	)
	if err != nil {
		return nil, nil, err
	}
	return dispatcher, recovery, nil
}

func (work *sandboxEngineRoleWork) reconcile(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	adapter sandboxEngineAdapter,
	account postgresstore.V1CEngineAccount,
) (sandbox.ReconciliationResult, error) {
	var result sandbox.ReconciliationResult
	var err error
	if work.exchange == sandbox.ExchangeBinance {
		coordinator, coordinatorErr :=
			binance.NewSandboxRecoveryCoordinator(
				adapter,
				adapter,
				store,
				account.AccountID,
				account.Epoch,
			)
		if coordinatorErr != nil {
			return sandbox.ReconciliationResult{}, coordinatorErr
		}
		result, err = coordinator.Recover(ctx)
	} else {
		current, snapshotErr := adapter.Snapshot(ctx)
		if snapshotErr != nil {
			return sandbox.ReconciliationResult{}, snapshotErr
		}
		if err = store.RecordAccountSnapshot(
			ctx,
			snapshotIdentity(work.exchange, current),
			current,
		); err != nil {
			return sandbox.ReconciliationResult{}, err
		}
		result, err = adapter.ReconcileSnapshot(
			ctx,
			account.AccountID,
			account.Epoch,
			current,
		)
	}
	if err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	if err = store.RecordReconciliation(ctx, result); err != nil {
		return sandbox.ReconciliationResult{}, err
	}
	return result, nil
}

func snapshotIdentity(
	exchange sandbox.Exchange,
	snapshot sandbox.AccountSnapshot,
) string {
	return string(exchange) + "-sandbox-snapshot-" +
		strconv.FormatInt(snapshot.ObservedAt.UnixNano(), 10) + "-" +
		snapshot.SnapshotHash
}

func (work *sandboxEngineRoleWork) runSandboxEngineLoop(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	account postgresstore.V1CEngineAccount,
	owner string,
	fence uint64,
	adapter sandboxEngineAdapter,
	source sandbox.PrivateEventSource,
	dispatcher *sandbox.SandboxDispatcher,
	recovery *sandbox.UnknownRecoveryHarness,
) error {
	loop := sandboxEngineLoop{
		work: work, store: store, account: account, owner: owner,
		fence: fence, adapter: adapter, dispatcher: dispatcher,
		recovery: recovery,
	}
	return loop.run(ctx, source)
}

type sandboxEngineLoop struct {
	work       *sandboxEngineRoleWork
	store      *postgresstore.V1CDispatcherStore
	account    postgresstore.V1CEngineAccount
	owner      string
	fence      uint64
	adapter    sandboxEngineAdapter
	dispatcher *sandbox.SandboxDispatcher
	recovery   *sandbox.UnknownRecoveryHarness
}

func (loop sandboxEngineLoop) run(
	ctx context.Context,
	source sandbox.PrivateEventSource,
) error {
	tickers := newSandboxEngineTickers()
	defer tickers.stop()
	privateSignals := make(chan sandboxPrivateStreamSignal, 2)
	go loop.work.consumePrivateEvents(
		ctx, loop.store, loop.fence, source, privateSignals,
	)
	health := newSandboxEngineHealth()
	for {
		select {
		case <-ctx.Done():
			return nil
		case signal := <-privateSignals:
			if err := health.observePrivate(ctx, loop, signal); err != nil {
				return err
			}
		case <-tickers.lease.C:
			if err := loop.renewLease(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		case <-tickers.dispatch.C:
			err := loop.dispatch(ctx, health.ready)
			if err = health.observeRuntime(ctx, loop, err); err != nil {
				return err
			}
		case <-tickers.recovery.C:
			err := loop.recover(ctx, health.ready)
			if err = health.observeRuntime(ctx, loop, err); err != nil {
				return err
			}
		case <-tickers.reconcile.C:
			if err := health.reconcile(ctx, loop); err != nil {
				return err
			}
		case <-tickers.eligibility.C:
			if err := health.refreshEligibility(ctx, loop); err != nil {
				return err
			}
		}
	}
}

type sandboxEngineTickers struct {
	lease       *time.Ticker
	dispatch    *time.Ticker
	recovery    *time.Ticker
	reconcile   *time.Ticker
	eligibility *time.Ticker
}

func newSandboxEngineTickers() sandboxEngineTickers {
	return sandboxEngineTickers{
		lease:       time.NewTicker(sandboxEngineLeaseRenewal),
		dispatch:    time.NewTicker(sandboxEngineDispatchInterval),
		recovery:    time.NewTicker(sandboxEngineRecoveryInterval),
		reconcile:   time.NewTicker(sandboxEngineReconcileInterval),
		eligibility: time.NewTicker(time.Second),
	}
}

func (tickers sandboxEngineTickers) stop() {
	tickers.lease.Stop()
	tickers.dispatch.Stop()
	tickers.recovery.Stop()
	tickers.reconcile.Stop()
	tickers.eligibility.Stop()
}

func (loop sandboxEngineLoop) renewLease(ctx context.Context) error {
	return loop.store.RenewAccountLease(
		ctx, loop.account.AccountID, loop.owner, loop.fence,
		time.Now().UTC(), sandboxEngineLeaseTTL,
	)
}

func (loop sandboxEngineLoop) dispatch(
	ctx context.Context,
	eligible bool,
) error {
	if !eligible {
		return nil
	}
	if err := loop.work.processSandboxEngineCommands(
		ctx, loop.store, loop.account, loop.owner, loop.fence,
		loop.adapter, loop.dispatcher,
	); err != nil {
		return err
	}
	if !loop.work.sandboxSubmissionEnabled() {
		return nil
	}
	_, err := loop.dispatcher.DispatchOnce(ctx, time.Now().UTC(), 2)
	return err
}

func (loop sandboxEngineLoop) recover(
	ctx context.Context,
	eligible bool,
) error {
	if !eligible {
		return nil
	}
	_, err := loop.recovery.RecoverOnce(ctx, time.Now().UTC(), 2)
	return err
}

func (loop sandboxEngineLoop) reconcile(
	ctx context.Context,
	eligible bool,
) error {
	if !eligible {
		return nil
	}
	result, err := loop.work.reconcile(
		ctx, loop.store, loop.adapter, loop.account,
	)
	if err != nil {
		return fmt.Errorf("sandbox_engine_reconciliation_failed: %w", err)
	}
	if result.State != "clean" {
		return fmt.Errorf(
			"sandbox_engine_reconciliation_state_%s",
			result.State,
		)
	}
	return nil
}

func (loop sandboxEngineLoop) refreshEligibility(
	ctx context.Context,
	current bool,
) (bool, error) {
	if !loop.work.sandboxSubmissionEnabled() {
		return true, nil
	}
	snapshot, err := loop.adapter.StartupEligibility(ctx)
	if ctx.Err() != nil {
		return current, nil
	}
	if err != nil || !snapshot.Eligible {
		return false, nil
	}
	if err = loop.store.RecordEngineObservation(
		ctx, loop.account.AccountID, loop.account.Epoch,
		loop.work.exchange, loop.fence, snapshot,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (loop sandboxEngineLoop) transitionReadiness(
	ctx context.Context,
	current bool,
	target bool,
) (bool, error) {
	if current == target {
		return current, nil
	}
	if ctx.Err() != nil {
		return current, nil
	}
	loop.work.ready.Store(false)
	state := sandbox.EngineDegraded
	if target {
		state = sandbox.EngineReadyPaused
	}
	if err := loop.store.SetEngineAccountState(
		ctx, loop.account.AccountID, state, time.Now().UTC(),
	); err != nil {
		if ctx.Err() != nil {
			return current, nil
		}
		return current, err
	}
	if target {
		loop.work.ready.Store(true)
	}
	return target, nil
}

func (work *sandboxEngineRoleWork) sandboxSubmissionEnabled() bool {
	if !work.product.Sandbox.IntegrationsEnabled ||
		!work.product.Sandbox.SubmissionEnabled {
		return false
	}
	for _, exchange := range work.product.Sandbox.Exchanges {
		if exchange.ID == string(work.exchange) {
			return exchange.IntegrationEnabled &&
				exchange.SubmissionEnabled
		}
	}
	return false
}
