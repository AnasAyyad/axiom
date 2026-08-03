package postgres

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"axiom/internal/qualification/c6"

	"github.com/jackc/pgx/v5/pgxpool"
)

// V1CC6QualificationStore persists immutable C6 runner evidence and supplies
// redacted database observations. It has no credential or exchange client.
type V1CC6QualificationStore struct {
	pool             *pgxpool.Pool
	mutex            sync.Mutex
	started          time.Time
	baselineRestarts int64
}

// NewV1CC6QualificationStore creates the redacted C6 PostgreSQL observation
// and evidence store.
func NewV1CC6QualificationStore(
	pool *pgxpool.Pool,
) (*V1CC6QualificationStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("c6_qualification_pool_missing")
	}
	return &V1CC6QualificationStore{pool: pool}, nil
}

// QualificationAccounts resolves the two current account epochs without
// exposing credential material.
func (store *V1CC6QualificationStore) QualificationAccounts(
	ctx context.Context,
	configurationHash string,
) ([]c6.AccountIdentity, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,exchange,environment,current_epoch,credential_generation
FROM v1c_exchange_accounts
WHERE exchange IN ('binance','bybit')
ORDER BY exchange,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]c6.AccountIdentity, 0, 2)
	for rows.Next() {
		var account c6.AccountIdentity
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

// Begin creates the immutable C6 run and account-identity rows before
// observation starts.
func (store *V1CC6QualificationStore) Begin(
	ctx context.Context,
	configuration c6.Config,
	started time.Time,
) error {
	var baselineRestarts int64
	if err := store.pool.QueryRow(
		ctx, c6BaselineRestartsSQL,
	).Scan(&baselineRestarts); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	identity := configuration.Identity
	if _, err = tx.Exec(ctx, c6InsertRunSQL,
		identity.RunID, identity.Mode, identity.CommitSHA,
		identity.BuildHash, identity.ExecutableHash,
		nullableText(identity.ImageHash),
		identity.ConfigurationHash, identity.SourceDirty,
		int64(configuration.Duration.Seconds()), started,
	); err != nil {
		return err
	}
	for _, account := range identity.Accounts {
		if _, err = tx.Exec(ctx, c6InsertAccountSQL,
			identity.RunID, account.ID, account.Exchange, account.Environment,
			account.AccountEpoch, account.CredentialGeneration,
			account.ConfigurationHash,
		); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(
		ctx, c6StartRunSQL, identity.RunID, started,
	); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	store.mutex.Lock()
	store.started = started
	store.baselineRestarts = baselineRestarts
	store.mutex.Unlock()
	return nil
}

// AppendSample records one immutable, bounded C6 observation.
func (store *V1CC6QualificationStore) AppendSample(
	ctx context.Context,
	runID string,
	sample c6.Sample,
) error {
	_, err := store.pool.Exec(ctx, c6InsertSampleSQL,
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
		sample.ProductionTargetObserved,
	)
	return err
}

// Finish records failure rows and seals the terminal run verdict.
func (store *V1CC6QualificationStore) Finish(
	ctx context.Context,
	evidence c6.Evidence,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for index, failure := range evidence.Failures {
		if _, err = tx.Exec(ctx, c6InsertFailureSQL,
			fmt.Sprintf("%s-failure-%03d", evidence.Identity.RunID, index+1),
			evidence.Identity.RunID, failure.Reason, failure.EvidenceHash,
			failure.OccurredAt,
		); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, c6FinishRunSQL,
		evidence.Identity.RunID, evidence.State,
		evidence.ObservedDurationSeconds, evidence.Qualified,
		evidence.EndedAt, evidence.EvidenceHash,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Observe reads one redacted C6 safety and SLO sample from PostgreSQL and
// process memory.
func (store *V1CC6QualificationStore) Observe(
	ctx context.Context,
	_ uint64,
	now time.Time,
) (c6.Sample, error) {
	store.mutex.Lock()
	started := store.started
	store.mutex.Unlock()
	if started.IsZero() {
		return c6.Sample{}, fmt.Errorf("c6_observation_not_started")
	}
	sample := c6.Sample{
		AllAccountsFresh: true, AllLeasesHeld: true,
		PersistenceHealthy: true, RestartSafe: true, EntrySafe: true,
	}
	if err := store.observeC6Orders(ctx, started, now, &sample); err != nil {
		return c6.Sample{}, err
	}
	if err := store.observeC6Accounts(ctx, started, now, &sample); err != nil {
		return c6.Sample{}, err
	}
	if err := store.observeC6Reconciliation(
		ctx, started, &sample,
	); err != nil {
		return c6.Sample{}, err
	}
	if err := store.observeC6Targets(ctx, started, &sample); err != nil {
		return c6.Sample{}, err
	}
	if err := store.observeC6AlertLatency(ctx, started, &sample); err != nil {
		return c6.Sample{}, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	sample.ResidentMemoryBytes = memory.Sys
	return sample, nil
}

func (store *V1CC6QualificationStore) observeC6Orders(
	ctx context.Context,
	started, now time.Time,
	sample *c6.Sample,
) error {
	err := store.pool.QueryRow(ctx, c6ObserveOrdersSQL, started, now).Scan(
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
		ctx, c6ObserveDuplicatesSQL, started,
	).Scan(&sample.DuplicateCreates)
}

func (store *V1CC6QualificationStore) observeC6Accounts(
	ctx context.Context,
	started, now time.Time,
	sample *c6.Sample,
) error {
	store.mutex.Lock()
	baselineRestarts := store.baselineRestarts
	store.mutex.Unlock()
	var total, fresh, leases int
	var cycles int64
	err := store.pool.QueryRow(ctx, c6ObserveAccountsSQL,
		now,
	).Scan(&total, &fresh, &leases, &cycles)
	if err != nil {
		return err
	}
	sample.AllAccountsFresh = total == 2 && fresh == total
	sample.AllLeasesHeld = total == 2 && leases == total
	sample.EntrySafe = sample.AllAccountsFresh && sample.AllLeasesHeld
	if cycles > baselineRestarts {
		sample.Restarts = uint64(cycles - baselineRestarts)
	}
	var runtimeHealthy bool
	err = store.pool.QueryRow(ctx, c6ObserveRuntimeSQL, started).Scan(
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

func (store *V1CC6QualificationStore) observeC6Reconciliation(
	ctx context.Context,
	started time.Time,
	sample *c6.Sample,
) error {
	return store.pool.QueryRow(ctx, `
SELECT
 count(*) FILTER (WHERE critical AND state<>'RESOLVED'),
 count(*) FILTER (WHERE state IN ('OPEN','ADJUSTED','QUARANTINED'))
FROM v1c_reconciliation_differences WHERE recorded_at >= $1`,
		started,
	).Scan(&sample.ReconciliationMismatches, &sample.SuspenseItems)
}

func (store *V1CC6QualificationStore) observeC6Targets(
	ctx context.Context,
	started time.Time,
	sample *c6.Sample,
) error {
	return store.pool.QueryRow(ctx, `
SELECT EXISTS(
 SELECT 1 FROM v1c_authenticated_request_evidence
 WHERE recorded_at >= $1
   AND host NOT IN (
     'testnet.binance.vision','ws-api.testnet.binance.vision',
     'api-demo.bybit.com','stream-demo.bybit.com'
   )
)`, started).Scan(&sample.ProductionTargetObserved)
}

func (store *V1CC6QualificationStore) observeC6AlertLatency(
	ctx context.Context,
	started time.Time,
	sample *c6.Sample,
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
	_ c6.Store       = (*V1CC6QualificationStore)(nil)
	_ c6.Probe       = (*V1CC6QualificationStore)(nil)
	_ c6.ChaosSource = (*V1CC6QualificationStore)(nil)
)
