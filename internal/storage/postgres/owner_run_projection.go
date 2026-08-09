package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const ownerRunProjectionSQL = `
WITH sandbox_run_records AS (
  SELECT strategy.id,'sandbox'::text AS mode,strategy.state,strategy.revision,
    strategy.created_at,
    greatest(
      strategy.created_at,
      coalesce(strategy.started_at,strategy.created_at),
      coalesce(strategy.stopped_at,strategy.created_at),
      coalesce(max(evaluation.occurred_at),strategy.created_at)
    ) AS updated_at,
    strategy.strategy_id AS strategy_version_id,
    array_agg(membership.exchange ORDER BY membership.exchange)::text[] AS exchanges,
    strategy.instrument,
    strategy.blocking_reason,
    array_agg(coalesce(evaluation.reason,'') ORDER BY membership.exchange)::text[] AS evaluation_reasons,
    NULL::text AS shadow_waiting_reason,NULL::timestamptz AS next_evaluation_at
  FROM sandbox_strategy_sessions strategy
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=strategy.id
  LEFT JOIN LATERAL (
    SELECT latest.reason,latest.occurred_at
    FROM sandbox_strategy_session_evaluations latest
    WHERE latest.strategy_session_id=strategy.id
      AND latest.account_id=membership.account_id
      AND latest.account_epoch=membership.account_epoch
    ORDER BY latest.occurred_at DESC,latest.id DESC
    LIMIT 1
  ) evaluation ON TRUE
  GROUP BY strategy.id,strategy.state,strategy.revision,strategy.created_at,
    strategy.started_at,strategy.stopped_at,strategy.strategy_id,
    strategy.instrument,strategy.blocking_reason
), run_records AS (
  SELECT job.id,job.job_type AS mode,job.state,job.progress_revision AS revision,
    job.created_at,job.updated_at,run.strategy_version_id,
    ARRAY[]::text[] AS exchanges,NULL::text AS instrument,
    NULL::text AS blocking_reason,ARRAY[]::text[] AS evaluation_reasons,
    NULL::text AS shadow_waiting_reason,NULL::timestamptz AS next_evaluation_at
  FROM jobs job JOIN runs run ON run.id=job.id
  WHERE job.job_type IN ('backtest','replay') AND job.request_payload IS NOT NULL
  UNION ALL
  SELECT session.id,'shadow' AS mode,session.state,session.revision,session.created_at,
    coalesce(stopped_at,created_at),strategy_version_id,
	CASE WHEN session.strategy_version_id='cross-exchange-arbitrage-1-0-0' THEN ARRAY(
	  SELECT DISTINCT scope.exchange_id FROM shadow_session_market_scopes scope
	  WHERE scope.session_id=session.id ORDER BY scope.exchange_id
	) ELSE ARRAY[session.exchange_id]::text[] END AS exchanges,
    CASE WHEN instrument.id IS NULL THEN NULL ELSE instrument.base_asset || instrument.quote_asset END AS instrument,
    NULL::text AS blocking_reason,ARRAY[]::text[] AS evaluation_reasons,
    activity.summary AS shadow_waiting_reason,activity.next_evaluation_at
  FROM shadow_sessions session
  LEFT JOIN instruments instrument ON instrument.id=session.instrument_id
  LEFT JOIN LATERAL (
    SELECT observation.summary,observation.next_evaluation_at
    FROM shadow_session_activity_observations observation
    WHERE observation.session_id=session.id
    ORDER BY observation.revision DESC LIMIT 1
  ) activity ON TRUE
  UNION ALL
  SELECT id,mode,state,revision,created_at,updated_at,strategy_version_id,
    exchanges,instrument,blocking_reason,evaluation_reasons,
    shadow_waiting_reason,next_evaluation_at
  FROM sandbox_run_records
)
SELECT id,mode,state,revision,created_at,updated_at,strategy_version_id,
  exchanges,instrument,blocking_reason,evaluation_reasons,shadow_waiting_reason,next_evaluation_at
FROM run_records`

// Runs joins the existing durable research-job, public-shadow, and explicitly
// armed exchange-sandbox lifecycles into one semantic owner projection. The
// underlying records stay authoritative; this method only changes how they are
// presented to the browser.
func (store *OwnerConsoleStore) Runs(ctx context.Context) (generated.RunPage, error) {
	rows, err := store.pool.Query(ctx, ownerRunProjectionSQL+`
ORDER BY created_at DESC,id DESC LIMIT 100`)
	if err != nil {
		return generated.RunPage{}, err
	}
	defer rows.Close()
	page := generated.RunPage{Items: make([]generated.RunResource, 0)}
	for rows.Next() {
		item, scanErr := scanOwnerRun(rows)
		if scanErr != nil {
			return generated.RunPage{}, scanErr
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

// Run reads an existing opaque run ID selected from the owner-facing list.
// It never asks the browser for an internal configuration or dataset ID.
func (store *OwnerConsoleStore) Run(ctx context.Context, id string) (generated.RunResource, error) {
	row := store.pool.QueryRow(ctx, ownerRunProjectionSQL+` WHERE id=$1`, id)
	item, err := scanOwnerRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.RunResource{}, console.ErrNotFound
	}
	return item, err
}

type ownerRunScanner interface{ Scan(...any) error }

func scanOwnerRun(scanner ownerRunScanner) (generated.RunResource, error) {
	row, err := scanOwnerRunRow(scanner)
	if err != nil {
		return generated.RunResource{}, err
	}
	strategyID, strategyVersion, strategyName, ok := ownerRunStrategy(row.strategyVersionID)
	if !ok {
		return generated.RunResource{}, console.ErrNotFound
	}
	item := generated.RunResource{Id: row.id, Mode: generated.RunResourceMode(row.mode), State: row.state,
		Revision: strconv.FormatInt(row.revision, 10), CreatedAt: row.created.UTC(), UpdatedAt: ptr(row.updated.UTC()),
		StrategyId: strategyID, StrategyVersion: strategyVersion, OrderCapable: strategyID != "inventory-rebalancing",
		AvailableActions: ownerRunActions(row.mode, row.state)}
	if len(row.exchanges) > 0 {
		item.Exchanges = ownerRunExchanges(row.exchanges)
		if item.Exchanges == nil {
			return generated.RunResource{}, console.ErrNotFound
		}
	}
	if row.instrument != nil {
		value, valid := ownerRunDisplayInstrument(*row.instrument)
		if !valid {
			return generated.RunResource{}, console.ErrNotFound
		}
		item.Instrument = &value
	}
	if err = projectOwnerRunMode(&item, row, strategyName); err != nil {
		return generated.RunResource{}, err
	}
	projectOwnerRunWaitingReason(&item, row)
	return item, nil
}

type ownerRunProjectionRow struct {
	id, mode, state, strategyVersionID              string
	instrument, blockingReason, shadowWaitingReason *string
	nextEvaluationAt                                *time.Time
	exchanges, evaluationReasons                    []string
	revision                                        int64
	created, updated                                time.Time
}

func scanOwnerRunRow(scanner ownerRunScanner) (ownerRunProjectionRow, error) {
	var row ownerRunProjectionRow
	err := scanner.Scan(&row.id, &row.mode, &row.state, &row.revision, &row.created, &row.updated,
		&row.strategyVersionID, &row.exchanges, &row.instrument, &row.blockingReason, &row.evaluationReasons,
		&row.shadowWaitingReason, &row.nextEvaluationAt)
	return row, err
}

func projectOwnerRunMode(item *generated.RunResource, row ownerRunProjectionRow, strategyName string) error {
	switch row.mode {
	case "backtest":
		item.FriendlyName, item.Environment = strategyName+" backtest", generated.RunResourceEnvironment("recorded_data")
	case "replay":
		item.FriendlyName, item.Environment = strategyName+" replay", generated.RunResourceEnvironment("recorded_data")
	case "shadow":
		item.FriendlyName, item.Environment = strategyName+" live shadow", generated.RunResourceEnvironment("production_public")
	case "sandbox":
		if len(row.exchanges) == 0 || len(row.exchanges) > 2 || len(row.evaluationReasons) != len(row.exchanges) {
			return console.ErrNotFound
		}
		item.FriendlyName = ownerSandboxRunName(strategyName, row.exchanges, row.instrument)
		item.Environment = ownerSandboxRunEnvironment(row.exchanges)
		if !item.Environment.Valid() {
			return console.ErrNotFound
		}
	default:
		return console.ErrNotFound
	}
	return nil
}

func projectOwnerRunWaitingReason(item *generated.RunResource, row ownerRunProjectionRow) {
	reason := ownerRunWaitingReason(row.mode, row.state)
	if row.mode == "shadow" && row.shadowWaitingReason != nil {
		reason = *row.shadowWaitingReason
		if row.nextEvaluationAt != nil {
			value := generated.Timestamp(row.nextEvaluationAt.UTC())
			item.NextEvaluationAt = &value
		}
	}
	if row.mode == "sandbox" {
		venues := ownerSandboxRunExchanges(row.exchanges)
		reason = sandboxStrategySessionWaitingReason(
			generated.SandboxStrategySessionState(row.state), row.blockingReason, venues, row.evaluationReasons,
		)
	}
	if reason != "" {
		item.WaitingReason = &reason
	}
}

// ownerRunActions is the sole projection of the durable lifecycle commands
// that are safe for the run's current state. The browser must not infer this
// from mode names or offer a command that the owner command service rejects.
func ownerRunActions(mode, state string) []generated.RunAction {
	switch mode {
	case "replay":
		switch state {
		case "RUNNING":
			return []generated.RunAction{generated.RunActionPause}
		case "PAUSED":
			return []generated.RunAction{generated.RunActionResume, generated.RunActionStep}
		}
	case "shadow":
		switch state {
		case "QUEUED", "RUNNING", "PAUSED":
			return []generated.RunAction{generated.RunActionStop}
		}
	case "sandbox":
		switch state {
		case "prepared", "running", "blocked":
			return []generated.RunAction{generated.RunActionStop}
		}
	}
	return []generated.RunAction{}
}

func ownerRunStrategy(value string) (id, version, name string, ok bool) {
	switch value {
	case "trend-following-1-0-0":
		return "trend-following", "trend-following@1.0.0", "Trend Following", true
	case "mean-reversion-1-0-0":
		return "mean-reversion", "mean-reversion@1.0.0", "Mean Reversion", true
	case "triangular-arbitrage-1-0-0":
		return "triangular-arbitrage", "triangular-arbitrage@1.0.0", "Triangular Arbitrage", true
	case "cross-exchange-arbitrage-1-0-0":
		return "cross-exchange-arbitrage", "cross-exchange-arbitrage@1.0.0", "Cross-Exchange Arbitrage", true
	case "inventory-rebalancing-1-0-0":
		return "inventory-rebalancing", "inventory-rebalancing@1.0.0", "Inventory Rebalancing", true
	case "trend":
		return "trend-following", "trend-following@1.0.0", "Trend Following", true
	case "mean-reversion":
		return "mean-reversion", "mean-reversion@1.0.0", "Mean Reversion", true
	case "triangular":
		return "triangular-arbitrage", "triangular-arbitrage@1.0.0", "Triangular Arbitrage", true
	case "cross-exchange-arbitrage":
		return "cross-exchange-arbitrage", "cross-exchange-arbitrage@1.0.0", "Cross-Exchange Arbitrage", true
	default:
		return "", "", "", false
	}
}

func ownerRunExchanges(values []string) *[]generated.RunResourceExchanges {
	result := make([]generated.RunResourceExchanges, 0, len(values))
	for _, value := range values {
		exchange := generated.RunResourceExchanges(value)
		if !exchange.Valid() {
			return nil
		}
		result = append(result, exchange)
	}
	return &result
}

func ownerSandboxRunExchanges(values []string) []generated.SandboxExchange {
	result := make([]generated.SandboxExchange, 0, len(values))
	for _, value := range values {
		result = append(result, generated.SandboxExchange(value))
	}
	return result
}

func ownerSandboxRunEnvironment(exchanges []string) generated.RunResourceEnvironment {
	if len(exchanges) == 1 && exchanges[0] == "binance" {
		return generated.RunResourceEnvironment("binance_spot_testnet")
	}
	if len(exchanges) == 1 && exchanges[0] == "bybit" {
		return generated.RunResourceEnvironment("bybit_demo")
	}
	if len(exchanges) == 2 && exchanges[0] == "binance" && exchanges[1] == "bybit" {
		return generated.RunResourceEnvironment("paired_exchange_sandbox")
	}
	return ""
}

func ownerSandboxRunName(strategy string, exchanges []string, instrument *string) string {
	var value *generated.SandboxStrategySessionInstrument
	if instrument != nil {
		candidate := generated.SandboxStrategySessionInstrument(*instrument)
		value = &candidate
	}
	return sandboxStrategySessionDisplayName(strategy, ownerSandboxRunExchanges(exchanges), value)
}

func ownerRunDisplayInstrument(value string) (string, bool) {
	switch value {
	case "BTCUSDT":
		return "BTC/USDT", true
	case "ETHUSDT":
		return "ETH/USDT", true
	default:
		return "", false
	}
}

func ownerRunWaitingReason(mode, state string) string {
	switch state {
	case "QUEUED":
		if mode == "shadow" {
			return "Waiting for the public-data shadow worker to prepare this simulation."
		}
		return "Waiting for a worker to claim this recorded-data run."
	case "PAUSED", "PAUSE_REQUESTED":
		return "Paused by the owner; no new evaluation will start until resumed."
	case "CANCEL_REQUESTED":
		return "Stopping safely; existing work is being reconciled before the run closes."
	}
	return ""
}

var _ console.RunReadService = (*OwnerConsoleStore)(nil)
