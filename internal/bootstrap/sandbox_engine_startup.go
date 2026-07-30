package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxEngineStartup struct {
	work        *sandboxEngineRoleWork
	store       *postgresstore.V1CDispatcherStore
	snapshot    config.Snapshot
	attestation sandboxEngineAttestation
	account     postgresstore.V1CEngineAccount
	owner       string
	fence       uint64
	gate        *sandbox.StartupGate
	observation sandbox.StartupObservation
	client      any
	identity    sandbox.AccountIdentity
	adapter     sandboxEngineAdapter
	dispatcher  *sandbox.SandboxDispatcher
	recovery    *sandbox.UnknownRecoveryHarness
	eligibility exchangecontracts.CollectorHealthSnapshot
	source      sandbox.PrivateEventSource
}

func (work *sandboxEngineRoleWork) startSandboxEngine(
	ctx context.Context,
) (*sandboxEngineStartup, error) {
	startup, err := work.newSandboxEngineStartup(ctx)
	if err != nil {
		return nil, err
	}
	stages := []func(context.Context) error{
		startup.acquireLease,
		startup.enterLocked,
		startup.validateBuildAndIdentity,
		startup.recoverDurableState,
		startup.loadExchangeState,
		startup.resolveUnknowns,
		startup.reconcileStartup,
		startup.synchronizeEligibility,
		startup.startPrivateStream,
		startup.enterReadyPaused,
	}
	for _, stage := range stages {
		if err = stage(ctx); err != nil {
			return nil, err
		}
	}
	return startup, nil
}

func (work *sandboxEngineRoleWork) newSandboxEngineStartup(
	ctx context.Context,
) (*sandboxEngineStartup, error) {
	store, err := postgresstore.NewV1CDispatcherStore(work.pool)
	if err != nil {
		return nil, err
	}
	snapshot, err := config.NewSnapshot(
		work.product, config.SourceDefault, "sandbox-engine-startup",
		&domain.SystemClock{},
	)
	if err != nil {
		return nil, err
	}
	attestation, account, err := loadSandboxEngineAttestation(
		work.exchange, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	account, err = store.EnsureAttestedAccount(
		ctx, account, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return &sandboxEngineStartup{
		work: work, store: store, snapshot: snapshot,
		attestation: attestation, account: account,
		owner: work.runtime.InstanceID + "-" + string(work.exchange),
	}, nil
}

func (startup *sandboxEngineStartup) acquireLease(
	ctx context.Context,
) error {
	fence, err := startup.store.AcquireAccountLease(
		ctx, startup.account.AccountID, startup.account.Environment,
		startup.owner, time.Now().UTC(), sandboxEngineLeaseTTL,
		sandbox.NoKillPoint{},
	)
	if err != nil {
		return err
	}
	sink, err := postgresstore.NewV1CEngineStartupEvidenceSink(
		startup.store, startup.account.AccountID,
		startup.work.exchange, fence,
	)
	if err != nil {
		return err
	}
	gate, err := sandbox.NewStartupGate(startup.work.exchange, sink, fence)
	if err != nil {
		return err
	}
	startup.fence = fence
	startup.gate = gate
	startup.observation = sandbox.StartupObservation{
		At: time.Now().UTC(), LeaseHeld: true,
	}
	return startup.complete(sandbox.StartupAcquireLease)
}

func (startup *sandboxEngineStartup) enterLocked(
	ctx context.Context,
) error {
	if err := startup.store.SetEngineAccountState(
		ctx, startup.account.AccountID, sandbox.EngineLocked,
		time.Now().UTC(),
	); err != nil {
		return err
	}
	return startup.complete(sandbox.StartupEnterLocked)
}

func (startup *sandboxEngineStartup) validateBuildAndIdentity(
	ctx context.Context,
) error {
	build := buildinfo.Current()
	startup.observation.BuildValid = build.GoVersion != "" &&
		startup.snapshot.Hash() != "" &&
		startup.work.product.SchemaVersion == config.SchemaVersionV1C
	startup.observation.ConfigurationValid = true
	if err := startup.complete(
		sandbox.StartupValidateBuildConfiguration,
	); err != nil {
		return err
	}
	client, identity, err := startup.work.validateSandboxIdentity(
		ctx, startup.store, startup.snapshot.ID().String(),
		startup.attestation,
	)
	if err != nil {
		return err
	}
	if err = startup.store.RecordValidatedEngineIdentity(ctx, identity); err != nil {
		return err
	}
	startup.client = client
	startup.identity = identity
	startup.observation.CredentialValid = true
	startup.observation.AccountGenerationOK =
		identity.CredentialGeneration == startup.account.CredentialGeneration
	return startup.complete(sandbox.StartupValidateCredentialGeneration)
}

func (startup *sandboxEngineStartup) recoverDurableState(
	ctx context.Context,
) error {
	if _, err := startup.store.RecoverPrivateInbox(
		ctx, startup.account.AccountID, startup.account.Epoch,
		startup.fence,
	); err != nil {
		return err
	}
	active, err := startup.store.ActiveSubmissions(
		ctx, startup.account.AccountID, startup.account.Epoch,
	)
	if err != nil || len(active) > 2 {
		return fmt.Errorf("sandbox_engine_recovery_set_invalid")
	}
	startup.observation.OutboxRecovered = true
	startup.observation.InboxRecovered = true
	return startup.complete(sandbox.StartupRecoverOutboxInbox)
}

func (startup *sandboxEngineStartup) loadExchangeState(
	ctx context.Context,
) error {
	adapter, err := startup.work.buildSandboxAdapter(
		ctx, startup.client, startup.identity,
		startup.account.Epoch, startup.store,
	)
	if err != nil {
		return err
	}
	initial, err := adapter.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err = startup.store.RecordAccountSnapshot(
		ctx, snapshotIdentity(startup.work.exchange, initial), initial,
	); err != nil {
		return err
	}
	dispatcher, recovery, err := newSandboxEngineDispatchers(
		startup.account, startup.owner, startup.fence,
		startup.store, adapter,
	)
	if err != nil {
		return err
	}
	startup.adapter = adapter
	startup.dispatcher = dispatcher
	startup.recovery = recovery
	startup.observation.ExchangeStateLoaded = true
	return startup.complete(sandbox.StartupLoadBalancesOrdersFills)
}

func (startup *sandboxEngineStartup) resolveUnknowns(
	ctx context.Context,
) error {
	if _, err := startup.recovery.RecoverOnce(
		ctx, time.Now().UTC(), 2,
	); err != nil {
		return err
	}
	startup.observation.UnknownResolvedOrQuarantined = true
	return startup.complete(sandbox.StartupResolveUnknownOrders)
}

func (startup *sandboxEngineStartup) reconcileStartup(
	ctx context.Context,
) error {
	result, err := startup.work.reconcile(
		ctx, startup.store, startup.adapter, startup.account,
	)
	if err != nil {
		return fmt.Errorf(
			"sandbox_engine_startup_reconciliation_failed: %w",
			err,
		)
	}
	if result.State != "clean" {
		return fmt.Errorf(
			"sandbox_engine_startup_reconciliation_state_%s",
			result.State,
		)
	}
	if err = startup.store.VerifyEngineRecoveryState(
		ctx, startup.account.AccountID, startup.account.Epoch,
	); err != nil {
		return err
	}
	startup.observation.JournalReconciled = true
	startup.observation.ReservationsReconciled = true
	return startup.complete(
		sandbox.StartupReconcileJournalReservations,
	)
}

func (startup *sandboxEngineStartup) synchronizeEligibility(
	ctx context.Context,
) error {
	eligibility, err := startup.adapter.StartupEligibility(ctx)
	if err != nil || !eligibility.Eligible {
		return fmt.Errorf("sandbox_engine_eligibility_failed")
	}
	startup.eligibility = eligibility
	startup.observation.Eligibility = eligibility
	startup.observation.FiltersSynchronized = true
	return startup.complete(sandbox.StartupSynchronizeFiltersBookClock)
}

func (startup *sandboxEngineStartup) startPrivateStream(
	ctx context.Context,
) error {
	source, err := startup.work.openPrivateSource(
		ctx, startup.client, startup.adapter, startup.store,
	)
	if err != nil {
		return err
	}
	startup.source = source
	startup.observation.PrivateStreamHealthy = true
	return startup.complete(sandbox.StartupStartPrivateStream)
}

func (startup *sandboxEngineStartup) enterReadyPaused(
	ctx context.Context,
) error {
	startup.observation.EvidenceHealthy = true
	if err := startup.complete(
		sandbox.StartupEnterReadyPaused,
	); err != nil {
		return err
	}
	if err := startup.store.SetEngineAccountState(
		ctx, startup.account.AccountID, sandbox.EngineReadyPaused,
		time.Now().UTC(),
	); err != nil {
		return err
	}
	return startup.store.RecordEngineObservation(
		ctx, startup.account.AccountID, startup.account.Epoch,
		startup.work.exchange, startup.fence, startup.eligibility,
	)
}

func (startup *sandboxEngineStartup) complete(
	stage sandbox.StartupStage,
) error {
	startup.observation.At = time.Now().UTC()
	return startup.gate.Complete(stage, startup.observation)
}

func (startup *sandboxEngineStartup) logReady(logger *slog.Logger) {
	logger.Info(
		"sandbox_engine_ready_paused",
		"event_code", "sandbox_engine_ready_paused",
		"exchange", startup.work.exchange,
		"account_id", startup.account.AccountID,
		"account_epoch", startup.account.Epoch,
		"fencing_token", startup.fence,
	)
}

func (startup *sandboxEngineStartup) lockAfterRun() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = startup.store.SetEngineAccountState(
		ctx, startup.account.AccountID, sandbox.EngineLocked,
		time.Now().UTC(),
	)
}
