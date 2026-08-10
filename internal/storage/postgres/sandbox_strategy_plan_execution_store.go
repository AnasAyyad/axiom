package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

const strategyPlanExecutionSQL = `
SELECT outbox.order_id,outbox.side,outbox.quantity::text,outbox.state,
       outbox.order_state,outbox.updated_at,
       fill.native_fill_id_hash::text,fill.canonical_fill,fill.occurred_at
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
JOIN sandbox_runtime_submission_plans plan
  ON plan.id=journal.plan_id AND plan.sandbox_session_id=parent.id
JOIN sandbox_runtime_submission_outbox outbox
  ON outbox.plan_id=plan.id
LEFT JOIN LATERAL (
  SELECT native_fill_id_hash,canonical_fill,occurred_at
  FROM sandbox_runtime_exchange_fills
  WHERE account_id=outbox.account_id AND account_epoch=outbox.account_epoch
    AND order_id=outbox.order_id AND occurred_at<=$13
  ORDER BY occurred_at DESC,native_fill_id_hash DESC
  LIMIT 1
) fill ON TRUE
WHERE journal.strategy_session_id=$1
  AND journal.plan_id=$14
  AND journal.account_id=$2
  AND journal.account_epoch=$3
  AND journal.strategy_revision=$4
  AND journal.decision_id=$15
  AND journal.event_ordinal=$16
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
  AND arm.expires_at>$13`

// StrategyPlanExecution returns an exact terminal fill snapshot for one
// strategy journal entry. Its SQL joins through the journal instead of the
// account, so a manually-created order or another strategy can never become
// position evidence for this session.
func (store *SandboxRuntimeDispatcherStore) StrategyPlanExecution(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	entry sandbox.StrategyDecisionJournalEntry,
	now time.Time,
) (sandbox.StrategyPlanExecution, error) {
	if store == nil || ctx == nil || entry.ValidFor(work, now) != nil || entry.PlanID == "" {
		return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_invalid")
	}
	row, err := store.loadStrategyPlanExecution(ctx, work, entry, now)
	if err != nil || row.outboxState != "TERMINAL" || !terminalStrategyOrderState(row.orderState) ||
		row.updatedAt.IsZero() || row.updatedAt.Location() != time.UTC || row.updatedAt.After(now) {
		return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_unavailable")
	}
	requested, err := domain.ParseQuantity(row.quantity)
	if err != nil {
		return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_invalid")
	}
	zero, _ := domain.ParseQuantity("0")
	result := sandbox.StrategyPlanExecution{PlanID: entry.PlanID, Side: domain.Side(row.side),
		RequestedQuantity: requested, CumulativeQuantity: zero, ObservedAt: now}
	if row.fillAt != nil {
		if row.fillHash == nil || *row.fillHash == "" || row.fillAt.IsZero() || row.fillAt.Location() != time.UTC ||
			row.fillAt.After(now) {
			return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_invalid")
		}
		var event execution.OrderEvent
		if json.Unmarshal(row.raw, &event) != nil || event.OrderID.String() != row.orderID ||
			event.OccurredAt.IsZero() || event.OccurredAt.Location() != time.UTC ||
			event.OccurredAt.After(now) {
			return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_invalid")
		}
		result.CumulativeQuantity = event.CumulativeQuantity
		result.Fills = append([]execution.FillFact(nil), event.Fills...)
	}
	result.EvidenceHash = sandbox.StrategyPlanExecutionEvidenceHash(result)
	if result.ValidFor(entry, now) != nil {
		return sandbox.StrategyPlanExecution{}, fmt.Errorf("strategy_plan_execution_invalid")
	}
	return result, nil
}

type strategyPlanExecutionRow struct {
	orderID, side, quantity, outboxState, orderState string
	updatedAt                                        time.Time
	fillHash                                         *string
	raw                                              []byte
	fillAt                                           *time.Time
}

func (store *SandboxRuntimeDispatcherStore) loadStrategyPlanExecution(ctx context.Context,
	work sandbox.StrategySessionWork, entry sandbox.StrategyDecisionJournalEntry, now time.Time,
) (strategyPlanExecutionRow, error) {
	var row strategyPlanExecutionRow
	err := store.pool.QueryRow(ctx, strategyPlanExecutionSQL,
		work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		work.Strategy, work.Instrument, work.SessionRevision, work.ConfigurationID,
		work.ConfigurationHash, work.StrategySetHash, work.ArmID, work.ArmRevision,
		now, entry.PlanID, entry.Evidence.DecisionID, entry.Evidence.EventOrdinal,
	).Scan(&row.orderID, &row.side, &row.quantity, &row.outboxState, &row.orderState, &row.updatedAt,
		&row.fillHash, &row.raw, &row.fillAt)
	return row, err
}

func terminalStrategyOrderState(state string) bool {
	switch state {
	case "FILLED", "CANCELED", "REJECTED", "EXPIRED":
		return true
	default:
		return false
	}
}

var _ sandbox.StrategyPlanExecutionSource = (*SandboxRuntimeDispatcherStore)(nil)
