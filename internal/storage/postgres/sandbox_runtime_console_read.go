package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/generated"
)

const (
	sandboxRuntimeConsoleOrderScope          = "sandbox_runtime-console-orders"
	sandboxRuntimeConsoleReconciliationScope = "sandbox_runtime-console-reconciliations"
	sandboxRuntimeConsoleStaleAfter          = 5 * time.Second
)

const sandboxRuntimeConsoleAccountsSQL = `
SELECT account.id,account.exchange,account.environment,account.state,
       account.current_epoch,account.credential_generation,account.revision,
       coalesce(observation.startup_cycle,0),
       coalesce(observation.private_stream_healthy,false),
       coalesce(observation.reconciliation_clean,false),
       coalesce(observation.evidence_healthy,false),
       coalesce(observation.observed_at,account.updated_at),
       active_session.id,active_session.revision,
       EXISTS(
         SELECT 1 FROM sandbox_runtime_account_leases lease
         WHERE lease.account_id=account.id
           AND lease.expires_at>$1
       ),
       coalesce((
         SELECT count(*) FROM sandbox_runtime_submission_outbox active_order
         WHERE active_order.account_id=account.id
           AND active_order.state IN (
             'PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN'
           )
       ),0)::integer
FROM sandbox_runtime_exchange_accounts account
LEFT JOIN sandbox_runtime_engine_observations observation
 ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
LEFT JOIN LATERAL (
  SELECT session.id,session.revision
  FROM sandbox_runtime_sandbox_session_accounts membership
  JOIN sandbox_runtime_sandbox_sessions session ON session.id=membership.session_id
  WHERE membership.account_id=account.id
    AND membership.account_epoch=account.current_epoch
    AND session.state IN ('READY_PAUSED','ARMED','PAUSED')
  ORDER BY session.updated_at DESC,session.id DESC
  LIMIT 1
) active_session ON true
ORDER BY account.exchange,account.id`

const sandboxRuntimeConsoleActiveArmSQL = `
SELECT arm.id,arm.sandbox_session_id,arm.created_at,arm.expires_at,
       arm.revoked_at,arm.revision,
       ARRAY(
         SELECT membership.account_id
         FROM sandbox_runtime_sandbox_session_accounts membership
         WHERE membership.session_id=arm.sandbox_session_id
         ORDER BY membership.account_id
       )
FROM sandbox_runtime_sandbox_arms arm
JOIN sandbox_runtime_sandbox_session_accounts membership
  ON membership.session_id=arm.sandbox_session_id
WHERE membership.account_id=$1
ORDER BY arm.created_at DESC,arm.id DESC
LIMIT 1`

// SandboxOverview returns one current redacted sandbox qualification operations snapshot.
func (store *OwnerConsoleStore) SandboxOverview(
	ctx context.Context,
) (generated.SandboxOverview, error) {
	accounts, err := store.sandboxRuntimeConsoleAccounts(ctx)
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
	strategySessions, err := store.sandboxStrategySessions(ctx)
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	now := store.clock.Now().UTC
	arms, stale := sandboxQualificationOverviewArms(accounts)
	riskState, err := store.sandboxRuntimeConsoleRiskState(ctx, accounts)
	if err != nil {
		return generated.SandboxOverview{}, err
	}
	return generated.SandboxOverview{
		EnvironmentLabel: generated.SandboxOverviewEnvironmentLabelVirtualOnly,
		RealTradingEnabled: generated.SandboxOverviewRealTradingEnabled(
			false,
		),
		ObservedAt:       now,
		Stale:            stale,
		Accounts:         accounts,
		ActiveArms:       arms,
		StrategySessions: strategySessions,
		Orders:           orders.Items,
		Reconciliations:  reconciliations.Items,
		ResetIncidents:   reconciliations.ResetIncidents,
		RiskState:        generated.SandboxOverviewRiskState(riskState),
		Qualification:    qualification,
		AuditUrl:         "/api/v1/audit-events?event_type=sandbox_runtime",
	}, nil
}

const sandboxStrategySessionsSQL = `
SELECT strategy.id,strategy.strategy_id,strategy.instrument,strategy.state,
       strategy.created_at,strategy.started_at,strategy.stopped_at,
       strategy.blocking_reason,strategy.revision,
       array_agg(account.exchange ORDER BY account.exchange),
       array_agg(COALESCE(evaluation.reason,'') ORDER BY account.exchange)
FROM sandbox_strategy_sessions strategy
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy.id
JOIN sandbox_runtime_exchange_accounts account ON account.id=membership.account_id
LEFT JOIN LATERAL (
  SELECT latest.reason
  FROM sandbox_strategy_session_evaluations latest
  WHERE latest.strategy_session_id=strategy.id
    AND latest.account_id=membership.account_id
    AND latest.account_epoch=membership.account_epoch
  ORDER BY latest.occurred_at DESC,latest.id DESC
  LIMIT 1
) evaluation ON TRUE
GROUP BY strategy.id,strategy.strategy_id,strategy.instrument,strategy.state,
         strategy.created_at,strategy.started_at,strategy.stopped_at,
         strategy.blocking_reason,strategy.revision
ORDER BY strategy.created_at DESC,strategy.id DESC`

func (store *OwnerConsoleStore) sandboxStrategySessions(
	ctx context.Context,
) ([]generated.SandboxStrategySession, error) {
	rows, err := store.pool.Query(ctx, sandboxStrategySessionsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.SandboxStrategySession, 0)
	for rows.Next() {
		var id, strategy, state string
		var instrument, blockingReason *string
		var createdAt time.Time
		var startedAt, stoppedAt *time.Time
		var revision int64
		var exchanges, evaluationReasons []string
		if err = rows.Scan(&id, &strategy, &instrument, &state, &createdAt,
			&startedAt, &stoppedAt, &blockingReason, &revision, &exchanges, &evaluationReasons); err != nil {
			return nil, err
		}
		item, itemErr := generatedSandboxStrategySession(id, strategy, instrument,
			state, createdAt, startedAt, stoppedAt, blockingReason, revision, exchanges, evaluationReasons)
		if itemErr != nil {
			return nil, itemErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func generatedSandboxStrategySession(
	id, strategy string,
	instrument *string,
	state string,
	createdAt time.Time,
	startedAt, stoppedAt *time.Time,
	blockingReason *string,
	revision int64,
	exchanges []string,
	evaluationReasons []string,
) (generated.SandboxStrategySession, error) {
	name, ok := sandboxStrategyName(strategy)
	if !ok || id == "" || revision <= 0 || createdAt.IsZero() || createdAt.Location() != time.UTC ||
		len(exchanges) < 1 || len(exchanges) > 2 || len(evaluationReasons) != len(exchanges) {
		return generated.SandboxStrategySession{}, fmt.Errorf("sandbox_strategy_session_projection_invalid")
	}
	venues := make([]generated.SandboxExchange, 0, len(exchanges))
	for _, exchange := range exchanges {
		venue := generated.SandboxExchange(exchange)
		if !venue.Valid() {
			return generated.SandboxStrategySession{}, fmt.Errorf("sandbox_strategy_session_projection_invalid")
		}
		venues = append(venues, venue)
	}
	item := generated.SandboxStrategySession{
		Id: id, StrategyName: name, Exchanges: venues,
		State: generated.SandboxStrategySessionState(state), CreatedAt: createdAt.UTC(),
		Revision: strconv.FormatInt(revision, 10),
		AuditUrl: "/api/v1/audit-events?target_type=sandbox_strategy_session",
	}
	if !item.State.Valid() {
		return generated.SandboxStrategySession{}, fmt.Errorf("sandbox_strategy_session_projection_invalid")
	}
	if instrument != nil {
		value := generated.SandboxStrategySessionInstrument(*instrument)
		if !value.Valid() {
			return generated.SandboxStrategySession{}, fmt.Errorf("sandbox_strategy_session_projection_invalid")
		}
		item.Instrument = &value
	}
	item.DisplayName = sandboxStrategySessionDisplayName(name, venues, item.Instrument)
	waitingReason := sandboxStrategySessionWaitingReason(item.State, blockingReason, venues, evaluationReasons)
	item.WaitingReason = &waitingReason
	item.StartedAt, item.StoppedAt = utcPointer(startedAt), utcPointer(stoppedAt)
	return item, nil
}

func sandboxStrategyName(value string) (string, bool) {
	switch value {
	case "trend":
		return "Trend Following", true
	case "mean-reversion":
		return "Mean Reversion", true
	case "triangular":
		return "Triangular Arbitrage", true
	case "cross-exchange-arbitrage":
		return "Cross-Exchange Arbitrage", true
	default:
		return "", false
	}
}

func sandboxStrategySessionDisplayName(
	strategy string,
	exchanges []generated.SandboxExchange,
	instrument *generated.SandboxStrategySessionInstrument,
) string {
	venueNames := make([]string, 0, len(exchanges))
	for _, exchange := range exchanges {
		if exchange == generated.SandboxExchangeBinance {
			venueNames = append(venueNames, "Binance Spot Testnet")
		} else {
			venueNames = append(venueNames, "Bybit Demo")
		}
	}
	label := strategy + " · " + strings.Join(venueNames, " + ")
	if instrument != nil {
		label += " · " + string(*instrument)
	}
	return label
}

func sandboxStrategySessionWaitingReason(
	state generated.SandboxStrategySessionState,
	blockingReason *string,
	exchanges []generated.SandboxExchange,
	evaluationReasons []string,
) string {
	switch state {
	case generated.SandboxStrategySessionStatePrepared:
		return "This session is prepared. Complete owner reauthentication and arm its selected account before the strategy can begin evaluating."
	case generated.SandboxStrategySessionStateRunning:
		if reason := sandboxStrategyEvaluationSummary(exchanges, evaluationReasons); reason != "" {
			return reason
		}
		return "The session is armed and running, but the engine has not recorded its first strategy evaluation yet. Confirm the selected sandbox engine is running."
	case generated.SandboxStrategySessionStateBlocked:
		if blockingReason != nil && *blockingReason == "arm_expired_or_revoked" {
			return "The owner arm expired or was revoked. New automatic entries are blocked; cancellation, reconciliation, and risk-reducing recovery remain available."
		}
		return "This strategy session is blocked. Review the account, arm, and reconciliation state before creating a new session."
	case generated.SandboxStrategySessionStateStopped:
		return "The owner stopped this strategy session. It cannot create new automatic entries."
	default:
		return "Session status is not recorded."
	}
}

func sandboxStrategyEvaluationSummary(exchanges []generated.SandboxExchange, reasons []string) string {
	entries := make([]string, 0, len(reasons))
	for index, reason := range reasons {
		if reason == "" {
			continue
		}
		entries = append(entries, sandboxStrategyExchangeName(exchanges[index])+": "+sandboxStrategyEvaluationReason(reason))
	}
	if len(entries) == 0 {
		return ""
	}
	return "Latest strategy evaluation — " + strings.Join(entries, " ")
}

func sandboxStrategyExchangeName(exchange generated.SandboxExchange) string {
	if exchange == generated.SandboxExchangeBinance {
		return "Binance Spot Testnet"
	}
	return "Bybit Demo"
}

// sandboxStrategyEvaluationReason translates only sanitized, stable scheduler
// reason codes. It deliberately never projects an adapter error or a private
// exchange payload into the owner console.
func sandboxStrategyEvaluationReason(reason string) string {
	if value, exists := sandboxStrategyEvaluationReasons[reason]; exists {
		return value
	}
	return "the latest safe evaluation did not produce an order. Review the session audit evidence for its recorded outcome."
}
