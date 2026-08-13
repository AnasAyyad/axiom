package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"axiom/internal/qualification/sandboxqualification"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SandboxQualificationStore persists immutable sandbox qualification runner evidence and supplies
// redacted database observations. It has no credential or exchange client.
type SandboxQualificationStore struct {
	pool             *pgxpool.Pool
	mutex            sync.Mutex
	started          time.Time
	runID            string
	baselineRestarts int64
}

// NewSandboxQualificationStore creates the redacted sandbox qualification PostgreSQL observation
// and evidence store.
func NewSandboxQualificationStore(
	pool *pgxpool.Pool,
) (*SandboxQualificationStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("sandbox_qualification_pool_missing")
	}
	return &SandboxQualificationStore{pool: pool}, nil
}

// QualificationAccounts resolves the two current account epochs without
// exposing credential material.
func (store *SandboxQualificationStore) QualificationAccounts(
	ctx context.Context,
	configurationHash string,
) ([]sandboxQualification.AccountIdentity, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,exchange,environment,current_epoch,credential_generation
FROM sandbox_runtime_exchange_accounts
WHERE exchange IN ('binance','bybit')
ORDER BY exchange,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sandboxQualification.AccountIdentity, 0, 2)
	for rows.Next() {
		var account sandboxQualification.AccountIdentity
		if err = rows.Scan(
			&account.ID, &account.Exchange, &account.Environment,
			&account.AccountEpoch, &account.CredentialGeneration,
		); err != nil {
			return nil, err
		}
		account.ConfigurationHash = configurationHash
		result = append(result, account)
	}
	return result, rows.Err()
}

// Begin creates the immutable sandbox qualification run and account-identity rows before
// observation starts.
func (store *SandboxQualificationStore) Begin(
	ctx context.Context,
	configuration sandboxQualification.Config,
	started time.Time,
) error {
	var baselineRestarts int64
	if err := store.pool.QueryRow(
		ctx, sandboxQualificationBaselineRestartsSQL,
	).Scan(&baselineRestarts); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	identity := configuration.Identity
	if _, err = tx.Exec(ctx, sandboxQualificationInsertRunSQL,
		identity.RunID, identity.Mode, identity.CommitSHA,
		identity.BuildHash, identity.ExecutableHash,
		nullableText(identity.ImageHash),
		identity.ConfigurationHash, identity.SourceDirty,
		int64(configuration.Duration.Seconds()), started,
	); err != nil {
		return err
	}
	for _, account := range identity.Accounts {
		if _, err = tx.Exec(ctx, sandboxQualificationInsertAccountSQL,
			identity.RunID, account.ID, account.Exchange, account.Environment,
			account.AccountEpoch, account.CredentialGeneration,
			account.ConfigurationHash,
		); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(
		ctx, sandboxQualificationStartRunSQL, identity.RunID, started,
	); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	store.mutex.Lock()
	store.started = started
	store.runID = identity.RunID
	store.baselineRestarts = baselineRestarts
	store.mutex.Unlock()
	return nil
}

// AppendSample records one immutable, bounded sandbox qualification observation.
func (store *SandboxQualificationStore) AppendSample(
	ctx context.Context,
	runID string,
	sample sandboxQualification.Sample,
) error {
	accounts := sample.Accounts
	if accounts == nil {
		accounts = []sandboxQualification.AccountObservation{}
	}
	encodedAccounts, err := json.Marshal(accounts)
	if err != nil {
		return fmt.Errorf("sandbox_qualification_account_observations_encode_failed")
	}
	_, err = store.pool.Exec(ctx, sandboxQualificationInsertSampleSQL,
		runID, sample.Ordinal, sample.ObservedAt, sample.OrdersAcknowledged,
		sample.DuplicateCreates, sample.LostFills,
		sample.DoublePostedFills, sample.UnknownOrders,
		sample.OldestUnknownSeconds, sample.ReconciliationMismatches,
		sample.SuspenseItems, sample.Reconnects, sample.Restarts,
		sample.RecoveryDurationMillis, sample.CriticalAlertLatencyMillis,
		sample.ResidentMemoryBytes, sample.DailySubmittedMicrounits,
		sample.LargestOrderMicrounits, sample.MaximumAccountOpen,
		sample.GlobalOpen, sample.AllAccountsFresh, sample.AllLeasesHeld,
		sample.PersistenceHealthy, sample.RestartSafe, sample.EntrySafe,
		sample.ProductionTargetObserved, encodedAccounts,
	)
	return err
}

// AppendRecoveryEvent persists one immutable redacted recovery lifecycle fact.
func (store *SandboxQualificationStore) AppendRecoveryEvent(
	ctx context.Context,
	event sandboxQualification.RecoveryEvent,
) error {
	if event.RunID == "" || event.AccountID == "" || event.AccountEpoch == 0 ||
		event.Exchange == "" || event.Environment == "" || event.Event == "" ||
		event.State == "" ||
		(event.IncidentSource != "reconciliation" &&
			event.IncidentSource != "private_stream") ||
		event.FailureKind == "" || event.CauseCode == "" ||
		event.DeadlineAt.IsZero() || event.OccurredAt.IsZero() ||
		event.OccurredAt.Location() != time.UTC || len(event.EvidenceHash) != 64 {
		return fmt.Errorf("sandbox_qualification_recovery_event_invalid")
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO sandbox_qualification_recovery_events(
 id,run_id,account_id,exchange,environment,account_epoch,event,state,
 incident_source,failure_kind,cause_code,deadline_at,clean_check_count,
 recovery_timestamp,evidence_hash,occurred_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		event.EvidenceHash, event.RunID, event.AccountID, event.Exchange,
		event.Environment, event.AccountEpoch, event.Event, event.State,
		event.IncidentSource, event.FailureKind, event.CauseCode, event.DeadlineAt,
		event.CleanCheckCount, event.RecoveryTimestamp, event.EvidenceHash,
		event.OccurredAt,
	)
	return err
}

// Finish records failure rows and seals the terminal run verdict.
func (store *SandboxQualificationStore) Finish(
	ctx context.Context,
	evidence sandboxQualification.Evidence,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for index, failure := range evidence.Failures {
		if _, err = tx.Exec(ctx, sandboxQualificationInsertFailureSQL,
			fmt.Sprintf("%s-failure-%03d", evidence.Identity.RunID, index+1),
			evidence.Identity.RunID, failure.Reason, failure.EvidenceHash,
			failure.OccurredAt,
		); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, sandboxQualificationFinishRunSQL,
		evidence.Identity.RunID, evidence.State,
		evidence.ObservedDurationSeconds, evidence.Qualified,
		evidence.EndedAt, evidence.EvidenceHash,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Observe reads one redacted sandbox qualification safety and SLO sample from PostgreSQL and
// process memory.
func (store *SandboxQualificationStore) Observe(
	ctx context.Context,
	_ uint64,
	now time.Time,
) (sandboxQualification.Sample, error) {
	store.mutex.Lock()
	started := store.started
	store.mutex.Unlock()
	if started.IsZero() {
		return sandboxQualification.Sample{}, fmt.Errorf("sandbox_qualification_observation_not_started")
	}
	sample := sandboxQualification.Sample{
		AllAccountsFresh: true, AllLeasesHeld: true,
		PersistenceHealthy: true, RestartSafe: true, EntrySafe: true,
	}
	if err := store.observeSandboxQualificationOrders(ctx, started, now, &sample); err != nil {
		return sandboxQualification.Sample{}, err
	}
	if err := store.observeSandboxQualificationAccounts(ctx, started, now, &sample); err != nil {
		return sandboxQualification.Sample{}, err
	}
	if err := store.observeSandboxQualificationReconciliation(
		ctx, started, &sample,
	); err != nil {
		return sandboxQualification.Sample{}, err
	}
	if err := store.observeSandboxQualificationTargets(ctx, started, &sample); err != nil {
		return sandboxQualification.Sample{}, err
	}
	if err := store.observeSandboxQualificationAlertLatency(ctx, started, &sample); err != nil {
		return sandboxQualification.Sample{}, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	sample.ResidentMemoryBytes = memory.Sys
	return sample, nil
}

func (store *SandboxQualificationStore) observeSandboxQualificationOrders(
	ctx context.Context,
	started, now time.Time,
	sample *sandboxQualification.Sample,
) error {
	err := store.pool.QueryRow(ctx, sandboxQualificationObserveOrdersSQL, started, now).Scan(
		&sample.OrdersAcknowledged, &sample.LostFills,
		&sample.DoublePostedFills, &sample.UnknownOrders,
		&sample.OldestUnknownSeconds, &sample.LargestOrderMicrounits,
		&sample.DailySubmittedMicrounits, &sample.MaximumAccountOpen,
		&sample.GlobalOpen,
	)
	if err != nil {
		return err
	}
	return store.pool.QueryRow(
		ctx, sandboxQualificationObserveDuplicatesSQL, started,
	).Scan(&sample.DuplicateCreates)
}

func (store *SandboxQualificationStore) observeSandboxQualificationAccounts(
	ctx context.Context,
	started, now time.Time,
	sample *sandboxQualification.Sample,
) error {
	store.mutex.Lock()
	baselineRestarts := store.baselineRestarts
	store.mutex.Unlock()
	var total, fresh, leases int
	var cycles int64
	err := store.pool.QueryRow(ctx, sandboxQualificationObserveAccountsSQL,
		now,
	).Scan(&total, &fresh, &leases, &cycles)
	if err != nil {
		return err
	}
	sample.AllAccountsFresh = total == 2 && fresh == total
	sample.AllLeasesHeld = total == 2 && leases == total
	sample.EntrySafe = sample.AllAccountsFresh && sample.AllLeasesHeld
	if err := store.observeSandboxQualificationAccountDetails(ctx, now, sample); err != nil {
		return err
	}
	if cycles > baselineRestarts {
		sample.Restarts = uint64(cycles - baselineRestarts)
	}
	var runtimeHealthy bool
	err = store.pool.QueryRow(
		ctx, sandboxQualificationObserveRuntimeSQL, started, now,
	).Scan(
		&sample.Reconnects,
		&sample.RecoveryDurationMillis,
		&runtimeHealthy,
	)
	if err != nil {
		return err
	}
	sample.RestartSafe = runtimeHealthy
	return nil
}

type sandboxQualificationRuntimeIncident struct {
	kind, failureKind, causeCode string
	at                           *time.Time
}

type sandboxQualificationAccountRuntime struct {
	account            sandboxQualification.AccountObservation
	runtimeSucceeded   bool
	runtimeAt          *time.Time
	first              sandboxQualificationRuntimeIncident
	latest             sandboxQualificationRuntimeIncident
	terminal           sandboxQualificationRuntimeIncident
	failureCount       int
	hasTerminalFailure bool
	reconnectAt        *time.Time
	firstCleanAt       *time.Time
	secondCleanAt      *time.Time
}

type sandboxQualificationAccountHealth struct {
	fresh, lease, safe, active bool
}

type sandboxQualificationAccountScanner interface {
	Scan(...any) error
}

func (store *SandboxQualificationStore) observeSandboxQualificationReconciliation(
	ctx context.Context,
	started time.Time,
	sample *sandboxQualification.Sample,
) error {
	return store.pool.QueryRow(ctx, `
SELECT
 count(*) FILTER (WHERE critical AND state<>'RESOLVED'),
 count(*) FILTER (WHERE state IN ('OPEN','ADJUSTED','QUARANTINED'))
FROM sandbox_runtime_reconciliation_differences WHERE recorded_at >= $1`,
		started,
	).Scan(&sample.ReconciliationMismatches, &sample.SuspenseItems)
}

func (store *SandboxQualificationStore) observeSandboxQualificationTargets(
	ctx context.Context,
	started time.Time,
	sample *sandboxQualification.Sample,
) error {
	return store.pool.QueryRow(ctx, `
SELECT EXISTS(
 SELECT 1 FROM sandbox_runtime_authenticated_request_evidence
 WHERE recorded_at >= $1
   AND host NOT IN (
     'testnet.binance.vision','ws-api.testnet.binance.vision',
     'api-demo.bybit.com','stream-demo.bybit.com'
   )
)`, started).Scan(&sample.ProductionTargetObserved)
}

func (store *SandboxQualificationStore) observeSandboxQualificationAlertLatency(
	ctx context.Context,
	started time.Time,
	sample *sandboxQualification.Sample,
) error {
	var latency *int64
	err := store.pool.QueryRow(ctx, `
SELECT max((extract(epoch FROM (alert.created_at-incident.opened_at))*1000)::bigint)
FROM alerts alert JOIN incidents incident ON incident.id=alert.incident_id
WHERE alert.created_at >= $1`, started).Scan(&latency)
	if err != nil {
		return err
	}
	if latency != nil && *latency > 0 {
		sample.CriticalAlertLatencyMillis = uint64(*latency)
	}
	return nil
}

var (
	_ sandboxQualification.Store       = (*SandboxQualificationStore)(nil)
	_ sandboxQualification.Probe       = (*SandboxQualificationStore)(nil)
	_ sandboxQualification.ChaosSource = (*SandboxQualificationStore)(nil)
)
