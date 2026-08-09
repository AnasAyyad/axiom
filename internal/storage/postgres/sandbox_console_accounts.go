package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/cockroachdb/apd/v3"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var sandboxStrategyEvaluationReasons = map[string]string{
	"waiting_for_finalized_candle":        "waiting for the next finalized market candle.",
	"waiting_for_multileg_runtime":        "a historical engine build was waiting for its paired-market strategy runtime.",
	"waiting_for_binance_coordinator":     "waiting for the Binance coordinator to evaluate the coherent paired-market input.",
	"waiting_for_multileg_facts":          "waiting for current fenced account, arm, eligibility, inventory, and risk facts for every required leg.",
	"waiting_for_synchronized_books":      "waiting for fresh synchronized public order books for every required market.",
	"waiting_for_multileg_risk":           "waiting for one complete, atomic central-risk projection across the required account set.",
	"waiting_for_multileg_capital":        "waiting for sufficient strategy-owned inventory, fee buffers, recovery allowance, and capped quote capacity.",
	"waiting_for_multileg_pipeline":       "waiting for the complete atomic allocation, central-risk, planning, and durable-dispatch pipeline.",
	"waiting_for_public_market_data":      "waiting for fresh public market data.",
	"waiting_for_strategy_admission":      "waiting for the current arm, account, market, and configuration admission checks.",
	"waiting_for_sizing_facts":            "waiting for a current account snapshot, risk policy, and asset-eligibility facts.",
	"waiting_for_position_projection":     "waiting for its current position projection.",
	"waiting_for_owned_inventory":         "waiting for confirmed strategy-owned inventory.",
	"waiting_for_strategy_pipeline":       "waiting for a complete shared allocation, risk, and execution plan.",
	"strategy_plan_approved":              "a safe execution plan was approved; follow the session evidence for the next recorded action.",
	"strategy_candidate_rejected":         "the synchronized books did not contain an eligible net-positive candidate after fees, filters, depth, rounding, and recovery economics.",
	"strategy_allocation_rejected":        "the candidate could not atomically reserve every required balance, fee, liquidity, and recovery resource.",
	"central_risk_rejected":               "central risk rejected the candidate; no order was created.",
	"strategy_planning_failed":            "the approved candidate could not be converted into a complete capped spot execution plan; no order was created.",
	"strategy_plan_projection_failed":     "the approved candidate could not be converted into a complete capped spot execution plan; no order was created.",
	"strategy_plan_persistence_uncertain": "durable plan acceptance was uncertain, so its allocation was quarantined for recovery and no retry was guessed.",
	"multileg_input_invalid":              "the multi-market evaluation failed closed before a safe durable plan could be established.",
	"multileg_pipeline_failed":            "the multi-market evaluation failed closed before a safe durable plan could be established.",
	"strategy_decision_recorded":          "the latest strategy decision was recorded with its shared-pipeline evidence.",
	"strategy_input_invalid":              "the latest market input was not safe to evaluate, so no order was created.",
	"strategy_decision_unavailable":       "the latest strategy decision could not be produced, so no order was created.",
	"strategy_decision_record_failed":     "the latest strategy decision could not be recorded, so no order was created.",
	"strategy_pipeline_blocked":           "the shared allocation, risk, or planning checks blocked the latest candidate; no order was created.",
}

func sandboxQualificationOverviewArms(
	accounts []generated.SandboxAccount,
) ([]generated.SandboxArm, bool) {
	arms := make([]generated.SandboxArm, 0, len(accounts))
	stale := false
	seen := make(map[string]struct{})
	for _, account := range accounts {
		stale = stale || account.Stale
		if account.ActiveArm == nil {
			continue
		}
		if _, exists := seen[account.ActiveArm.Id]; exists {
			continue
		}
		seen[account.ActiveArm.Id] = struct{}{}
		arms = append(arms, *account.ActiveArm)
	}
	return arms, stale
}

type sandboxRuntimeConsoleAccountRow struct {
	id, exchange, environment, state    string
	epoch, generation, revision, cycle  int64
	privateHealthy, reconciliationClean bool
	evidenceHealthy, leaseHeld          bool
	observed                            time.Time
	sessionID                           *string
	sessionRevision                     *int64
	accountOpen                         int
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleAccounts(
	ctx context.Context,
) ([]generated.SandboxAccount, error) {
	rows, err := store.pool.Query(
		ctx, sandboxRuntimeConsoleAccountsSQL, store.clock.Now().UTC,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := store.clock.Now().UTC
	globalOpen, dailyReserved, err := store.sandboxRuntimeConsoleGlobalCapacity(ctx, now)
	if err != nil {
		return nil, err
	}
	accounts := make([]generated.SandboxAccount, 0, 2)
	for rows.Next() {
		row, scanErr := scanSandboxRuntimeConsoleAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		arm, armErr := store.sandboxRuntimeConsoleActiveArm(ctx, row.id, now)
		if armErr != nil {
			return nil, armErr
		}
		capUsage, capErr := sandboxRuntimeConsoleCapUsage(
			now, dailyReserved, row.accountOpen, globalOpen,
		)
		if capErr != nil {
			return nil, capErr
		}
		accounts = append(accounts, generatedSandboxRuntimeConsoleAccount(row, now, arm, capUsage))
	}
	return accounts, rows.Err()
}

func scanSandboxRuntimeConsoleAccount(
	row sandboxRuntimeConsoleRowScanner,
) (sandboxRuntimeConsoleAccountRow, error) {
	var result sandboxRuntimeConsoleAccountRow
	err := row.Scan(
		&result.id, &result.exchange, &result.environment, &result.state,
		&result.epoch, &result.generation, &result.revision, &result.cycle,
		&result.privateHealthy, &result.reconciliationClean,
		&result.evidenceHealthy, &result.observed,
		&result.sessionID, &result.sessionRevision,
		&result.leaseHeld, &result.accountOpen,
	)
	return result, err
}

func generatedSandboxRuntimeConsoleAccount(
	row sandboxRuntimeConsoleAccountRow,
	now time.Time,
	arm *generated.SandboxArm,
	capUsage generated.SandboxCapUsage,
) generated.SandboxAccount {
	stale := now.Sub(row.observed.UTC()) > sandboxRuntimeConsoleStaleAfter
	ready := !stale && row.leaseHeld && row.privateHealthy &&
		row.reconciliationClean && row.evidenceHealthy &&
		(row.state == "READY_PAUSED" || row.state == "ARMED")
	account := generated.SandboxAccount{
		Id: row.id, Exchange: generated.SandboxExchange(row.exchange),
		Environment: generated.SandboxEnvironment(row.environment),
		State:       generated.SandboxAccountState(row.state), EngineReady: ready,
		AccountEpoch: row.epoch, CredentialGeneration: row.generation,
		Revision: strconv.FormatInt(row.revision, 10), StartupCycle: row.cycle,
		PrivateStreamHealthy: row.privateHealthy,
		ReconciliationClean:  row.reconciliationClean,
		EvidenceHealthy:      row.evidenceHealthy, LeaseHeld: row.leaseHeld,
		ObservedAt: row.observed.UTC(), Stale: stale,
		ActiveArm: arm, CapUsage: capUsage,
		AuditUrl: "/api/v1/audit-events?event_type=sandbox_runtime_" + row.exchange,
	}
	account.SessionId = row.sessionID
	if row.sessionRevision != nil {
		value := strconv.FormatInt(*row.sessionRevision, 10)
		account.SessionRevision = &value
	}
	return account
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleGlobalCapacity(
	ctx context.Context,
	now time.Time,
) (int, string, error) {
	var globalOpen int
	if err := store.pool.QueryRow(ctx, `
SELECT count(*)::integer FROM sandbox_runtime_submission_outbox
WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
	).Scan(&globalOpen); err != nil {
		return 0, "", err
	}
	var dailyReserved string
	err := store.pool.QueryRow(ctx, `
SELECT reserved_notional::text FROM sandbox_runtime_daily_cap_counters
WHERE utc_day=$1`, now.Format("2006-01-02")).Scan(&dailyReserved)
	if errors.Is(err, pgx.ErrNoRows) {
		dailyReserved = "0"
		err = nil
	}
	return globalOpen, dailyReserved, err
}

func sandboxRuntimeConsoleCapUsage(
	now time.Time,
	dailyReserved string,
	accountOpen, globalOpen int,
) (generated.SandboxCapUsage, error) {
	remaining, err := decimalSubtract("50", dailyReserved)
	if err != nil {
		return generated.SandboxCapUsage{}, err
	}
	date, err := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if err != nil {
		return generated.SandboxCapUsage{}, err
	}
	return generated.SandboxCapUsage{
		UtcDay:           openapi_types.Date{Time: date},
		PerOrderLimit:    "10",
		DailyLimit:       "50",
		DailyReserved:    generated.NonnegativeDecimal(dailyReserved),
		DailyRemaining:   generated.NonnegativeDecimal(remaining),
		AccountOpen:      accountOpen,
		AccountOpenLimit: generated.SandboxCapUsageAccountOpenLimit(1),
		GlobalOpen:       globalOpen,
		GlobalOpenLimit:  generated.SandboxCapUsageGlobalOpenLimit(2),
	}, nil
}

func decimalSubtract(left, right string) (string, error) {
	leftValue, _, err := apd.NewFromString(left)
	if err != nil {
		return "", err
	}
	rightValue, _, err := apd.NewFromString(right)
	if err != nil {
		return "", err
	}
	result := apd.New(0, 0)
	if _, err = apd.BaseContext.Sub(result, leftValue, rightValue); err != nil ||
		result.Sign() < 0 {
		return "", fmt.Errorf("sandbox_runtime_console_capacity_invalid")
	}
	return result.Text('f'), nil
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleActiveArm(
	ctx context.Context,
	accountID string,
	now time.Time,
) (*generated.SandboxArm, error) {
	var (
		id, sessionID        string
		createdAt, expiresAt time.Time
		revokedAt            *time.Time
		revision             int64
		accountIDs           []string
	)
	err := store.pool.QueryRow(ctx, sandboxRuntimeConsoleActiveArmSQL, accountID).Scan(
		&id, &sessionID, &createdAt, &expiresAt,
		&revokedAt, &revision, &accountIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state := "active"
	if revokedAt != nil {
		state = "revoked"
	} else if !now.Before(expiresAt.UTC()) {
		state = "expired"
	}
	return &generated.SandboxArm{
		Id:         id,
		SessionId:  sessionID,
		AccountIds: accountIDs,
		State:      generated.SandboxArmState(state),
		CreatedAt:  createdAt.UTC(),
		ExpiresAt:  expiresAt.UTC(),
		RevokedAt:  utcPointer(revokedAt),
		Revision:   strconv.FormatInt(revision, 10),
		AuditUrl:   "/api/v1/audit-events?event_type=sandbox_arm",
	}, nil
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}

var _ console.SandboxReadService = (*OwnerConsoleStore)(nil)
