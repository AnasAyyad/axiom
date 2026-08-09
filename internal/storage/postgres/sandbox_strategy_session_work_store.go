package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// ActiveStrategySessionWork returns the currently runnable automatic strategy
// sessions attached to one exact account epoch. It is intentionally a
// read-only scheduling snapshot: callers must perform the full allocation,
// risk, arm, and dispatcher admission again immediately before creating an
// entry. This prevents a stale worker read from becoming an order authority.
func (store *SandboxRuntimeDispatcherStore) ActiveStrategySessionWork(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]sandbox.StrategySessionWork, error) {
	if account == "" || epoch == 0 || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 16 {
		return nil, fmt.Errorf("sandbox_strategy_session_work_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, account, owner, fence, now); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_fence_invalid")
	}
	rows, err := tx.Query(ctx, activeStrategySessionWorkSQL, account, int64(epoch), now, limit)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_query_failed")
	}
	defer rows.Close()
	items := make([]sandbox.StrategySessionWork, 0, limit)
	for rows.Next() {
		item, scanErr := scanStrategySessionWork(rows, now)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_query_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_commit_failed")
	}
	return items, nil
}

type strategySessionWorkScanner interface{ Scan(...any) error }

func scanStrategySessionWork(scanner strategySessionWorkScanner, now time.Time) (sandbox.StrategySessionWork, error) {
	var item sandbox.StrategySessionWork
	var accountEpoch, sessionRevision, strategyRevision, armRevision int64
	err := scanner.Scan(&item.SessionID, &item.Strategy, &item.Instrument,
		&item.ConfigurationID, &item.ConfigurationHash, &item.StrategySetHash,
		&accountEpoch, &item.Account.ID, &item.Account.Exchange,
		&sessionRevision, &strategyRevision, &item.ArmID, &armRevision,
		&item.StartedAt, &item.ArmExpiresAt)
	if err != nil || accountEpoch <= 0 || sessionRevision <= 0 || strategyRevision <= 0 || armRevision <= 0 {
		return sandbox.StrategySessionWork{}, fmt.Errorf("sandbox_strategy_session_work_scan_failed")
	}
	item.Account.Epoch = uint64(accountEpoch)
	item.SessionRevision = uint64(sessionRevision)
	item.StrategyRevision = uint64(strategyRevision)
	item.ArmRevision = uint64(armRevision)
	// pgx may decode a UTC timestamptz using time.Local when the host's local
	// zone is UTC. Normalize without changing the instant or evidence value.
	item.StartedAt = item.StartedAt.UTC()
	item.ArmExpiresAt = item.ArmExpiresAt.UTC()
	if item.ValidAt(now) != nil {
		return sandbox.StrategySessionWork{}, fmt.Errorf("sandbox_strategy_session_work_invalid")
	}
	return item, nil
}

// StrategySessionAdmission revalidates one scheduled automatic strategy
// snapshot immediately before a future planner may create an entry plan. In
// addition to the normal entry gates, it binds the current child strategy
// session, parent session, and exact arm revision to the original work item.
func (store *SandboxRuntimeDispatcherStore) StrategySessionAdmission(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	approvedAt time.Time,
	switches [4]bool,
) (sandbox.StrategySessionAdmission, error) {
	if work.ValidAt(approvedAt) != nil || approvedAt.IsZero() ||
		approvedAt.Location() != time.UTC {
		return sandbox.StrategySessionAdmission{},
			fmt.Errorf("sandbox_strategy_session_admission_invalid")
	}
	facts, err := store.readStrategySessionAdmissionFacts(ctx, work, approvedAt)
	if err != nil {
		return sandbox.StrategySessionAdmission{}, err
	}
	eligibility, safety, cycle, err := buildCanaryAdmission(
		work.Account.ID, work.Account.Exchange, approvedAt, switches, facts.canaryAdmissionFacts,
	)
	if err != nil {
		return sandbox.StrategySessionAdmission{},
			fmt.Errorf("sandbox_strategy_session_admission_blocked")
	}
	admission := sandbox.StrategySessionAdmission{
		Work: work, Arm: facts.arm(work), Eligibility: eligibility, Safety: safety,
		StartupCycle: cycle, ApprovedAt: approvedAt,
	}
	if admission.Valid() != nil {
		return sandbox.StrategySessionAdmission{},
			fmt.Errorf("sandbox_strategy_session_admission_invalid")
	}
	return admission, nil
}

func (store *SandboxRuntimeDispatcherStore) readStrategySessionAdmissionFacts(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	approvedAt time.Time,
) (strategySessionAdmissionFacts, error) {
	var facts strategySessionAdmissionFacts
	err := store.pool.QueryRow(ctx, strategySessionAdmissionFactsSQL,
		work.Account.ID, work.Account.Exchange, work.Instrument, work.SessionID,
		work.SessionRevision, work.Strategy, work.StrategyRevision,
		work.ConfigurationID, work.ConfigurationHash, work.StrategySetHash,
		work.ArmID, work.ArmRevision, approvedAt, approvedAt.Format("2006-01-02"),
	).Scan(
		&facts.epoch, &facts.state, &facts.cycle, &facts.eligibilityJSON,
		&facts.privateHealthy, &facts.reconciliationClean,
		&facts.evidenceHealthy, &facts.leaseHeld,
		&facts.openAccount, &facts.openGlobal, &facts.dailyAvailable,
		&facts.authorizationID, &facts.actorUserID, &facts.actorSessionID,
		&facts.reasonHash, &facts.armCreatedAt, &facts.armExpiresAt,
		&facts.armRevision, &facts.accountIDs,
	)
	if err != nil || facts.epoch <= 0 || facts.cycle <= 0 {
		return strategySessionAdmissionFacts{},
			fmt.Errorf("sandbox_strategy_session_admission_unavailable")
	}
	return facts, nil
}

type strategySessionAdmissionFacts struct {
	canaryAdmissionFacts
	authorizationID, actorUserID, actorSessionID, reasonHash string
	armCreatedAt, armExpiresAt                               time.Time
	armRevision                                              int64
	accountIDs                                               []string
}

func (facts strategySessionAdmissionFacts) arm(
	work sandbox.StrategySessionWork,
) sandbox.Arm {
	accounts := make([]sandbox.AccountID, 0, len(facts.accountIDs))
	for _, account := range facts.accountIDs {
		accounts = append(accounts, sandbox.AccountID(account))
	}
	return sandbox.Arm{ID: work.ArmID, SessionID: work.SessionID,
		AccountIDs: accounts, AuthorizationHash: stableSandboxRuntimeHash(facts.authorizationID),
		ActorUserID: facts.actorUserID, ActorSessionID: facts.actorSessionID,
		ReasonHash: facts.reasonHash, CreatedAt: facts.armCreatedAt.UTC(),
		ExpiresAt: facts.armExpiresAt.UTC(), Revision: uint64(facts.armRevision)}
}

const activeStrategySessionWorkSQL = `
SELECT strategy.id,strategy.strategy_id,strategy.instrument,
       parent.configuration_id,configuration.configuration_hash,parent.strategy_set_hash,
       membership.account_epoch,membership.account_id,membership.exchange,
       parent.revision,strategy.revision,arm.id,arm.revision,
       strategy.started_at,arm.expires_at
FROM sandbox_strategy_sessions strategy
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy.id
JOIN sandbox_runtime_sandbox_session_accounts parent_membership
  ON parent_membership.session_id=parent.id
 AND parent_membership.account_id=membership.account_id
 AND parent_membership.account_epoch=membership.account_epoch
JOIN sandbox_runtime_exchange_accounts account
  ON account.id=membership.account_id
 AND account.current_epoch=membership.account_epoch
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE strategy.state='running'
  AND parent.state='ARMED'
  AND membership.account_id=$1
  AND membership.account_epoch=$2
  AND arm.revoked_at IS NULL
  AND arm.created_at <= $3
  AND arm.expires_at > $3
ORDER BY strategy.started_at,strategy.id
LIMIT $4`

var _ sandbox.StrategySessionWorkSource = (*SandboxRuntimeDispatcherStore)(nil)
var _ sandbox.StrategySessionAdmissionSource = (*SandboxRuntimeDispatcherStore)(nil)

const strategySessionAdmissionFactsSQL = `
SELECT account.current_epoch,account.state,observation.startup_cycle,
       market_observation.eligibility,
       observation.private_stream_healthy,
       observation.reconciliation_clean,
       observation.evidence_healthy,
       EXISTS(
         SELECT 1 FROM sandbox_runtime_account_leases lease
         WHERE lease.account_id=account.id
           AND lease.fencing_token=observation.startup_cycle
           AND lease.expires_at>$13
       ),
       (SELECT count(*) FROM sandbox_runtime_submission_outbox
        WHERE account_id=account.id
          AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')),
       (SELECT count(*) FROM sandbox_runtime_submission_outbox
        WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')),
       coalesce((
         SELECT reserved_notional<50 FROM sandbox_runtime_daily_cap_counters
         WHERE utc_day=$14
       ),true),
       arm.authorization_id,arm.actor_user_id,arm.actor_session_id,
       arm.reason_hash,arm.created_at,arm.expires_at,arm.revision,
       ARRAY(
         SELECT arm_membership.account_id
         FROM sandbox_runtime_sandbox_session_accounts arm_membership
         WHERE arm_membership.session_id=parent.id
         ORDER BY arm_membership.account_id
       )
FROM sandbox_strategy_sessions strategy
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy.id
JOIN sandbox_runtime_sandbox_session_accounts parent_membership
  ON parent_membership.session_id=parent.id
 AND parent_membership.account_id=membership.account_id
 AND parent_membership.account_epoch=membership.account_epoch
JOIN sandbox_runtime_exchange_accounts account
  ON account.id=membership.account_id
 AND account.current_epoch=membership.account_epoch
JOIN sandbox_runtime_engine_observations observation
  ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
JOIN sandbox_runtime_engine_market_observations market_observation
  ON market_observation.account_id=account.id
 AND market_observation.account_epoch=account.current_epoch
 AND market_observation.instrument=$3
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE account.id=$1
  AND account.exchange=$2
  AND observation.exchange=$2
  AND market_observation.exchange=$2
  AND (market_observation.eligibility->>'eligible')::boolean
  AND strategy.id=$4
  AND parent.state='ARMED'
  AND parent.revision=$5
  AND strategy.state='running'
  AND strategy.strategy_id=$6
  AND strategy.revision=$7
  AND parent.configuration_id=$8
  AND configuration.configuration_hash=$9
  AND parent.strategy_set_hash=$10
  AND arm.id=$11
  AND arm.revision=$12
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$13
  AND market_observation.observed_at<=$13
  AND $13-market_observation.observed_at<=interval '250 milliseconds'`
