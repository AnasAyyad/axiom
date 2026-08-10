package postgres

import (
	"context"
	"fmt"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const insertStrategySessionEvaluationSQL = `
INSERT INTO sandbox_strategy_session_evaluations(
  id,strategy_session_id,account_id,account_epoch,strategy_revision,
  state,reason,evidence_hash,occurred_at
)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9
WHERE EXISTS(
  SELECT 1
  FROM sandbox_strategy_sessions strategy
  JOIN sandbox_runtime_sandbox_sessions parent
    ON parent.id=strategy.sandbox_session_id
  JOIN configuration_versions configuration
    ON configuration.id=parent.configuration_id
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=strategy.id
   AND membership.account_id=$3
   AND membership.account_epoch=$4
  JOIN sandbox_runtime_sandbox_arms arm
    ON arm.sandbox_session_id=parent.id
  WHERE strategy.id=$2
    AND strategy.strategy_id=$10
    AND strategy.instrument=$11
    AND strategy.state='running'
    AND strategy.revision=$5
    AND parent.state='ARMED'
    AND parent.revision=$12
    AND parent.configuration_id=$13
    AND configuration.configuration_hash=$14
    AND parent.strategy_set_hash=$15
    AND arm.id=$16
    AND arm.revision=$17
    AND arm.revoked_at IS NULL
    AND arm.expires_at>$9
)
ON CONFLICT DO NOTHING`

// RecordStrategySessionEvaluation appends one owner-visible scheduler outcome.
// The write rechecks the current engine lease and every session/arm revision;
// a stale scheduler cannot report a result for a revoked or replaced session.
func (store *SandboxRuntimeDispatcherStore) RecordStrategySessionEvaluation(
	ctx context.Context,
	owner string,
	fence uint64,
	evaluation sandbox.StrategySessionEvaluation,
) error {
	work, now := evaluation.Work, evaluation.OccurredAt
	if store == nil || ctx == nil || owner == "" || fence == 0 ||
		work.ValidAt(now) != nil || evaluation.ValidFor(work, now) != nil {
		return fmt.Errorf("strategy_session_evaluation_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("strategy_session_evaluation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, work.Account.ID, owner, fence, now); err != nil {
		return fmt.Errorf("strategy_session_evaluation_fence_invalid")
	}
	id := "strategy-evaluation-" + evaluation.EvidenceHash[:32]
	tag, err := tx.Exec(ctx, insertStrategySessionEvaluationSQL,
		id, work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		evaluation.State, evaluation.Reason, evaluation.EvidenceHash, now,
		work.Strategy, work.Instrument, work.SessionRevision, work.ConfigurationID,
		work.ConfigurationHash, work.StrategySetHash, work.ArmID, work.ArmRevision,
	)
	if err != nil {
		return fmt.Errorf("strategy_session_evaluation_write_failed")
	}
	if tag.RowsAffected() == 0 {
		var recorded bool
		err = tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM sandbox_strategy_session_evaluations
  WHERE id=$1 AND strategy_session_id=$2 AND account_id=$3
    AND account_epoch=$4 AND strategy_revision=$5 AND state=$6
    AND reason=$7 AND evidence_hash=$8 AND occurred_at=$9
)`, id, work.SessionID, work.Account.ID, work.Account.Epoch,
			work.StrategyRevision, evaluation.State, evaluation.Reason,
			evaluation.EvidenceHash, now).Scan(&recorded)
		if err != nil || !recorded {
			return fmt.Errorf("strategy_session_evaluation_stale")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("strategy_session_evaluation_commit_failed")
	}
	return nil
}

var _ sandbox.StrategySessionEvaluationRecorder = (*SandboxRuntimeDispatcherStore)(nil)
