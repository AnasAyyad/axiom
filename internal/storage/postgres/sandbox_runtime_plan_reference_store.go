package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func validateSandboxRuntimePlanReferences(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	entry, err := sandbox.RequiresEntryArm(plan)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_action_rejected")
	}
	actorUser, actorSession, err := validateSandboxRuntimeSessionAndArm(ctx, tx, plan, entry)
	if err != nil {
		return err
	}
	if entry && validateSandboxRuntimeArmActor(ctx, tx, actorUser, actorSession, plan.ApprovedAt) != nil {
		return fmt.Errorf("sandbox_runtime_arm_session_invalid")
	}
	exchanges, err := validateSandboxRuntimePlanAccounts(ctx, tx, plan, entry)
	if err != nil {
		return err
	}
	if err = sandbox.ValidateSubmissionTopology(plan.Submissions, exchanges); err != nil {
		return fmt.Errorf("sandbox_runtime_plan_topology_rejected")
	}
	if plan.Pipeline.IntentKind == sandbox.ApprovalStrategyIntent &&
		plan.Submissions[0].Action != sandbox.IntentRecovery {
		if validateSandboxRuntimePlanAccountSnapshots(ctx, tx, plan) != nil ||
			validateSandboxRuntimeInitialReservationOwnership(ctx, tx, plan) != nil {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
		}
	}
	if err = validateSandboxRuntimeStrategyPlanDecision(ctx, tx, plan); err != nil {
		return err
	}
	return nil
}

// validateSandboxRuntimeInitialReservationOwnership independently proves that every leg
// exposed at plan approval fits the exact exchange-authoritative available
// balance bound to the plan. Sequential dependent reservations are excluded:
// their input must be activated later from the prior authoritative fill.
func validateSandboxRuntimeInitialReservationOwnership(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	policy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
	}
	initial := len(plan.Submissions)
	if policy == execution.DispatchSequential {
		initial = 1
	}
	available := make(map[sandboxRuntimeBalanceKey]domain.Balance, initial)
	required := make(map[sandboxRuntimeBalanceKey]domain.Balance, initial)
	for index := 0; index < initial; index++ {
		submission := plan.Submissions[index]
		reservation := plan.Reservations[index]
		reference, exists := plan.AccountSnapshots[submission.AccountID]
		if !exists {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
		}
		asset, assetErr := domain.ParseAssetSymbol(reservation.Asset)
		quantity, quantityErr := domain.ParseBalance(reservation.Quantity)
		if assetErr != nil || quantityErr != nil {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
		}
		key := sandboxRuntimeBalanceKey{account: submission.AccountID, asset: asset}
		if prior, found := required[key]; found {
			quantity, err = prior.Add(quantity)
			if err != nil {
				return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
			}
		}
		required[key] = quantity
		if _, loaded := available[key]; loaded {
			continue
		}
		balance, loadErr := loadSandboxRuntimeReferencedBalance(ctx, tx, reference, asset)
		if loadErr != nil {
			return loadErr
		}
		available[key] = balance
	}
	if !sandboxRuntimeReservationBalancesSufficient(available, required) {
		return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
	}
	return nil
}

func sandboxRuntimeReservationBalancesSufficient(available, required map[sandboxRuntimeBalanceKey]domain.Balance) bool {
	for key, quantity := range required {
		balance, exists := available[key]
		if !exists || balance.Compare(quantity) < 0 {
			return false
		}
	}
	return true
}

type sandboxRuntimeBalanceKey struct {
	account sandbox.AccountID
	asset   domain.AssetSymbol
}

func loadSandboxRuntimeReferencedBalance(ctx context.Context, tx pgx.Tx, reference sandbox.AccountSnapshotReference,
	asset domain.AssetSymbol,
) (domain.Balance, error) {
	var encoded []byte
	err := tx.QueryRow(ctx, `
SELECT balances_payload::text
FROM sandbox_runtime_account_snapshots
WHERE account_id=$1 AND account_epoch=$2 AND snapshot_hash=$3
  AND observed_at=$4`, reference.AccountID, reference.AccountEpoch,
		reference.SnapshotHash, reference.ObservedAt).Scan(&encoded)
	if err != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
	}
	var balances []sandbox.Balance
	if json.Unmarshal(encoded, &balances) != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
	}
	for _, balance := range balances {
		if balance.Asset == asset {
			return balance.Available, nil
		}
	}
	return domain.Balance{}, fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
}

func validateSandboxRuntimeStrategyPlanDecision(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	if plan.Pipeline.IntentKind != sandbox.ApprovalStrategyIntent {
		if plan.StrategyDecision != nil {
			return fmt.Errorf("sandbox_runtime_strategy_plan_decision_rejected")
		}
		return nil
	}
	var automatic bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM sandbox_strategy_sessions WHERE id=$1)`, plan.SessionID,
	).Scan(&automatic); err != nil {
		return fmt.Errorf("sandbox_runtime_strategy_plan_decision_read_failed")
	}
	if automatic && (plan.StrategyDecision == nil || plan.StrategyDecision.ValidForPlan(plan) != nil) {
		return fmt.Errorf("sandbox_runtime_strategy_plan_decision_rejected")
	}
	if !automatic && plan.StrategyDecision != nil && plan.StrategyDecision.ValidForPlan(plan) != nil {
		return fmt.Errorf("sandbox_runtime_strategy_plan_decision_rejected")
	}
	return nil
}

func validateSandboxRuntimePlanAccountSnapshots(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	for _, submission := range plan.Submissions {
		reference, exists := plan.AccountSnapshots[submission.AccountID]
		if !exists || reference.ValidateFor(
			submission.AccountID, submission.AccountEpoch, plan.ApprovedAt,
		) != nil {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
		}
		var recorded bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM sandbox_runtime_account_snapshots
  WHERE account_id=$1 AND account_epoch=$2 AND snapshot_hash=$3
    AND observed_at=$4
)`, submission.AccountID, submission.AccountEpoch,
			reference.SnapshotHash, reference.ObservedAt).Scan(&recorded); err != nil || !recorded {
			return fmt.Errorf("sandbox_runtime_plan_snapshot_rejected")
		}
	}
	return nil
}

func validateSandboxRuntimeSessionAndArm(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	entry bool,
) (string, string, error) {
	var sessionState string
	if err := tx.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_sandbox_sessions WHERE id=$1 FOR UPDATE`, plan.SessionID).Scan(&sessionState); err != nil ||
		sessionState == "STOPPED" || (entry && sessionState != "ARMED") {
		return "", "", fmt.Errorf("sandbox_runtime_session_not_ready")
	}
	var armCreated, armExpires time.Time
	var revoked *time.Time
	var actorUser, actorSession, reasonHash string
	var armRevision int64
	if err := tx.QueryRow(ctx, `
SELECT created_at,expires_at,revoked_at,actor_user_id,actor_session_id,
       reason_hash,revision
FROM sandbox_runtime_sandbox_arms
WHERE id=$1 AND sandbox_session_id=$2 FOR UPDATE`,
		plan.Arm.ID,
		plan.SessionID,
	).Scan(
		&armCreated,
		&armExpires,
		&revoked,
		&actorUser,
		&actorSession,
		&reasonHash,
		&armRevision,
	); err != nil ||
		!armCreated.Equal(plan.Arm.CreatedAt) ||
		!armExpires.Equal(plan.Arm.ExpiresAt) ||
		actorUser != plan.Arm.ActorUserID ||
		actorSession != plan.Arm.ActorSessionID ||
		reasonHash != plan.Arm.ReasonHash ||
		uint64(armRevision) != plan.Arm.Revision ||
		(entry && (revoked != nil || !plan.ApprovedAt.Before(armExpires))) {
		return "", "", fmt.Errorf("sandbox_runtime_arm_not_active")
	}
	return actorUser, actorSession, nil
}

func validateSandboxRuntimeArmActor(
	ctx context.Context,
	tx pgx.Tx,
	actorUser, actorSession string,
	at time.Time,
) error {
	var actorActive bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM sessions session
  JOIN users actor ON actor.id=session.user_id
  WHERE session.id=$1 AND session.user_id=$2
    AND actor.status='active'
    AND session.revoked_at IS NULL
    AND session.expires_at>$3
    AND session.idle_expires_at>$3
	)`, actorSession, actorUser, at).Scan(&actorActive); err != nil || !actorActive {
		return fmt.Errorf("sandbox_runtime_arm_session_invalid")
	}
	return nil
}

func validateSandboxRuntimePlanAccounts(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	entry bool,
) (map[sandbox.AccountID]sandbox.Exchange, error) {
	exchanges := make(map[sandbox.AccountID]sandbox.Exchange, len(plan.Submissions))
	for _, submission := range plan.Submissions {
		exchange, err := validateSandboxRuntimePlanAccount(ctx, tx, plan, submission, entry)
		if err != nil {
			return nil, err
		}
		exchanges[submission.AccountID] = exchange
	}
	return exchanges, nil
}

func validateSandboxRuntimePlanAccount(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
	entry bool,
) (sandbox.Exchange, error) {
	var exchange, accountState string
	var epoch int64
	if err := tx.QueryRow(ctx, validateSandboxRuntimePlanAccountSQL,
		submission.AccountID,
		plan.SessionID,
		submission.AccountEpoch,
	).Scan(&exchange, &epoch, &accountState); err != nil ||
		uint64(epoch) != submission.AccountEpoch ||
		(entry && accountState != "ARMED") {
		return "", fmt.Errorf("sandbox_runtime_account_epoch_rejected")
	}
	exchangeName := sandbox.Exchange(exchange)
	if entry {
		if err := validateSandboxRuntimeAccountEntry(ctx, tx, plan, submission, exchangeName); err != nil {
			return "", err
		}
	}
	return exchangeName, nil
}

const validateSandboxRuntimePlanAccountSQL = `
SELECT account.exchange,account.current_epoch,account.state
FROM sandbox_runtime_exchange_accounts account
JOIN sandbox_runtime_sandbox_session_accounts membership
  ON membership.account_id=account.id AND membership.account_epoch=account.current_epoch
WHERE account.id=$1 AND membership.session_id=$2 AND membership.account_epoch=$3
-- Session membership is immutable for runtime; lock only the mutable account.
FOR SHARE OF account`

func validateSandboxRuntimeAccountEntry(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
	exchange sandbox.Exchange,
) error {
	snapshot, exists := sandbox.EligibilityForSubmission(plan, submission, exchange)
	if !exists || !snapshot.Eligible || snapshot.Exchange != string(exchange) ||
		snapshot.Instrument != submission.Instrument.Symbol() ||
		snapshot.ObservedAt.IsZero() ||
		snapshot.ObservedAt.Location() != time.UTC ||
		snapshot.ObservedAt.After(plan.ApprovedAt) ||
		plan.ApprovedAt.Sub(snapshot.ObservedAt) > 250*time.Millisecond {
		return fmt.Errorf("sandbox_runtime_public_ineligible")
	}
	safety, exists := plan.EntrySafety[submission.AccountID]
	if !exists || safety.ValidateFor(submission, exchange, plan.ApprovedAt) != nil {
		return fmt.Errorf("sandbox_runtime_entry_safety_rejected")
	}
	var leaseHeld bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM sandbox_runtime_account_leases
  WHERE account_id=$1 AND environment=$2 AND expires_at>$3
)`,
		submission.AccountID,
		environmentForSandboxRuntimeExchange(exchange),
		plan.ApprovedAt,
	).Scan(&leaseHeld); err != nil || !leaseHeld {
		return fmt.Errorf("sandbox_runtime_entry_lease_rejected")
	}
	return nil
}

func environmentForSandboxRuntimeExchange(exchange sandbox.Exchange) sandbox.Environment {
	if exchange == sandbox.ExchangeBinance {
		return sandbox.EnvironmentBinanceSpotTestnet
	}
	return sandbox.EnvironmentBybitDemo
}
