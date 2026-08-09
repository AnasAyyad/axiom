package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/cockroachdb/apd/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SandboxRuntimeDispatcherStore is the durable dispatcher recovery submission/inbox boundary.
type SandboxRuntimeDispatcherStore struct{ pool *pgxpool.Pool }

// NewSandboxRuntimeDispatcherStore constructs the durable sandbox dispatch repository.
func NewSandboxRuntimeDispatcherStore(pool *pgxpool.Pool) (*SandboxRuntimeDispatcherStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("sandbox_runtime_dispatcher_pool_missing")
	}
	return &SandboxRuntimeDispatcherStore{pool: pool}, nil
}

// ApprovePlan atomically persists a capped plan, reservations, and fenced outbox.
func (store *SandboxRuntimeDispatcherStore) ApprovePlan(
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
	total, policyHash, err := prepareSandboxRuntimePlan(plan, limits)
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateSandboxRuntimePlanReferences(ctx, tx, plan); err != nil {
		return err
	}
	if err = reserveSandboxRuntimePlanCapacity(ctx, tx, plan, limits, total); err != nil {
		return err
	}
	if err = insertSandboxRuntimePlan(ctx, tx, plan, policyHash); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_plan_commit_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterPlanCommit)
}

func prepareSandboxRuntimePlan(
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
) (*apd.Decimal, string, error) {
	maximumOrder, err := domain.ParseNotional(limits.MaximumOrderNotional)
	entry, actionErr := sandbox.RequiresEntryArm(plan)
	if err != nil || actionErr != nil ||
		validateSandboxRuntimePlanHeader(plan, limits, entry) != nil {
		return nil, "", fmt.Errorf("sandbox_runtime_plan_policy_rejected")
	}
	if _, err = sandbox.ValidatePlanSaga(plan); err != nil {
		return nil, "", fmt.Errorf("sandbox_runtime_plan_saga_rejected")
	}
	if err = plan.Pipeline.ValidateFor(plan); err != nil {
		return nil, "", fmt.Errorf("sandbox_runtime_plan_approval_pipeline_rejected")
	}
	return sumSandboxRuntimePlanLegs(plan, maximumOrder)
}

func validateSandboxRuntimePlanHeader(
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	entry bool,
) error {
	if limits.MaximumOrderNotional != "10" || limits.MaximumDailyNotional != "50" ||
		limits.MaximumOpenPerAccount != 1 || limits.MaximumOpenGlobal != 2 ||
		len(plan.Submissions) == 0 || len(plan.Submissions) > 3 ||
		len(plan.Submissions) != len(plan.Reservations) ||
		plan.ID == "" || plan.SessionID == "" || plan.ApprovalHash == "" ||
		plan.ConfigurationID == "" || plan.ApprovedAt.IsZero() ||
		plan.ApprovedAt.Location() != time.UTC || plan.Arm.SessionID != plan.SessionID ||
		plan.Arm.Validate() != nil ||
		(entry && !plan.Arm.Active(plan.ApprovedAt)) ||
		(!entry && len(plan.EntrySafety) != 0) {
		return fmt.Errorf("sandbox_runtime_plan_policy_rejected")
	}
	return nil
}

func sumSandboxRuntimePlanLegs(
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
			return nil, "", fmt.Errorf("sandbox_runtime_plan_submission_rejected")
		}
		if _, armed := armedAccounts[submission.AccountID]; !armed {
			return nil, "", fmt.Errorf("sandbox_runtime_plan_account_not_armed")
		}
		reservation := plan.Reservations[index]
		if reservation.ValidateFor(submission) != nil {
			return nil, "", fmt.Errorf("sandbox_runtime_reservation_rejected")
		}
		value, _, parseErr := apd.NewFromString(submission.Notional.String())
		if parseErr != nil {
			return nil, "", fmt.Errorf("sandbox_runtime_plan_submission_rejected")
		}
		if _, addErr := apd.BaseContext.Add(total, total, value); addErr != nil {
			return nil, "", fmt.Errorf("sandbox_runtime_plan_submission_rejected")
		}
	}
	return total, policyHash, nil
}

func reserveSandboxRuntimePlanCapacity(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
	total *apd.Decimal,
) error {
	policy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_saga_rejected")
	}
	initial := plan.Submissions
	if policy == execution.DispatchSequential {
		initial = plan.Submissions[:1]
	}
	if err = validateSandboxRuntimeOpenCapacity(ctx, tx, initial, limits); err != nil {
		return err
	}
	day := plan.ApprovedAt.Format("2006-01-02")
	if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_runtime_daily_cap_counters(utc_day,reserved_notional,revision,updated_at)
VALUES ($1,0,1,$2) ON CONFLICT (utc_day) DO NOTHING`, day, plan.ApprovedAt); err != nil {
		return fmt.Errorf("sandbox_runtime_daily_cap_initialize_failed")
	}
	var reserved string
	if err = tx.QueryRow(ctx, `
UPDATE sandbox_runtime_daily_cap_counters
SET reserved_notional=reserved_notional+$2,revision=revision+1,updated_at=$3
WHERE utc_day=$1 AND reserved_notional+$2<=50
RETURNING reserved_notional::text`, day, total.Text('f'), plan.ApprovedAt).Scan(&reserved); err != nil {
		return fmt.Errorf("sandbox_runtime_daily_cap_rejected")
	}
	return nil
}

func validateSandboxRuntimeOpenCapacity(ctx context.Context, tx pgx.Tx, initial []sandbox.Submission,
	limits sandbox.SubmissionLimits,
) error {
	var openGlobal int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_submission_outbox
WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`).Scan(&openGlobal); err != nil ||
		openGlobal+len(initial) > limits.MaximumOpenGlobal {
		return fmt.Errorf("sandbox_runtime_global_open_cap")
	}
	seen := make(map[sandbox.AccountID]struct{}, len(initial))
	for _, submission := range initial {
		if _, exists := seen[submission.AccountID]; exists {
			return fmt.Errorf("sandbox_runtime_account_open_cap")
		}
		seen[submission.AccountID] = struct{}{}
		var openAccount int
		if err := tx.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_submission_outbox
WHERE account_id=$1 AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
			submission.AccountID).Scan(&openAccount); err != nil || openAccount != 0 {
			return fmt.Errorf("sandbox_runtime_account_open_cap")
		}
	}
	return nil
}

func insertSandboxRuntimePlan(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	policyHash string,
) error {
	dispatchPolicy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_saga_rejected")
	}
	if err = insertSandboxRuntimePlanHeader(ctx, tx, plan, policyHash, dispatchPolicy); err != nil {
		return err
	}
	if err = insertSandboxRuntimePlanEligibility(ctx, tx, plan); err != nil {
		return err
	}
	if err = insertSandboxRuntimePlanEntrySafety(ctx, tx, plan); err != nil {
		return err
	}
	if err = insertSandboxRuntimePlanAccountSnapshots(ctx, tx, plan); err != nil {
		return err
	}
	if err = insertSandboxRuntimeStrategyPlanDecision(ctx, tx, plan); err != nil {
		return err
	}
	for index, submission := range plan.Submissions {
		if err = insertSandboxRuntimePlanLeg(
			ctx, tx, plan, index, submission, plan.Reservations[index],
		); err != nil {
			return err
		}
	}
	return nil
}

func insertSandboxRuntimeStrategyPlanDecision(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	if plan.Pipeline.IntentKind != sandbox.ApprovalStrategyIntent {
		return nil
	}
	evidence := plan.StrategyDecision
	if evidence == nil {
		return nil
	}
	if evidence.ValidForPlan(plan) != nil {
		return fmt.Errorf("sandbox_runtime_strategy_plan_decision_invalid")
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_strategy_plan_decisions(
  plan_id,sandbox_session_id,account_id,account_epoch,strategy,instrument,
  decision_id,event_ordinal,event_logical_time,input_hash,decision_hash,
  canonical_input,canonical_decision
)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13
WHERE EXISTS (
  SELECT 1 FROM sandbox_runtime_submission_plans
  WHERE id=$1 AND sandbox_session_id=$2 AND intent_kind='STRATEGY'
)`,
		plan.ID, evidence.SessionID, evidence.AccountID, evidence.AccountEpoch,
		evidence.Strategy, evidence.Instrument, evidence.DecisionID,
		evidence.EventOrdinal, evidence.EventLogicalTime, evidence.InputHash,
		evidence.DecisionHash, []byte(evidence.CanonicalInput), []byte(evidence.CanonicalDecision))
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_strategy_plan_decision_insert_failed")
	}
	return insertSandboxStrategyPlanDecision(ctx, tx, plan, *evidence)
}
