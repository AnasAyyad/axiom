package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type sandboxRiskAccountingFacts struct {
	State          string
	EvidenceHash   string
	ProjectionHash string
	Quantity       domain.Balance
	TotalCost      domain.Money
	RealizedPnL    domain.PnL
}

type sandboxRiskOperationalFacts struct {
	OpenOrders         uint32
	CommittedBuyValue  domain.Money
	Slippage           domain.Percent
	ReconciliationID   string
	ReconciliationHash string
	StorageRevision    uint64
	StorageObservedAt  time.Time
	EngineStartupCycle uint64
}

type sandboxRiskValuationHistory struct {
	Ready                       bool
	AccountPeakEquity           domain.Money
	UTCDayBaselineEquity        domain.Money
	Rolling24HourBaselineEquity domain.Money
	StrategyPeakPnL             domain.PnL
}

const sandboxRiskAccountingCountSQL = `
SELECT count(*) FROM sandbox_accounting_transactions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3
  AND ($5 OR instrument=$4)`

const sandboxRiskAccountingProjectionSQL = `
SELECT quantity::text,total_cost::text,realized_pnl::text,valuation_state,
       projection_hash::text,source_transaction_count,last_occurred_at
FROM sandbox_accounting_positions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3 AND instrument=$4
FOR SHARE`

const sandboxRiskOpenOrdersSQL = `
SELECT count(*),count(*) FILTER (WHERE state='UNKNOWN'),
       coalesce(sum(reserved_notional) FILTER (WHERE side='buy'),0)::text
FROM sandbox_runtime_submission_outbox
WHERE account_id=$1 AND account_epoch=$2
  AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`

const sandboxRiskReconciliationSQL = `
SELECT id,evidence_hash::text,state,reconciled_at FROM sandbox_runtime_reconciliations
WHERE account_id=$1 AND account_epoch=$2 AND reconciled_at<=$3
ORDER BY reconciled_at DESC,id DESC LIMIT 1`

const sandboxRiskStorageSQL = `
SELECT level,revision,observed_at FROM owner_console_storage_pressure_state
WHERE scope_id='market-data'`

const sandboxRiskEngineHealthSQL = `
SELECT startup_cycle,observed_at FROM sandbox_runtime_engine_observations
WHERE account_id=$1 AND account_epoch=$2 AND exchange=$3
  AND private_stream_healthy AND reconciliation_clean AND evidence_healthy`

const sandboxRiskSlippageSQL = `
SELECT accounting.side,accounting.price::text,outbox.limit_price::text
FROM sandbox_accounting_transactions accounting
JOIN sandbox_runtime_submission_outbox outbox
  ON outbox.account_id=accounting.account_id
 AND outbox.account_epoch=accounting.account_epoch
 AND outbox.order_id=accounting.order_id
WHERE accounting.strategy_session_id=$1 AND accounting.account_id=$2
  AND accounting.account_epoch=$3 AND ($5 OR accounting.instrument=$4)
ORDER BY accounting.occurred_at,accounting.fill_ordinal,accounting.id`

const sandboxRiskValuationHistorySQL = `
SELECT strategy_session_id,account_equity::text,strategy_total_pnl::text,observed_at
FROM sandbox_strategy_risk_valuations
WHERE account_id=$1 AND account_epoch=$2 AND observed_at<=$3
ORDER BY observed_at,id FOR SHARE`

const insertSandboxRiskValuationSQL = `
INSERT INTO sandbox_strategy_risk_valuations(
 id,purpose,strategy_session_id,account_id,account_epoch,strategy_revision,instrument,
 snapshot_hash,market_hash,policy_id,policy_version,policy_hash,
 accounting_state,accounting_evidence_hash,accounting_projection_hash,mark_price,
 account_equity,volatile_asset_value,combined_volatile_value,committed_buy_value,
 exchange_risk_value,reserve_value,reserved_value,strategy_position_quantity,
 strategy_position_value,strategy_total_cost,strategy_realized_pnl,
 strategy_unrealized_pnl,strategy_total_pnl,account_peak_equity,
 utc_day_baseline_equity,rolling_24_hour_baseline_equity,strategy_peak_pnl,
 open_orders,slippage,reconciliation_id,reconciliation_hash,storage_revision,
 storage_observed_at,engine_startup_cycle,admission_hash,risk_observation_id,
 observed_at,recorded_at,evidence_hash
) VALUES(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
 $21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,
 $39,$40,$41,$42,$43,$44,$45
)
ON CONFLICT DO NOTHING`

// ProjectStrategyRiskObservation derives a complete observation from exact
// durable/account/public facts and persists both valuation and observation in
// one fenced transaction. A first call records only the missing baseline and
// returns unavailable, so missing history never becomes an optimistic zero.
func (store *SandboxRuntimeDispatcherStore) ProjectStrategyRiskObservation(
	ctx context.Context,
	lease sandbox.StrategySessionExecutionLease,
	admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	now time.Time,
) (sandbox.StrategyRiskObservation, error) {
	work := admission.Work
	if store == nil || ctx == nil || lease.ValidFor(work) != nil || admission.Valid() != nil ||
		!admission.ApprovedAt.Equal(now) || snapshot.Validate() != nil ||
		facts.ValidFor(work, snapshot, now) != nil || sandbox.StrategyMarketEvidenceHash(market) == "" ||
		market.Instrument.Symbol() != work.Instrument {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, work.Account.ID, lease.Owner, lease.Fence, now); err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_fence_invalid")
	}
	valuation, history, err := buildSandboxRiskProjection(ctx, tx, admission, snapshot, market, facts, now)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	return persistSandboxRiskProjection(ctx, tx, work, admission, snapshot, market, facts,
		valuation, history, now)
}

func buildSandboxRiskProjection(ctx context.Context, tx pgx.Tx, admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot, market sandbox.StrategyMarketInput, facts sandbox.StrategyRiskFacts,
	now time.Time,
) (sandbox.StrategyRiskValuation, sandboxRiskValuationHistory, error) {
	work := admission.Work
	accounting, err := loadSandboxRiskAccountingFacts(ctx, tx, work, now)
	if err != nil {
		return sandbox.StrategyRiskValuation{}, sandboxRiskValuationHistory{}, err
	}
	operational, err := loadSandboxRiskOperationalFacts(ctx, tx, admission, now)
	if err != nil {
		return sandbox.StrategyRiskValuation{}, sandboxRiskValuationHistory{}, err
	}
	valuation, err := buildSandboxRiskCurrentValuation(
		work, snapshot, market, facts, admission, accounting, operational, now,
	)
	if err != nil {
		return sandbox.StrategyRiskValuation{}, sandboxRiskValuationHistory{}, err
	}
	history, err := loadSandboxRiskValuationHistory(ctx, tx, valuation, now)
	if err != nil {
		return sandbox.StrategyRiskValuation{}, sandboxRiskValuationHistory{}, err
	}
	valuation.AccountPeakEquity = history.AccountPeakEquity
	valuation.UTCDayBaselineEquity = history.UTCDayBaselineEquity
	valuation.Rolling24HourBaselineEquity = history.Rolling24HourBaselineEquity
	valuation.StrategyPeakPnL = history.StrategyPeakPnL
	valuation.Purpose = sandbox.StrategyRiskValuationEvaluated
	if !history.Ready {
		valuation.Purpose = sandbox.StrategyRiskValuationBaseline
	}
	if valuation.ValidFor(work, snapshot, market, facts, admission, now) != nil {
		return sandbox.StrategyRiskValuation{}, sandboxRiskValuationHistory{},
			fmt.Errorf("sandbox_strategy_risk_projection_invalid")
	}
	return valuation, history, nil
}

func persistSandboxRiskProjection(ctx context.Context, tx pgx.Tx, work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission, snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput, facts sandbox.StrategyRiskFacts,
	valuation sandbox.StrategyRiskValuation, history sandboxRiskValuationHistory, now time.Time,
) (sandbox.StrategyRiskObservation, error) {
	if !history.Ready {
		if err := insertSandboxRiskValuation(ctx, tx, valuation, "", now); err != nil {
			return sandbox.StrategyRiskObservation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_commit_failed")
		}
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_baseline_initialized")
	}
	observation, err := valuation.Observation(work, snapshot, market, facts, admission, now)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_invalid")
	}
	observationID, err := insertSandboxStrategyRiskObservation(
		ctx, tx, work, snapshot, facts, observation, now,
	)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	if err = insertSandboxRiskValuation(ctx, tx, valuation, observationID, now); err != nil {
		return sandbox.StrategyRiskObservation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_projection_commit_failed")
	}
	return observation, nil
}

func loadSandboxRiskAccountingFacts(
	ctx context.Context,
	tx pgx.Tx,
	work sandbox.StrategySessionWork,
	now time.Time,
) (sandboxRiskAccountingFacts, error) {
	var transactionCount int64
	portfolioWide := work.Strategy == sandbox.StrategyTriangular
	if err := tx.QueryRow(ctx, sandboxRiskAccountingCountSQL, work.SessionID, work.Account.ID, work.Account.Epoch,
		work.Instrument, portfolioWide).Scan(&transactionCount); err != nil || transactionCount < 0 {
		return sandboxRiskAccountingFacts{}, fmt.Errorf("sandbox_strategy_risk_accounting_unavailable")
	}
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPnL, _ := domain.ParsePnL("0")
	if transactionCount == 0 {
		return sandboxRiskAccountingFacts{State: sandbox.StrategyRiskAccountingNoFills,
			EvidenceHash: stableSandboxRuntimeHash("sandbox-risk-no-fills", string(work.SessionID),
				string(work.Account.ID), fmt.Sprintf("%d", work.Account.Epoch), work.Instrument),
			Quantity: zeroBalance, TotalCost: zeroMoney, RealizedPnL: zeroPnL}, nil
	}
	var quantity, cost, realized, state, projectionHash string
	var sourceCount int64
	var lastOccurredAt time.Time
	err := tx.QueryRow(ctx, sandboxRiskAccountingProjectionSQL,
		work.SessionID, work.Account.ID, work.Account.Epoch, work.Instrument).Scan(
		&quantity, &cost, &realized, &state, &projectionHash, &sourceCount, &lastOccurredAt,
	)
	if err != nil || state != sandboxAccountingValuationComplete || sourceCount != transactionCount ||
		sourceCount <= 0 || projectionHash == "" || lastOccurredAt.IsZero() || lastOccurredAt.UTC().After(now) {
		return sandboxRiskAccountingFacts{}, fmt.Errorf("sandbox_strategy_risk_accounting_unavailable")
	}
	parsedQuantity, quantityErr := domain.ParseBalance(quantity)
	parsedCost, costErr := domain.ParseMoney(cost)
	parsedRealized, realizedErr := domain.ParsePnL(realized)
	if quantityErr != nil || costErr != nil || realizedErr != nil {
		return sandboxRiskAccountingFacts{}, fmt.Errorf("sandbox_strategy_risk_accounting_invalid")
	}
	return sandboxRiskAccountingFacts{State: sandbox.StrategyRiskAccountingComplete,
		EvidenceHash: projectionHash, ProjectionHash: projectionHash, Quantity: parsedQuantity,
		TotalCost: parsedCost, RealizedPnL: parsedRealized}, nil
}

func loadSandboxRiskOperationalFacts(
	ctx context.Context,
	tx pgx.Tx,
	admission sandbox.StrategySessionAdmission,
	now time.Time,
) (sandboxRiskOperationalFacts, error) {
	work := admission.Work
	var openOrders, unknownOrders int64
	var committed string
	err := tx.QueryRow(ctx, sandboxRiskOpenOrdersSQL,
		work.Account.ID, work.Account.Epoch).Scan(&openOrders, &unknownOrders, &committed)
	if err != nil || openOrders < 0 || uint64(openOrders) > uint64(^uint32(0)) || unknownOrders != 0 ||
		uint32(openOrders) >= 1 || !admission.Safety.OpenCapacityAvailable {
		return sandboxRiskOperationalFacts{}, fmt.Errorf("sandbox_strategy_risk_orders_unavailable")
	}
	committedBuy, err := domain.ParseMoney(committed)
	if err != nil {
		return sandboxRiskOperationalFacts{}, fmt.Errorf("sandbox_strategy_risk_orders_invalid")
	}
	var reconciliationID, reconciliationHash, reconciliationState string
	var reconciledAt time.Time
	err = tx.QueryRow(ctx, sandboxRiskReconciliationSQL, work.Account.ID, work.Account.Epoch, now).Scan(
		&reconciliationID, &reconciliationHash, &reconciliationState, &reconciledAt,
	)
	if err != nil || reconciliationState != "clean" || !admission.Safety.ReconciliationClean ||
		reconciledAt.IsZero() || now.Sub(reconciledAt.UTC()) > 60*time.Second {
		return sandboxRiskOperationalFacts{}, fmt.Errorf("sandbox_strategy_risk_reconciliation_unavailable")
	}
	var storageLevel string
	var storageRevision, engineCycle int64
	var storageObservedAt, engineObservedAt time.Time
	err = tx.QueryRow(ctx, sandboxRiskStorageSQL).Scan(&storageLevel, &storageRevision, &storageObservedAt)
	if err != nil || storageLevel != "NORMAL" || storageRevision <= 0 ||
		storageObservedAt.IsZero() || storageObservedAt.After(now) ||
		now.Sub(storageObservedAt.UTC()) > 30*time.Second {
		return sandboxRiskOperationalFacts{}, fmt.Errorf("sandbox_strategy_risk_storage_unavailable")
	}
	err = tx.QueryRow(ctx, sandboxRiskEngineHealthSQL,
		work.Account.ID, work.Account.Epoch, work.Account.Exchange).Scan(&engineCycle, &engineObservedAt)
	if err != nil || engineCycle <= 0 || uint64(engineCycle) != admission.StartupCycle ||
		engineObservedAt.IsZero() || engineObservedAt.After(now) || now.Sub(engineObservedAt.UTC()) > 2*time.Second {
		return sandboxRiskOperationalFacts{}, fmt.Errorf("sandbox_strategy_risk_engine_health_unavailable")
	}
	slippage, err := loadSandboxRiskSlippage(ctx, tx, work)
	if err != nil {
		return sandboxRiskOperationalFacts{}, err
	}
	return newSandboxRiskOperationalFacts(openOrders, committedBuy, slippage, reconciliationID,
		reconciliationHash, storageRevision, storageObservedAt, engineCycle), nil
}

func newSandboxRiskOperationalFacts(openOrders int64, committed domain.Money, slippage domain.Percent,
	reconciliationID, reconciliationHash string, storageRevision int64, storageObservedAt time.Time,
	engineCycle int64,
) sandboxRiskOperationalFacts {
	return sandboxRiskOperationalFacts{OpenOrders: uint32(openOrders), CommittedBuyValue: committed,
		Slippage: slippage, ReconciliationID: reconciliationID, ReconciliationHash: reconciliationHash,
		StorageRevision: uint64(storageRevision), StorageObservedAt: storageObservedAt.UTC(),
		EngineStartupCycle: uint64(engineCycle)}
}

func loadSandboxRiskSlippage(
	ctx context.Context,
	tx pgx.Tx,
	work sandbox.StrategySessionWork,
) (domain.Percent, error) {
	zero, _ := domain.ParsePercent("0")
	worst := zero
	rows, err := tx.Query(ctx, sandboxRiskSlippageSQL,
		work.SessionID, work.Account.ID, work.Account.Epoch, work.Instrument,
		work.Strategy == sandbox.StrategyTriangular)
	if err != nil {
		return domain.Percent{}, fmt.Errorf("sandbox_strategy_risk_slippage_unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var side, fillText, limitText string
		if err = rows.Scan(&side, &fillText, &limitText); err != nil {
			return domain.Percent{}, fmt.Errorf("sandbox_strategy_risk_slippage_invalid")
		}
		value, adverse, valueErr := sandboxRiskAdverseSlippage(side, fillText, limitText)
		if valueErr != nil {
			return domain.Percent{}, fmt.Errorf("sandbox_strategy_risk_slippage_invalid")
		}
		if adverse && value.Compare(worst) > 0 {
			worst = value
		}
	}
	if rows.Err() != nil {
		return domain.Percent{}, fmt.Errorf("sandbox_strategy_risk_slippage_unavailable")
	}
	return worst, nil
}
