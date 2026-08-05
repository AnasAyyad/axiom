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

const (
	v1cConsoleOrderScope          = "v1c-console-orders"
	v1cConsoleReconciliationScope = "v1c-console-reconciliations"
	v1cConsoleStaleAfter          = 5 * time.Second
)

const v1cConsoleAccountsSQL = `
SELECT account.id,account.exchange,account.environment,account.state,
       account.current_epoch,account.credential_generation,account.revision,
       coalesce(observation.startup_cycle,0),
       coalesce(observation.private_stream_healthy,false),
       coalesce(observation.reconciliation_clean,false),
       coalesce(observation.evidence_healthy,false),
       coalesce(observation.observed_at,account.updated_at),
       active_session.id,active_session.revision,
       EXISTS(
         SELECT 1 FROM v1c_account_leases lease
         WHERE lease.account_id=account.id
           AND lease.expires_at>$1
       ),
       coalesce((
         SELECT count(*) FROM v1c_submission_outbox active_order
         WHERE active_order.account_id=account.id
           AND active_order.state IN (
             'PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN'
           )
       ),0)::integer
FROM v1c_exchange_accounts account
LEFT JOIN v1c_engine_observations observation
 ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
LEFT JOIN LATERAL (
  SELECT session.id,session.revision
  FROM v1c_sandbox_session_accounts membership
  JOIN v1c_sandbox_sessions session ON session.id=membership.session_id
  WHERE membership.account_id=account.id
    AND membership.account_epoch=account.current_epoch
    AND session.state IN ('READY_PAUSED','ARMED','PAUSED')
  ORDER BY session.updated_at DESC,session.id DESC
  LIMIT 1
) active_session ON true
ORDER BY account.exchange,account.id`

const v1cConsoleActiveArmSQL = `
SELECT arm.id,arm.sandbox_session_id,arm.created_at,arm.expires_at,
       arm.revoked_at,arm.revision,
       ARRAY(
         SELECT membership.account_id
         FROM v1c_sandbox_session_accounts membership
         WHERE membership.session_id=arm.sandbox_session_id
         ORDER BY membership.account_id
       )
FROM v1c_sandbox_arms arm
JOIN v1c_sandbox_session_accounts membership
  ON membership.session_id=arm.sandbox_session_id
WHERE membership.account_id=$1
ORDER BY arm.created_at DESC,arm.id DESC
LIMIT 1`

// SandboxOverview returns one current redacted C6 operations snapshot.
func (store *A11ConsoleStore) SandboxOverview(
	ctx context.Context,
) (generated.SandboxOverview, error) {
	accounts, err := store.v1cConsoleAccounts(ctx)
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	orders, err := store.SandboxOrders(ctx, "", 20, "", "")
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	reconciliations, err := store.SandboxReconciliations(ctx, "", 20, "")
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	qualification, err := store.SandboxQualification(ctx)
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	now := store.clock.Now().UTC
	arms, stale := c6OverviewArms(accounts)
	riskState, err := store.v1cConsoleRiskState(ctx, accounts)
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	return generated.SandboxOverview{
		EnvironmentLabel: generated.SandboxOverviewEnvironmentLabelVirtualOnly,
		RealTradingEnabled: generated.SandboxOverviewRealTradingEnabled(
			false,
		),
		ObservedAt:      now,
		Stale:           stale,
		Accounts:        accounts,
		ActiveArms:      arms,
		Orders:          orders.Items,
		Reconciliations: reconciliations.Items,
		ResetIncidents:  reconciliations.ResetIncidents,
		RiskState:       generated.SandboxOverviewRiskState(riskState),
		Qualification:   qualification,
		AuditUrl:        "/api/v1/audit-events?event_type=v1c",
	}, nil
}

func c6OverviewArms(
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

type v1cConsoleAccountRow struct {
	id, exchange, environment, state    string
	epoch, generation, revision, cycle  int64
	privateHealthy, reconciliationClean bool
	evidenceHealthy, leaseHeld          bool
	observed                            time.Time
	sessionID                           *string
	sessionRevision                     *int64
	accountOpen                         int
}

func (store *A11ConsoleStore) v1cConsoleAccounts(
	ctx context.Context,
) ([]generated.SandboxAccount, error) {
	rows, err := store.pool.Query(
		ctx, v1cConsoleAccountsSQL, store.clock.Now().UTC,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := store.clock.Now().UTC
	globalOpen, dailyReserved, err := store.v1cConsoleGlobalCapacity(ctx, now)
	if err != nil {
		return nil, err
	}
	accounts := make([]generated.SandboxAccount, 0, 2)
	for rows.Next() {
		row, scanErr := scanV1CConsoleAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		arm, armErr := store.v1cConsoleActiveArm(ctx, row.id, now)
		if armErr != nil {
			return nil, armErr
		}
		capUsage, capErr := v1cConsoleCapUsage(
			now, dailyReserved, row.accountOpen, globalOpen,
		)
		if capErr != nil {
			return nil, capErr
		}
		accounts = append(accounts, generatedV1CConsoleAccount(row, now, arm, capUsage))
	}
	return accounts, rows.Err()
}

func scanV1CConsoleAccount(
	row v1cConsoleRowScanner,
) (v1cConsoleAccountRow, error) {
	var result v1cConsoleAccountRow
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

func generatedV1CConsoleAccount(
	row v1cConsoleAccountRow,
	now time.Time,
	arm *generated.SandboxArm,
	capUsage generated.SandboxCapUsage,
) generated.SandboxAccount {
	stale := now.Sub(row.observed.UTC()) > v1cConsoleStaleAfter
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
		AuditUrl: "/api/v1/audit-events?event_type=v1c_" + row.exchange,
	}
	account.SessionId = row.sessionID
	if row.sessionRevision != nil {
		value := strconv.FormatInt(*row.sessionRevision, 10)
		account.SessionRevision = &value
	}
	return account
}

func (store *A11ConsoleStore) v1cConsoleGlobalCapacity(
	ctx context.Context,
	now time.Time,
) (int, string, error) {
	var globalOpen int
	if err := store.pool.QueryRow(ctx, `
SELECT count(*)::integer FROM v1c_submission_outbox
WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
	).Scan(&globalOpen); err != nil {
		return 0, "", err
	}
	var dailyReserved string
	err := store.pool.QueryRow(ctx, `
SELECT reserved_notional::text FROM v1c_daily_cap_counters
WHERE utc_day=$1`, now.Format("2006-01-02")).Scan(&dailyReserved)
	if errors.Is(err, pgx.ErrNoRows) {
		dailyReserved = "0"
		err = nil
	}
	return globalOpen, dailyReserved, err
}

func v1cConsoleCapUsage(
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
		return "", fmt.Errorf("v1c_console_capacity_invalid")
	}
	return result.Text('f'), nil
}

func (store *A11ConsoleStore) v1cConsoleActiveArm(
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
	err := store.pool.QueryRow(ctx, v1cConsoleActiveArmSQL, accountID).Scan(
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

var _ console.SandboxReadService = (*A11ConsoleStore)(nil)
