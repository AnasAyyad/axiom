package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// insertSandboxStrategyPlanDecision records the accepted decision in the
// complete strategy journal inside the same transaction as its immutable
// durable plan. The NULL-plan writer is intentionally separate and may never
// upgrade a previous record after the fact.
func insertSandboxStrategyPlanDecision(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	evidence sandbox.StrategyDecisionEvidence,
) error {
	id := "strategy-decision-" + evidence.DecisionHash[:32]
	tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_strategy_decisions(
  id,strategy_session_id,plan_id,account_id,account_epoch,strategy_revision,
  strategy,instrument,decision_id,event_ordinal,event_logical_time,
  input_hash,decision_hash,canonical_input,canonical_decision,occurred_at
)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16
WHERE EXISTS(
  SELECT 1 FROM sandbox_strategy_sessions strategy_session
  WHERE strategy_session.id=$2
    AND strategy_session.sandbox_session_id=$2
    AND strategy_session.strategy_id=$7
    AND strategy_session.instrument=$8
    AND strategy_session.revision=$6
)
`, id, evidence.SessionID, plan.ID, evidence.AccountID, evidence.AccountEpoch,
		evidence.StrategyRevision, evidence.Strategy, evidence.Instrument,
		evidence.DecisionID, evidence.EventOrdinal, evidence.EventLogicalTime,
		evidence.InputHash, evidence.DecisionHash, []byte(evidence.CanonicalInput),
		[]byte(evidence.CanonicalDecision), plan.ApprovedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_strategy_plan_decision_insert_failed")
	}
	return nil
}

func insertSandboxRuntimePlanAccountSnapshots(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	accounts := make([]string, 0, len(plan.AccountSnapshots))
	for account := range plan.AccountSnapshots {
		accounts = append(accounts, string(account))
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		reference := plan.AccountSnapshots[sandbox.AccountID(account)]
		if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_plan_account_snapshots(
  plan_id,account_id,account_epoch,snapshot_hash,observed_at
) VALUES ($1,$2,$3,$4,$5)`,
			plan.ID, reference.AccountID, reference.AccountEpoch,
			reference.SnapshotHash, reference.ObservedAt); err != nil {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_insert_failed")
		}
	}
	return nil
}

func insertSandboxRuntimePlanHeader(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	policyHash string,
	dispatchPolicy execution.DispatchPolicy,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_submission_plans(
  id,sandbox_session_id,arm_id,approval_hash,intent_kind,intent_hash,
  allocator_hash,risk_hash,planner_hash,asset_approval_hash,policy_hash,
  configuration_id,leg_count,dispatch_policy,state,approved_at,
  execution_expires_at,saga_revision,revision
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'APPROVED',$15,$16,1,1
)`,
		plan.ID, plan.SessionID, plan.Arm.ID, plan.ApprovalHash,
		plan.Pipeline.IntentKind, plan.Pipeline.IntentHash,
		plan.Pipeline.AllocatorHash, plan.Pipeline.RiskHash,
		plan.Pipeline.PlannerHash, plan.Pipeline.AssetApprovalHash,
		policyHash, plan.ConfigurationID, len(plan.Submissions), dispatchPolicy,
		plan.ApprovedAt, plan.ExecutionExpiresAt); err != nil {
		return fmt.Errorf("sandbox_runtime_plan_insert_failed")
	}
	return nil
}

func insertSandboxRuntimePlanEligibility(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	type marketSnapshot struct {
		exchange   string
		instrument string
		snapshot   sandbox.EligibilitySnapshot
	}
	markets := make([]marketSnapshot, 0, len(plan.Eligibility)+len(plan.MarketEligibility))
	for exchange := range plan.Eligibility {
		snapshot := plan.Eligibility[exchange]
		markets = append(markets, marketSnapshot{exchange: string(exchange),
			instrument: snapshot.Instrument, snapshot: snapshot})
	}
	for _, snapshot := range plan.MarketEligibility {
		markets = append(markets, marketSnapshot{exchange: snapshot.Exchange,
			instrument: snapshot.Instrument, snapshot: snapshot})
	}
	sort.Slice(markets, func(left, right int) bool {
		if markets[left].exchange != markets[right].exchange {
			return markets[left].exchange < markets[right].exchange
		}
		return markets[left].instrument < markets[right].instrument
	})
	for index, market := range markets {
		if market.exchange == "" || market.instrument == "" ||
			(index > 0 && markets[index-1].exchange == market.exchange &&
				markets[index-1].instrument == market.instrument) {
			return fmt.Errorf("sandbox_runtime_plan_eligibility_invalid")
		}
		encoded, encodeErr := json.Marshal(market.snapshot)
		if encodeErr != nil {
			return fmt.Errorf("sandbox_runtime_plan_eligibility_encode_failed")
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_plan_eligibility(plan_id,exchange,instrument,snapshot,observed_at)
VALUES ($1,$2,$3,$4,$5)`,
			plan.ID, market.exchange, market.instrument, string(encoded),
			market.snapshot.ObservedAt); err != nil {
			return fmt.Errorf("sandbox_runtime_plan_eligibility_insert_failed")
		}
	}
	return nil
}

func insertSandboxRuntimePlanEntrySafety(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	accounts := make([]string, 0, len(plan.EntrySafety))
	for account := range plan.EntrySafety {
		accounts = append(accounts, string(account))
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		snapshot := plan.EntrySafety[sandbox.AccountID(account)]
		if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_plan_entry_safety(
  plan_id,account_id,account_epoch,exchange,state,arm_active,
  global_integration_enabled,global_submission_enabled,
  exchange_integration_enabled,exchange_submission_enabled,
  public_eligible,private_stream_healthy,account_state_fresh,
  reconciliation_clean,lease_held,evidence_healthy,
  open_capacity_available,daily_capacity_available,observed_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19
)`,
			plan.ID,
			snapshot.AccountID,
			snapshot.AccountEpoch,
			snapshot.Exchange,
			snapshot.State,
			snapshot.ArmActive,
			snapshot.GlobalIntegrationEnabled,
			snapshot.GlobalSubmissionEnabled,
			snapshot.ExchangeIntegrationEnabled,
			snapshot.ExchangeSubmissionEnabled,
			snapshot.PublicEligible,
			snapshot.PrivateStreamHealthy,
			snapshot.AccountStateFresh,
			snapshot.ReconciliationClean,
			snapshot.LeaseHeld,
			snapshot.EvidenceHealthy,
			snapshot.OpenCapacityAvailable,
			snapshot.DailyCapacityAvailable,
			snapshot.ObservedAt,
		); err != nil {
			return fmt.Errorf("sandbox_runtime_plan_entry_safety_insert_failed")
		}
	}
	return nil
}

func insertSandboxRuntimePlanLeg(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	index int,
	submission sandbox.Submission,
	reservation sandbox.DurableReservation,
) error {
	policy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_saga_rejected")
	}
	reservationState := sandbox.ReservationActive
	if policy == execution.DispatchSequential && index > 0 {
		reservationState = sandbox.ReservationWaiting
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_reservations(
  id,plan_id,account_id,account_epoch,order_id,asset_symbol,quantity,state,created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		reservation.ID, plan.ID, reservation.AccountID, reservation.AccountEpoch,
		reservation.OrderID, reservation.Asset, reservation.Quantity,
		reservationState, plan.ApprovedAt); err != nil {
		return fmt.Errorf("sandbox_runtime_reservation_insert_failed")
	}
	outboxID := fmt.Sprintf("%s-%02d", plan.ID, index)
	state := sandbox.OutboxPending
	var dependsOn any
	if policy == execution.DispatchSequential && index > 0 {
		state = sandbox.OutboxWaiting
		dependsOn = index - 1
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_submission_outbox(
  id,plan_id,account_id,account_epoch,order_id,client_order_id,strategy_id,
  instrument,side,quantity,limit_price,reserved_notional,order_style,intent_action,
  request_hash,policy_hash,leg_index,depends_on_leg_index,state,order_state,
  approved_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,'APPROVED',$20,$20)`,
		outboxID, plan.ID, submission.AccountID, submission.AccountEpoch,
		submission.OrderID.String(), submission.ClientOrderID, submission.StrategyID.String(),
		submission.Instrument.Symbol(), submission.Side, submission.Quantity.String(),
		submission.LimitPrice.String(), submission.Notional.String(), submission.Style,
		submission.Action, submission.RequestHash, submission.PolicyHash, index,
		dependsOn, state, plan.ApprovedAt); err != nil {
		return fmt.Errorf("sandbox_runtime_outbox_insert_failed")
	}
	return nil
}
