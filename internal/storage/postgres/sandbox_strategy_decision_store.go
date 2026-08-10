package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const insertSandboxStrategyDecisionSQL = `
INSERT INTO sandbox_strategy_decisions(
  id,strategy_session_id,plan_id,account_id,account_epoch,strategy_revision,
  strategy,instrument,decision_id,event_ordinal,event_logical_time,
  input_hash,decision_hash,canonical_input,canonical_decision,occurred_at
)
SELECT $1,$2,NULL,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15
WHERE EXISTS(
  SELECT 1
  FROM sandbox_strategy_sessions strategy_session
  JOIN sandbox_runtime_sandbox_sessions parent
    ON parent.id=strategy_session.sandbox_session_id
  JOIN configuration_versions configuration
    ON configuration.id=parent.configuration_id
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=strategy_session.id
   AND membership.account_id=$3
   AND membership.account_epoch=$4
  JOIN sandbox_runtime_sandbox_arms arm
    ON arm.sandbox_session_id=parent.id
  WHERE strategy_session.id=$2
    AND strategy_session.strategy_id=$6
    AND strategy_session.instrument=$7
    AND strategy_session.state='running'
    AND strategy_session.revision=$5
    AND parent.state='ARMED'
    AND parent.revision=$16
    AND parent.configuration_id=$17
    AND configuration.configuration_hash=$18
    AND parent.strategy_set_hash=$19
    AND arm.id=$20
    AND arm.revision=$21
    AND arm.revoked_at IS NULL
    AND arm.expires_at>$15
)
ON CONFLICT DO NOTHING`

const strategyDecisionJournalSQL = `
SELECT journal.plan_id,journal.account_id,journal.account_epoch,
       journal.strategy_revision,journal.strategy,journal.instrument,
       journal.decision_id,journal.event_ordinal,journal.event_logical_time,
       journal.input_hash::text,journal.decision_hash::text,
       journal.canonical_input,journal.canonical_decision,journal.occurred_at
FROM sandbox_strategy_decisions journal
JOIN sandbox_strategy_sessions strategy_session
  ON strategy_session.id=journal.strategy_session_id
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy_session.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy_session.id
 AND membership.account_id=$2
 AND membership.account_epoch=$3
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE journal.strategy_session_id=$1
  AND journal.account_id=$2
  AND journal.account_epoch=$3
  AND journal.strategy_revision=$4
  AND strategy_session.strategy_id=$5
  AND strategy_session.instrument=$6
  AND strategy_session.state='running'
  AND strategy_session.revision=$4
  AND parent.state='ARMED'
  AND parent.revision=$7
  AND parent.configuration_id=$8
  AND configuration.configuration_hash=$9
  AND parent.strategy_set_hash=$10
  AND arm.id=$11
  AND arm.revision=$12
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$13
  AND journal.occurred_at<=$13
ORDER BY journal.event_ordinal,journal.occurred_at,journal.id`

// RecordSandboxStrategyDecision appends a complete non-order strategy
// decision while the exact fenced account, arm, and immutable work snapshot
// remain current. Accepted decisions that have a durable plan are recorded by
// the plan transaction instead, so the plan reference cannot be patched in.
func (store *SandboxRuntimeDispatcherStore) RecordSandboxStrategyDecision(
	ctx context.Context,
	owner string,
	fence uint64,
	work sandbox.StrategySessionWork,
	evidence sandbox.StrategyDecisionEvidence,
	occurredAt time.Time,
) error {
	if store == nil || ctx == nil || owner == "" || fence == 0 ||
		work.ValidAt(occurredAt) != nil ||
		(sandbox.StrategyDecisionJournalEntry{Evidence: evidence, OccurredAt: occurredAt}).ValidFor(work, occurredAt) != nil {
		return fmt.Errorf("sandbox_strategy_decision_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("sandbox_strategy_decision_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, work.Account.ID, owner, fence, occurredAt); err != nil {
		return fmt.Errorf("sandbox_strategy_decision_fence_invalid")
	}
	id := "strategy-decision-" + evidence.DecisionHash[:32]
	tag, err := tx.Exec(ctx, insertSandboxStrategyDecisionSQL,
		id, work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		work.Strategy, work.Instrument, evidence.DecisionID, evidence.EventOrdinal,
		evidence.EventLogicalTime, evidence.InputHash, evidence.DecisionHash,
		[]byte(evidence.CanonicalInput), []byte(evidence.CanonicalDecision), occurredAt,
		work.SessionRevision, work.ConfigurationID, work.ConfigurationHash,
		work.StrategySetHash, work.ArmID, work.ArmRevision,
	)
	if err != nil {
		return fmt.Errorf("sandbox_strategy_decision_write_failed")
	}
	if tag.RowsAffected() == 0 {
		if !sandboxStrategyDecisionRecorded(ctx, tx, id, work, evidence, occurredAt) {
			return fmt.Errorf("sandbox_strategy_decision_stale")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_strategy_decision_commit_failed")
	}
	return nil
}

func sandboxStrategyDecisionRecorded(ctx context.Context, tx pgx.Tx, id string,
	work sandbox.StrategySessionWork, evidence sandbox.StrategyDecisionEvidence, occurredAt time.Time,
) bool {
	var recorded bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM sandbox_strategy_decisions
  WHERE id=$1 AND strategy_session_id=$2 AND plan_id IS NULL
    AND account_id=$3 AND account_epoch=$4 AND strategy_revision=$5
    AND decision_id=$6 AND event_ordinal=$7 AND event_logical_time=$8
    AND input_hash=$9 AND decision_hash=$10 AND occurred_at=$11
)`, id, work.SessionID, work.Account.ID, work.Account.Epoch,
		work.StrategyRevision, evidence.DecisionID, evidence.EventOrdinal,
		evidence.EventLogicalTime, evidence.InputHash, evidence.DecisionHash, occurredAt).Scan(&recorded)
	return err == nil && recorded
}

// StrategyDecisionJournal reads only the exact active session's immutable
// decision sequence. It rejects a legacy accepted-plan record that lacks a
// corresponding journal row, because projecting a zero or guessed state over
// that gap could authorize an unowned sell.
func (store *SandboxRuntimeDispatcherStore) StrategyDecisionJournal(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) ([]sandbox.StrategyDecisionJournalEntry, error) {
	if store == nil || ctx == nil || work.ValidAt(now) != nil {
		return nil, fmt.Errorf("sandbox_strategy_decision_journal_invalid")
	}
	rows, err := store.pool.Query(ctx, strategyDecisionJournalSQL,
		work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		work.Strategy, work.Instrument, work.SessionRevision, work.ConfigurationID,
		work.ConfigurationHash, work.StrategySetHash, work.ArmID, work.ArmRevision, now,
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_decision_journal_unavailable")
	}
	defer rows.Close()
	entries, err := scanSandboxStrategyDecisionJournal(rows, work, now)
	if err != nil {
		return nil, err
	}
	var gap bool
	err = store.pool.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM sandbox_runtime_strategy_plan_decisions legacy
  JOIN sandbox_runtime_submission_plans plan ON plan.id=legacy.plan_id
  LEFT JOIN sandbox_strategy_decisions journal ON journal.plan_id=plan.id
  WHERE legacy.sandbox_session_id=$1 AND legacy.account_id=$2
    AND legacy.account_epoch=$3 AND legacy.strategy=$4 AND legacy.instrument=$5
    AND journal.id IS NULL
)`, work.SessionID, work.Account.ID, work.Account.Epoch, work.Strategy, work.Instrument).Scan(&gap)
	if err != nil || gap {
		return nil, fmt.Errorf("sandbox_strategy_decision_journal_incomplete")
	}
	return entries, nil
}

func scanSandboxStrategyDecisionJournal(rows pgx.Rows, work sandbox.StrategySessionWork,
	now time.Time,
) ([]sandbox.StrategyDecisionJournalEntry, error) {
	entries := make([]sandbox.StrategyDecisionJournalEntry, 0)
	var priorOrdinal uint64
	for rows.Next() {
		var planID *string
		entry := sandbox.StrategyDecisionJournalEntry{}
		if err := rows.Scan(&planID, &entry.Evidence.AccountID, &entry.Evidence.AccountEpoch,
			&entry.Evidence.StrategyRevision, &entry.Evidence.Strategy, &entry.Evidence.Instrument,
			&entry.Evidence.DecisionID, &entry.Evidence.EventOrdinal, &entry.Evidence.EventLogicalTime,
			&entry.Evidence.InputHash, &entry.Evidence.DecisionHash,
			&entry.Evidence.CanonicalInput, &entry.Evidence.CanonicalDecision, &entry.OccurredAt); err != nil {
			return nil, fmt.Errorf("sandbox_strategy_decision_journal_invalid")
		}
		entry.Evidence.SessionID = work.SessionID
		if planID != nil {
			entry.PlanID = *planID
		}
		if entry.ValidFor(work, now) != nil || (priorOrdinal != 0 && entry.Evidence.EventOrdinal <= priorOrdinal) {
			return nil, fmt.Errorf("sandbox_strategy_decision_journal_invalid")
		}
		priorOrdinal = entry.Evidence.EventOrdinal
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_decision_journal_unavailable")
	}
	return entries, nil
}

var _ sandbox.StrategyDecisionJournalSource = (*SandboxRuntimeDispatcherStore)(nil)
