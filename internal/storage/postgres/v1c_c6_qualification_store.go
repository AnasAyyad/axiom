package postgres

import (
	"context"
	"encoding/json"
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
	runID            string
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
	store.runID = identity.RunID
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
	accounts := sample.Accounts
	if accounts == nil {
		accounts = []c6.AccountObservation{}
	}
	encodedAccounts, err := json.Marshal(accounts)
	if err != nil {
		return fmt.Errorf("c6_account_observations_encode_failed")
	}
	_, err = store.pool.Exec(ctx, c6InsertSampleSQL,
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
func (store *V1CC6QualificationStore) AppendRecoveryEvent(
	ctx context.Context,
	event c6.RecoveryEvent,
) error {
	if event.RunID == "" || event.AccountID == "" || event.AccountEpoch == 0 ||
		event.Exchange == "" || event.Environment == "" || event.Event == "" ||
		event.State == "" ||
		(event.IncidentSource != "reconciliation" &&
			event.IncidentSource != "private_stream") ||
		event.FailureKind == "" || event.CauseCode == "" ||
		event.DeadlineAt.IsZero() || event.OccurredAt.IsZero() ||
		event.OccurredAt.Location() != time.UTC || len(event.EvidenceHash) != 64 {
		return fmt.Errorf("c6_recovery_event_invalid")
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO v1c_c6_recovery_events(
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
	if err := store.observeC6AccountDetails(ctx, now, sample); err != nil {
		return err
	}
	if cycles > baselineRestarts {
		sample.Restarts = uint64(cycles - baselineRestarts)
	}
	var runtimeHealthy bool
	err = store.pool.QueryRow(ctx, c6ObserveRuntimeSQL, started, now).Scan(
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

func (store *V1CC6QualificationStore) observeC6AccountDetails(
	ctx context.Context,
	now time.Time,
	sample *c6.Sample,
) error {
	store.mutex.Lock()
	started := store.started
	store.mutex.Unlock()
	rows, err := store.pool.Query(ctx, c6ObserveAccountDetailsSQL, now, started)
	if err != nil {
		return err
	}
	defer rows.Close()
	accounts := make([]c6.AccountObservation, 0, 2)
	allFresh, allLeases, allSafe := true, true, true
	active := 0
	for rows.Next() {
		var account c6.AccountObservation
		var runtimeSucceeded bool
		var runtimeAt *time.Time
		var firstIncidentKind, firstFailureKind, firstCause string
		var firstIncidentAt *time.Time
		var latestIncidentKind, latestFailureKind, latestCause string
		var latestIncidentAt *time.Time
		var runtimeFailureCount int
		var runtimeHasTerminalFailure bool
		var terminalKind, terminalFailureKind, terminalCause string
		var terminalAt, reconnectAt, firstCleanAt, secondCleanAt *time.Time
		if err = rows.Scan(
			&account.ID, &account.Exchange, &account.Environment, &account.Epoch,
			&account.State, &account.StreamHealthy, &account.EvidenceHealthy,
			&account.LeaseHeld, &account.ReconciliationClean,
			&runtimeSucceeded, &runtimeAt,
			&firstIncidentKind, &firstFailureKind, &firstCause, &firstIncidentAt,
			&latestIncidentKind, &latestFailureKind, &latestCause, &latestIncidentAt,
			&runtimeFailureCount, &runtimeHasTerminalFailure,
			&terminalKind, &terminalFailureKind, &terminalCause, &terminalAt,
			&reconnectAt, &firstCleanAt, &secondCleanAt,
		); err != nil {
			return err
		}
		account.AccountSafe = account.State == "DEGRADED" ||
			account.State == "READY_PAUSED"
		account.RecoveryState = "not_required"
		if firstIncidentAt != nil &&
			permittedC6RecoveryFailure(firstFailureKind) {
			deadline := firstIncidentAt.UTC().Add(2 * time.Minute)
			account.DeadlineAt = &deadline
			account.RecoveryEvents = append(account.RecoveryEvents,
				c6AccountRecoveryEvent(
					"detected", "active", firstIncidentKind,
					firstFailureKind, firstCause, deadline, 0,
					firstIncidentAt.UTC(), nil,
				),
			)
			if firstCleanAt != nil {
				account.CleanCheckCount = 1
				account.RecoveryEvents = append(account.RecoveryEvents,
					c6AccountRecoveryEvent(
						"first_clean_check", "active", firstIncidentKind,
						firstFailureKind, firstCause, deadline, 1,
						firstCleanAt.UTC(), nil,
					),
				)
			}
		}
		if runtimeHasTerminalFailure && runtimeFailureCount <= 1 {
			state, event := "unrecoverable", "unrecoverable"
			if terminalCause == "recovery_deadline_exceeded" {
				state, event = "expired", "expired"
			}
			account.RecoveryState, account.RecoveryEvent = state, event
			account.IncidentSource = c6RecoverySource(terminalKind)
			account.FailureKind = stableC6FailureKind(terminalFailureKind)
			account.CauseCode = stableC6Cause(terminalCause)
			deadline := now.UTC()
			if account.DeadlineAt != nil {
				deadline = account.DeadlineAt.UTC()
			} else if terminalAt != nil {
				deadline = terminalAt.UTC().Add(2 * time.Minute)
				account.DeadlineAt = &deadline
			}
			occurred := now.UTC()
			if terminalAt != nil {
				occurred = terminalAt.UTC()
			}
			account.RecoveryEvents = append(account.RecoveryEvents,
				c6AccountRecoveryEvent(
					event, state, terminalKind, account.FailureKind,
					account.CauseCode, deadline, account.CleanCheckCount,
					occurred, nil,
				),
			)
		} else if runtimeFailureCount > 1 {
			account.RecoveryState = "repeated"
			account.RecoveryEvent = "repeated"
			account.IncidentSource = c6RecoverySource(latestIncidentKind)
			account.FailureKind = stableC6FailureKind(latestFailureKind)
			account.CauseCode = stableC6Cause(latestCause)
			deadline := now.UTC()
			if account.DeadlineAt != nil {
				deadline = account.DeadlineAt.UTC()
			} else if latestIncidentAt != nil {
				deadline = latestIncidentAt.UTC().Add(2 * time.Minute)
				account.DeadlineAt = &deadline
			}
			occurred := now.UTC()
			if latestIncidentAt != nil {
				occurred = latestIncidentAt.UTC()
			}
			account.RecoveryEvents = append(account.RecoveryEvents,
				c6AccountRecoveryEvent(
					"repeated", "repeated", latestIncidentKind,
					account.FailureKind, account.CauseCode, deadline,
					account.CleanCheckCount, occurred, nil,
				),
			)
		} else if runtimeFailureCount == 1 && firstIncidentAt != nil {
			account.IncidentSource = c6RecoverySource(firstIncidentKind)
			account.FailureKind = stableC6FailureKind(firstFailureKind)
			account.CauseCode = stableC6Cause(firstCause)
			deadline := firstIncidentAt.UTC().Add(2 * time.Minute)
			account.DeadlineAt = &deadline
			streamRecovered := account.StreamHealthy &&
				(account.IncidentSource != "private_stream" || reconnectAt != nil)
			if secondCleanAt != nil && account.State == "READY_PAUSED" &&
				streamRecovered && account.EvidenceHealthy &&
				account.LeaseHeld && account.AccountSafe && runtimeSucceeded {
				account.RecoveryState, account.RecoveryEvent = "recovered", "recovered"
				account.CleanCheckCount = 2
				recoveredAt := secondCleanAt.UTC()
				account.RecoveryTimestamp = &recoveredAt
				account.RecoveryEvents = append(account.RecoveryEvents,
					c6AccountRecoveryEvent(
						"recovered", "recovered", firstIncidentKind,
						account.FailureKind, account.CauseCode, deadline, 2,
						recoveredAt, &recoveredAt,
					),
				)
			} else if !now.Before(deadline) {
				account.RecoveryState, account.RecoveryEvent = "expired", "expired"
				if firstCleanAt != nil {
					account.CleanCheckCount = 1
				}
				account.RecoveryEvents = append(account.RecoveryEvents,
					c6AccountRecoveryEvent(
						"expired", "expired", firstIncidentKind,
						account.FailureKind, "recovery_deadline_exceeded",
						deadline, account.CleanCheckCount, deadline, nil,
					),
				)
			} else if account.State == "DEGRADED" {
				account.RecoveryState = "active"
				account.RecoveryEvent = "detected"
				if firstCleanAt != nil {
					account.RecoveryEvent = "first_clean_check"
					account.CleanCheckCount = 1
				}
			} else {
				account.RecoveryState, account.RecoveryEvent = "unrecoverable", "unrecoverable"
				account.FailureKind = "validation_rejected"
				account.CauseCode = "recovery_state_not_degraded"
				account.RecoveryEvents = append(account.RecoveryEvents,
					c6AccountRecoveryEvent(
						"unrecoverable", "unrecoverable", firstIncidentKind,
						account.FailureKind, account.CauseCode, deadline,
						account.CleanCheckCount, now.UTC(), nil,
					),
				)
			}
		}
		if account.IncidentSource == "private_stream" &&
			account.RecoveryState == "active" {
			account.StreamHealthy = reconnectAt != nil
		}
		if account.RecoveryState == "active" ||
			account.RecoveryState == "expired" ||
			account.RecoveryState == "repeated" ||
			account.RecoveryState == "unrecoverable" {
			account.ReconciliationClean = false
		}
		streamAllowed := account.StreamHealthy ||
			(account.IncidentSource == "private_stream" &&
				account.CleanCheckCount == 0)
		allowedActive := account.RecoveryState == "active" &&
			account.State == "DEGRADED" && streamAllowed &&
			account.EvidenceHealthy && account.LeaseHeld && account.AccountSafe &&
			now.Before(account.DeadlineAt.UTC()) &&
			(account.FailureKind == "transient_outage" ||
				account.FailureKind == "maintenance")
		fresh := account.State == "READY_PAUSED" &&
			account.StreamHealthy && account.EvidenceHealthy &&
			account.LeaseHeld && account.ReconciliationClean &&
			runtimeSucceeded && runtimeAt != nil &&
			!runtimeAt.After(now) && now.Sub(runtimeAt.UTC()) <= 2*time.Minute
		if allowedActive {
			active++
			fresh = true
		}
		allFresh = allFresh && fresh
		allLeases = allLeases && account.LeaseHeld
		allSafe = allSafe && account.AccountSafe && account.StreamHealthy &&
			account.EvidenceHealthy && account.LeaseHeld
		accounts = append(accounts, account)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	sample.Accounts = accounts
	sample.RecoveryActive = active > 0
	sample.AllAccountsFresh = len(accounts) == 2 && allFresh
	sample.AllLeasesHeld = len(accounts) == 2 && allLeases
	sample.EntrySafe = len(accounts) == 2 && allSafe
	return nil
}

func c6RecoverySource(runtimeKind string) string {
	if runtimeKind == "PRIVATE_STREAM" || runtimeKind == "PRIVATE_RECONNECT" {
		return "private_stream"
	}
	return "reconciliation"
}

func permittedC6RecoveryFailure(kind string) bool {
	return kind == "transient_outage" || kind == "maintenance"
}

func stableC6FailureKind(kind string) string {
	if kind == "" {
		return "validation_rejected"
	}
	return kind
}

func stableC6Cause(cause string) string {
	if cause == "" {
		return "untyped_failure"
	}
	return cause
}

func c6AccountRecoveryEvent(
	event, state, runtimeKind, failureKind, causeCode string,
	deadline time.Time,
	cleanChecks uint8,
	occurredAt time.Time,
	recoveryTimestamp *time.Time,
) c6.AccountRecoveryEvent {
	return c6.AccountRecoveryEvent{
		Event: event, State: state,
		IncidentSource: c6RecoverySource(runtimeKind),
		FailureKind:    stableC6FailureKind(failureKind),
		CauseCode:      stableC6Cause(causeCode), DeadlineAt: deadline.UTC(),
		CleanCheckCount: cleanChecks, RecoveryTimestamp: recoveryTimestamp,
		OccurredAt: occurredAt.UTC(),
	}
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
