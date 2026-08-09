package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/generated"
)

const sandboxRunPortfolioSummarySQL = `
WITH latest AS (
  SELECT DISTINCT ON (valuation.account_id,valuation.account_epoch)
    valuation.*
  FROM sandbox_strategy_risk_valuations valuation
  WHERE valuation.strategy_session_id=$1
  ORDER BY valuation.account_id,valuation.account_epoch,
    valuation.observed_at DESC,valuation.id DESC
)
SELECT
  (SELECT count(*) FROM latest),
  (SELECT count(*) FROM sandbox_accounting_positions position
    WHERE position.strategy_session_id=$1),
  coalesce(
    (SELECT sum(strategy_realized_pnl) FROM latest),
    (SELECT sum(realized_pnl) FROM sandbox_accounting_positions position
      WHERE position.strategy_session_id=$1),0
  )::text,
  coalesce((SELECT sum(strategy_unrealized_pnl) FROM latest),0)::text,
  coalesce((SELECT sum(strategy_total_pnl) FROM latest),0)::text,
  coalesce((SELECT max(observation.account_drawdown)
    FROM latest valuation
    JOIN sandbox_strategy_risk_observations observation
      ON observation.id=valuation.risk_observation_id),0)::text,
  coalesce((SELECT sum(slippage) FROM latest),0)::text`

const sandboxRunPositionsSQL = `
SELECT membership.exchange,position.instrument,position.quantity::text,
  position.total_cost::text,position.weighted_average_cost::text,
  position.realized_pnl::text,position.valuation_state,position.updated_at
FROM sandbox_accounting_positions position
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=position.strategy_session_id
 AND membership.account_id=position.account_id
WHERE position.strategy_session_id=$1
ORDER BY membership.exchange,position.instrument,position.account_id
LIMIT 20`

const sandboxRunFeesSQL = `
SELECT membership.exchange,fee.instrument,fee.asset_symbol,
  fee.fee_quantity::text,fee.rebate_quantity::text
FROM sandbox_accounting_position_fees fee
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=fee.strategy_session_id
 AND membership.account_id=fee.account_id
WHERE fee.strategy_session_id=$1
ORDER BY membership.exchange,fee.instrument,fee.asset_symbol,fee.account_id
LIMIT 20`

const sandboxRunRiskObservationsSQL = `
WITH latest AS (
  SELECT DISTINCT ON (observation.account_id,observation.account_epoch)
    observation.*,membership.exchange
  FROM sandbox_strategy_risk_observations observation
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=observation.strategy_session_id
   AND membership.account_id=observation.account_id
  WHERE observation.strategy_session_id=$1
  ORDER BY observation.account_id,observation.account_epoch,
    observation.observed_at DESC,observation.id DESC
)
SELECT exchange,instrument,policy_version,account_drawdown::text,
  utc_day_loss::text,rolling_24_hour_loss::text,strategy_loss::text,
  asset_exposure::text,combined_exposure::text,exchange_exposure::text,
  reserve::text,reserved_capital::text,spread::text,slippage::text,
  open_orders,quality_score,gap,stale_data,reconciliation_fault,
  accounting_fault,unknown_order,persistence_fault,disk_fault,api_error,
  lease_lost,observed_at,evidence_hash::text
FROM latest
ORDER BY exchange,instrument`

func (store *OwnerConsoleStore) sandboxRunPortfolio(
	ctx context.Context,
	run generated.RunResource,
) (generated.RunPortfolioProjection, error) {
	var valuationCount, positionCount int64
	var realized, unrealized, total, drawdown, slippage string
	err := store.pool.QueryRow(ctx, sandboxRunPortfolioSummarySQL, run.Id).Scan(
		&valuationCount, &positionCount, &realized, &unrealized, &total, &drawdown, &slippage,
	)
	if err != nil {
		return generated.RunPortfolioProjection{}, err
	}
	if valuationCount == 0 && positionCount == 0 {
		reason := "No sandbox fill or immutable central-risk valuation has been recorded for this run yet."
		return generated.RunPortfolioProjection{
			State: generated.RunPortfolioProjectionStateNotRecorded, WaitingReason: &reason,
		}, nil
	}
	positions, err := store.sandboxRunPositions(ctx, run.Id)
	if err != nil {
		return generated.RunPortfolioProjection{}, err
	}
	fees, err := store.sandboxRunFees(ctx, run.Id)
	if err != nil {
		return generated.RunPortfolioProjection{}, err
	}
	projection := sandboxPortfolioProjection(
		valuationCount, positionCount, realized, unrealized, total, drawdown, slippage, positions, fees,
	)
	return sealRunPortfolioProjection(projection)
}

func sandboxPortfolioProjection(
	valuationCount, positionCount int64,
	realized, unrealized, total, drawdown, slippage string,
	positions []generated.RunPortfolioPosition,
	fees []generated.RunPortfolioFee,
) generated.RunPortfolioProjection {
	ordinal := strconv.FormatInt(valuationCount+positionCount, 10)
	summary := "Sandbox fills are journaled; the next immutable central-risk valuation has not been recorded yet."
	if valuationCount > 0 {
		summary = fmt.Sprintf("Latest immutable accounting and central-risk valuation recorded for %d exchange account(s).", valuationCount)
	}
	projection := generated.RunPortfolioProjection{
		State: generated.RunPortfolioProjectionStateRecorded, Summary: &summary,
		Ordinal: &ordinal, Positions: &positions, Fees: &fees,
	}
	value := generated.Decimal(realized)
	projection.RealizedPnl = &value
	if valuationCount > 0 {
		unrealizedValue, totalValue := generated.Decimal(unrealized), generated.Decimal(total)
		drawdownValue, slippageValue := generated.NonnegativeDecimal(drawdown), generated.NonnegativeDecimal(slippage)
		projection.UnrealizedPnl, projection.TotalPnl = &unrealizedValue, &totalValue
		projection.AccountDrawdown, projection.Slippage = &drawdownValue, &slippageValue
	}
	return projection
}

func (store *OwnerConsoleStore) sandboxRunPositions(
	ctx context.Context,
	id string,
) ([]generated.RunPortfolioPosition, error) {
	rows, err := store.pool.Query(ctx, sandboxRunPositionsSQL, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.RunPortfolioPosition, 0)
	for rows.Next() {
		var exchange, instrument, quantity, totalCost, averageCost, realized, state string
		var updated time.Time
		if err = rows.Scan(&exchange, &instrument, &quantity, &totalCost, &averageCost,
			&realized, &state, &updated); err != nil {
			return nil, err
		}
		item := generated.RunPortfolioPosition{
			Exchange: generated.RunPortfolioPositionExchange(exchange), Instrument: instrument,
			Quantity: generated.NonnegativeDecimal(quantity), TotalCost: generated.NonnegativeDecimal(totalCost),
			WeightedAverageCost: generated.NonnegativeDecimal(averageCost), RealizedPnl: generated.Decimal(realized),
			ValuationState: generated.RunPortfolioPositionValuationState(state), UpdatedAt: generated.Timestamp(updated.UTC()),
		}
		if !item.Exchange.Valid() || !item.ValuationState.Valid() {
			return nil, fmt.Errorf("sandbox_run_portfolio_invalid")
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *OwnerConsoleStore) sandboxRunFees(
	ctx context.Context,
	id string,
) ([]generated.RunPortfolioFee, error) {
	rows, err := store.pool.Query(ctx, sandboxRunFeesSQL, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.RunPortfolioFee, 0)
	for rows.Next() {
		var exchange, instrument, asset, fee, rebate string
		if err = rows.Scan(&exchange, &instrument, &asset, &fee, &rebate); err != nil {
			return nil, err
		}
		item := generated.RunPortfolioFee{Exchange: generated.RunPortfolioFeeExchange(exchange),
			Instrument: instrument, Asset: asset, Fee: generated.NonnegativeDecimal(fee),
			Rebate: generated.NonnegativeDecimal(rebate)}
		if !item.Exchange.Valid() {
			return nil, fmt.Errorf("sandbox_run_portfolio_invalid")
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func sealRunPortfolioProjection(
	projection generated.RunPortfolioProjection,
) (generated.RunPortfolioProjection, error) {
	canonical, err := json.Marshal(projection)
	if err != nil {
		return generated.RunPortfolioProjection{}, fmt.Errorf("sandbox_run_portfolio_invalid")
	}
	digest := sha256.Sum256(canonical)
	payload, hash := string(canonical), hex.EncodeToString(digest[:])
	projection.CanonicalPayload, projection.ContentHash = &payload, &hash
	return projection, nil
}

func (store *OwnerConsoleStore) sandboxRunRisk(
	ctx context.Context,
	run generated.RunResource,
) (generated.RunRiskProjection, error) {
	observations, healthBlockers, err := store.sandboxRunRiskObservations(ctx, run.Id)
	if err != nil {
		return generated.RunRiskProjection{}, err
	}
	blockers := append([]string(nil), healthBlockers...)
	status := generated.RunRiskProjectionStatusNormal
	if run.State == "blocked" {
		status = generated.RunRiskProjectionStatusBlocked
		if run.WaitingReason != nil {
			blockers = appendUniqueString(blockers, *run.WaitingReason)
		}
	} else if len(observations) == 0 {
		status = generated.RunRiskProjectionStatusWaiting
		if run.WaitingReason != nil {
			blockers = appendUniqueString(blockers, *run.WaitingReason)
		}
	} else if len(healthBlockers) > 0 {
		status = generated.RunRiskProjectionStatusBlocked
	}
	if len(observations) == 0 {
		return generated.RunRiskProjection{State: generated.RunRiskProjectionStateNotRecorded, Status: &status,
			Summary: "No run-scoped central-risk observation has been recorded yet.", Blockers: &blockers}, nil
	}
	summary := fmt.Sprintf("Latest immutable central-risk inputs recorded for %d exchange account(s).", len(observations))
	return generated.RunRiskProjection{State: generated.RunRiskProjectionStateRecorded, Status: &status,
		Summary: summary, Blockers: &blockers, Observations: &observations}, nil
}

func (store *OwnerConsoleStore) sandboxRunRiskObservations(
	ctx context.Context,
	id string,
) ([]generated.RunRiskObservation, []string, error) {
	rows, err := store.pool.Query(ctx, sandboxRunRiskObservationsSQL, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]generated.RunRiskObservation, 0)
	blockers := make([]string, 0)
	for rows.Next() {
		item, flags, scanErr := scanSandboxRunRiskObservation(rows)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		item.HealthBlockers = sandboxRiskHealthBlockers(flags)
		for _, blocker := range item.HealthBlockers {
			blockers = appendUniqueString(blockers, blocker)
		}
		items = append(items, item)
	}
	return items, blockers, rows.Err()
}

type sandboxRunRiskFlags struct {
	gap, staleData, reconciliation, accounting, unknownOrder bool
	persistence, disk, apiError, leaseLost                   bool
}

func scanSandboxRunRiskObservation(scanner ownerRunScanner) (
	generated.RunRiskObservation,
	sandboxRunRiskFlags,
	error,
) {
	var exchange, instrument, drawdown, utcLoss, rollingLoss, strategyLoss string
	var assetExposure, combinedExposure, exchangeExposure, reserve, reserved, spread, slippage string
	var policyVersion int64
	var observed time.Time
	var evidenceHash string
	var flags sandboxRunRiskFlags
	item := generated.RunRiskObservation{}
	err := scanner.Scan(&exchange, &instrument, &policyVersion, &drawdown, &utcLoss, &rollingLoss,
		&strategyLoss, &assetExposure, &combinedExposure, &exchangeExposure, &reserve, &reserved,
		&spread, &slippage, &item.OpenOrders, &item.QualityScore, &flags.gap, &flags.staleData,
		&flags.reconciliation, &flags.accounting, &flags.unknownOrder, &flags.persistence, &flags.disk,
		&flags.apiError, &flags.leaseLost, &observed, &evidenceHash)
	if err != nil {
		return generated.RunRiskObservation{}, sandboxRunRiskFlags{}, err
	}
	item.Exchange, item.Instrument = generated.RunRiskObservationExchange(exchange), instrument
	item.PolicyVersion, item.AccountDrawdown = strconv.FormatInt(policyVersion, 10), generated.NonnegativeDecimal(drawdown)
	item.UtcDayLoss, item.Rolling24HourLoss = generated.NonnegativeDecimal(utcLoss), generated.NonnegativeDecimal(rollingLoss)
	item.StrategyLoss, item.AssetExposure = generated.NonnegativeDecimal(strategyLoss), generated.NonnegativeDecimal(assetExposure)
	item.CombinedExposure, item.ExchangeExposure = generated.NonnegativeDecimal(combinedExposure), generated.NonnegativeDecimal(exchangeExposure)
	item.Reserve, item.ReservedCapital = generated.NonnegativeDecimal(reserve), generated.NonnegativeDecimal(reserved)
	item.Spread, item.Slippage = generated.NonnegativeDecimal(spread), generated.NonnegativeDecimal(slippage)
	item.ObservedAt, item.EvidenceHash = generated.Timestamp(observed.UTC()), evidenceHash
	if !item.Exchange.Valid() || policyVersion <= 0 || evidenceHash == "" {
		return generated.RunRiskObservation{}, sandboxRunRiskFlags{}, fmt.Errorf("sandbox_run_risk_invalid")
	}
	return item, flags, nil
}

func sandboxRiskHealthBlockers(flags sandboxRunRiskFlags) []string {
	checks := []struct {
		active bool
		label  string
	}{
		{flags.gap, "market-data sequence gap"}, {flags.staleData, "stale market data"},
		{flags.reconciliation, "reconciliation fault"}, {flags.accounting, "accounting fault"},
		{flags.unknownOrder, "unknown order"}, {flags.persistence, "persistence fault"},
		{flags.disk, "storage fault"}, {flags.apiError, "exchange API fault"}, {flags.leaseLost, "execution lease lost"},
	}
	result := make([]string, 0)
	for _, check := range checks {
		if check.active {
			result = append(result, check.label)
		}
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
