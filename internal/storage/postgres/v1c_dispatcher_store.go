package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/cockroachdb/apd/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// V1CDispatcherStore is the durable C3 submission/inbox boundary.
type V1CDispatcherStore struct{ pool *pgxpool.Pool }

// NewV1CDispatcherStore constructs the durable sandbox dispatch repository.
func NewV1CDispatcherStore(pool *pgxpool.Pool) (*V1CDispatcherStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("v1c_dispatcher_pool_missing")
	}
	return &V1CDispatcherStore{pool: pool}, nil
}

// ApprovePlan atomically persists a capped plan, reservations, and fenced outbox.
func (store *V1CDispatcherStore) ApprovePlan(
	ctx context.Context,
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	kill sandbox.KillPoint,
) error {
	if kill == nil {
		kill = sandbox.NoKillPoint{}
	}
	if err := kill.Hit(ctx, sandbox.KillBeforePlanCommit); err != nil {
		return err
	}
	total, policyHash, err := prepareV1CPlan(plan, limits)
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_plan_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateV1CPlanReferences(ctx, tx, plan); err != nil {
		return err
	}
	if err = reserveV1CPlanCapacity(ctx, tx, plan, limits, total); err != nil {
		return err
	}
	if err = insertV1CPlan(ctx, tx, plan, policyHash); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_plan_commit_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterPlanCommit)
}

func prepareV1CPlan(
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
) (*apd.Decimal, string, error) {
	maximumOrder, err := domain.ParseNotional(limits.MaximumOrderNotional)
	entry, actionErr := sandbox.RequiresEntryArm(plan)
	if err != nil || actionErr != nil ||
		validateV1CPlanHeader(plan, limits, entry) != nil {
		return nil, "", fmt.Errorf("v1c_plan_policy_rejected")
	}
	if _, err = sandbox.ValidatePlanSaga(plan); err != nil {
		return nil, "", fmt.Errorf("v1c_plan_saga_rejected")
	}
	if err = plan.Pipeline.ValidateFor(plan); err != nil {
		return nil, "", fmt.Errorf("v1c_plan_approval_pipeline_rejected")
	}
	return sumV1CPlanLegs(plan, maximumOrder)
}

func validateV1CPlanHeader(
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	entry bool,
) error {
	if limits.MaximumOrderNotional != "10" || limits.MaximumDailyNotional != "50" ||
		limits.MaximumOpenPerAccount != 1 || limits.MaximumOpenGlobal != 2 ||
		len(plan.Submissions) == 0 || len(plan.Submissions) > 2 ||
		len(plan.Submissions) != len(plan.Reservations) ||
		plan.ID == "" || plan.SessionID == "" || plan.ApprovalHash == "" ||
		plan.ConfigurationID == "" || plan.ApprovedAt.IsZero() ||
		plan.ApprovedAt.Location() != time.UTC || plan.Arm.SessionID != plan.SessionID ||
		plan.Arm.Validate() != nil ||
		(entry && !plan.Arm.Active(plan.ApprovedAt)) ||
		(!entry && len(plan.EntrySafety) != 0) {
		return fmt.Errorf("v1c_plan_policy_rejected")
	}
	return nil
}

func sumV1CPlanLegs(
	plan sandbox.ApprovedSandboxPlan,
	maximumOrder domain.Notional,
) (*apd.Decimal, string, error) {
	armedAccounts := make(map[sandbox.AccountID]struct{}, len(plan.Arm.AccountIDs))
	for _, account := range plan.Arm.AccountIDs {
		armedAccounts[account] = struct{}{}
	}
	total := apd.New(0, 0)
	policyHash := plan.Submissions[0].PolicyHash
	for index, submission := range plan.Submissions {
		if err := submission.Validate(maximumOrder); err != nil ||
			submission.ApprovedAt != plan.ApprovedAt ||
			submission.PlanID.String() != plan.ID || submission.PolicyHash != policyHash {
			return nil, "", fmt.Errorf("v1c_plan_submission_rejected")
		}
		if _, armed := armedAccounts[submission.AccountID]; !armed {
			return nil, "", fmt.Errorf("v1c_plan_account_not_armed")
		}
		reservation := plan.Reservations[index]
		if reservation.ValidateFor(submission) != nil {
			return nil, "", fmt.Errorf("v1c_reservation_rejected")
		}
		value, _, parseErr := apd.NewFromString(submission.Notional.String())
		if parseErr != nil {
			return nil, "", fmt.Errorf("v1c_plan_submission_rejected")
		}
		if _, addErr := apd.BaseContext.Add(total, total, value); addErr != nil {
			return nil, "", fmt.Errorf("v1c_plan_submission_rejected")
		}
	}
	return total, policyHash, nil
}

func reserveV1CPlanCapacity(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	total *apd.Decimal,
) error {
	var openGlobal int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM v1c_submission_outbox
WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`).Scan(&openGlobal); err != nil ||
		openGlobal+len(plan.Submissions) > limits.MaximumOpenGlobal {
		return fmt.Errorf("v1c_global_open_cap")
	}
	for _, submission := range plan.Submissions {
		var openAccount int
		if err := tx.QueryRow(ctx, `
SELECT count(*) FROM v1c_submission_outbox
WHERE account_id=$1 AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
			submission.AccountID).Scan(&openAccount); err != nil || openAccount != 0 {
			return fmt.Errorf("v1c_account_open_cap")
		}
	}
	day := plan.ApprovedAt.Format("2006-01-02")
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_daily_cap_counters(utc_day,reserved_notional,revision,updated_at)
VALUES ($1,0,1,$2) ON CONFLICT (utc_day) DO NOTHING`, day, plan.ApprovedAt); err != nil {
		return fmt.Errorf("v1c_daily_cap_initialize_failed")
	}
	var reserved string
	if err := tx.QueryRow(ctx, `
UPDATE v1c_daily_cap_counters
SET reserved_notional=reserved_notional+$2,revision=revision+1,updated_at=$3
WHERE utc_day=$1 AND reserved_notional+$2<=50
RETURNING reserved_notional::text`, day, total.Text('f'), plan.ApprovedAt).Scan(&reserved); err != nil {
		return fmt.Errorf("v1c_daily_cap_rejected")
	}
	return nil
}

func insertV1CPlan(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	policyHash string,
) error {
	dispatchPolicy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil {
		return fmt.Errorf("v1c_plan_saga_rejected")
	}
	if err = insertV1CPlanHeader(ctx, tx, plan, policyHash, dispatchPolicy); err != nil {
		return err
	}
	if err = insertV1CPlanEligibility(ctx, tx, plan); err != nil {
		return err
	}
	if err = insertV1CPlanEntrySafety(ctx, tx, plan); err != nil {
		return err
	}
	for index, submission := range plan.Submissions {
		if err = insertV1CPlanLeg(
			ctx, tx, plan, index, submission, plan.Reservations[index],
		); err != nil {
			return err
		}
	}
	return nil
}

func insertV1CPlanHeader(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	policyHash string,
	dispatchPolicy execution.DispatchPolicy,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_submission_plans(
  id,sandbox_session_id,arm_id,approval_hash,intent_kind,intent_hash,
  allocator_hash,risk_hash,planner_hash,asset_approval_hash,policy_hash,
  configuration_id,leg_count,dispatch_policy,state,approved_at,saga_revision,revision
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'APPROVED',$15,1,1
)`,
		plan.ID, plan.SessionID, plan.Arm.ID, plan.ApprovalHash,
		plan.Pipeline.IntentKind, plan.Pipeline.IntentHash,
		plan.Pipeline.AllocatorHash, plan.Pipeline.RiskHash,
		plan.Pipeline.PlannerHash, plan.Pipeline.AssetApprovalHash,
		policyHash, plan.ConfigurationID, len(plan.Submissions), dispatchPolicy,
		plan.ApprovedAt); err != nil {
		return fmt.Errorf("v1c_plan_insert_failed")
	}
	return nil
}

func insertV1CPlanEligibility(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	exchangeNames := make([]string, 0, len(plan.Eligibility))
	for exchange := range plan.Eligibility {
		exchangeNames = append(exchangeNames, string(exchange))
	}
	sort.Strings(exchangeNames)
	for _, exchange := range exchangeNames {
		snapshot := plan.Eligibility[sandbox.Exchange(exchange)]
		encoded, encodeErr := json.Marshal(snapshot)
		if encodeErr != nil {
			return fmt.Errorf("v1c_plan_eligibility_encode_failed")
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO v1c_plan_eligibility(plan_id,exchange,snapshot,observed_at)
VALUES ($1,$2,$3,$4)`,
			plan.ID, exchange, string(encoded), snapshot.ObservedAt); err != nil {
			return fmt.Errorf("v1c_plan_eligibility_insert_failed")
		}
	}
	return nil
}

func insertV1CPlanEntrySafety(
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
INSERT INTO v1c_plan_entry_safety(
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
			return fmt.Errorf("v1c_plan_entry_safety_insert_failed")
		}
	}
	return nil
}

func insertV1CPlanLeg(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	index int,
	submission sandbox.Submission,
	reservation sandbox.DurableReservation,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_reservations(
  id,plan_id,account_id,account_epoch,order_id,asset_symbol,quantity,state,created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'ACTIVE',$8)`,
		reservation.ID, plan.ID, reservation.AccountID, reservation.AccountEpoch,
		reservation.OrderID, reservation.Asset, reservation.Quantity, plan.ApprovedAt); err != nil {
		return fmt.Errorf("v1c_reservation_insert_failed")
	}
	outboxID := fmt.Sprintf("%s-%02d", plan.ID, index)
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_submission_outbox(
  id,plan_id,account_id,account_epoch,order_id,client_order_id,strategy_id,
  instrument,side,quantity,limit_price,reserved_notional,order_style,intent_action,
  request_hash,policy_hash,state,order_state,approved_at,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'PENDING','APPROVED',$17,$17)`,
		outboxID, plan.ID, submission.AccountID, submission.AccountEpoch,
		submission.OrderID.String(), submission.ClientOrderID, submission.StrategyID.String(),
		submission.Instrument.Symbol(), submission.Side, submission.Quantity.String(),
		submission.LimitPrice.String(), submission.Notional.String(), submission.Style,
		submission.Action, submission.RequestHash, submission.PolicyHash, plan.ApprovedAt); err != nil {
		return fmt.Errorf("v1c_outbox_insert_failed")
	}
	return nil
}
