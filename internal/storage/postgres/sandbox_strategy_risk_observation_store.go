package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const insertSandboxStrategyRiskObservationSQL = `
INSERT INTO sandbox_strategy_risk_observations(
  id,strategy_session_id,account_id,account_epoch,strategy_revision,instrument,
  snapshot_hash,market_hash,policy_id,policy_version,policy_hash,
  account_drawdown,utc_day_loss,rolling_24_hour_loss,strategy_loss,
  asset_exposure,combined_exposure,exchange_exposure,reserve,reserved_capital,
  spread,slippage,open_orders,book_age_nanoseconds,queue_lag_nanoseconds,
  clock_drift_nanoseconds,quality_score,gap,stale_data,reconciliation_fault,
  accounting_fault,unknown_order,persistence_fault,disk_fault,api_error,
  lease_lost,observed_at,recorded_at,evidence_hash
)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
       $12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,
       $28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39
WHERE EXISTS(
  SELECT 1
  FROM sandbox_strategy_sessions strategy_session
  JOIN sandbox_runtime_sandbox_sessions parent
    ON parent.id=strategy_session.sandbox_session_id
  JOIN configuration_versions configuration
    ON configuration.id=parent.configuration_id
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=strategy_session.id
   AND membership.account_id=$3
   AND membership.account_epoch=$4
  JOIN sandbox_runtime_sandbox_arms arm
    ON arm.sandbox_session_id=parent.id
  WHERE strategy_session.id=$2
    AND strategy_session.strategy_id=$40
    AND strategy_session.instrument=$6
    AND strategy_session.state='running'
    AND strategy_session.revision=$5
    AND parent.state='ARMED'
    AND parent.revision=$41
    AND parent.configuration_id=$42
    AND configuration.configuration_hash=$43
    AND parent.strategy_set_hash=$44
    AND arm.id=$45
    AND arm.revision=$46
    AND arm.revoked_at IS NULL
    AND arm.expires_at>$38
)
ON CONFLICT DO NOTHING`

const sandboxStrategyRiskObservationSQL = `
SELECT observation.strategy_session_id,observation.strategy_revision,
       observation.account_id,observation.account_epoch,
       observation.snapshot_hash::text,observation.market_hash::text,
       observation.instrument,observation.policy_id,observation.policy_version,
       observation.policy_hash::text,observation.account_drawdown::text,
       observation.utc_day_loss::text,observation.rolling_24_hour_loss::text,
       observation.strategy_loss::text,observation.asset_exposure::text,
       observation.combined_exposure::text,observation.exchange_exposure::text,
       observation.reserve::text,observation.reserved_capital::text,
       observation.spread::text,observation.slippage::text,
       observation.open_orders,observation.book_age_nanoseconds,
       observation.queue_lag_nanoseconds,observation.clock_drift_nanoseconds,
       observation.quality_score,observation.gap,observation.stale_data,
       observation.reconciliation_fault,observation.accounting_fault,
       observation.unknown_order,observation.persistence_fault,
       observation.disk_fault,observation.api_error,observation.lease_lost,
       observation.observed_at,observation.evidence_hash::text
FROM sandbox_strategy_risk_observations observation
JOIN sandbox_strategy_sessions strategy_session
  ON strategy_session.id=observation.strategy_session_id
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy_session.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy_session.id
 AND membership.account_id=$2
 AND membership.account_epoch=$3
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE observation.strategy_session_id=$1
  AND observation.account_id=$2
  AND observation.account_epoch=$3
  AND observation.strategy_revision=$4
  AND strategy_session.strategy_id=$5
  AND observation.instrument=$6
  AND strategy_session.instrument=$6
  AND strategy_session.state='running'
  AND strategy_session.revision=$4
  AND parent.state='ARMED'
  AND parent.revision=$7
  AND parent.configuration_id=$8
  AND configuration.configuration_hash=$9
  AND parent.strategy_set_hash=$10
  AND arm.id=$11
  AND arm.revision=$12
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$13
  AND observation.snapshot_hash=$14
  AND observation.market_hash=$15
  AND observation.policy_id=$16
  AND observation.policy_version=$17
  AND observation.policy_hash=$18
  AND observation.observed_at<=$13
  AND observation.observed_at>$13-interval '250 milliseconds'
ORDER BY observation.observed_at DESC,observation.id
LIMIT 1`

// RecordStrategyRiskObservation appends complete central-risk inputs only
// while the exact engine lease, running strategy revision, armed parent, and
// immutable input references remain current. This evidence does not itself
// approve a candidate or authorize submission.
func (store *SandboxRuntimeDispatcherStore) RecordStrategyRiskObservation(
	ctx context.Context,
	owner string,
	fence uint64,
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	observation sandbox.StrategyRiskObservation,
	now time.Time,
) error {
	if store == nil || ctx == nil || owner == "" || fence == 0 ||
		observation.ValidFor(work, snapshot, market, facts, now) != nil {
		return fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("sandbox_strategy_risk_observation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, work.Account.ID, owner, fence, now); err != nil {
		return fmt.Errorf("sandbox_strategy_risk_observation_fence_invalid")
	}
	if _, err = insertSandboxStrategyRiskObservation(ctx, tx, work, snapshot, facts, observation, now); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_strategy_risk_observation_commit_failed")
	}
	return nil
}

func insertSandboxStrategyRiskObservation(
	ctx context.Context,
	tx pgx.Tx,
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	facts sandbox.StrategyRiskFacts,
	observation sandbox.StrategyRiskObservation,
	now time.Time,
) (string, error) {
	evidenceHash := observation.EvidenceHash()
	if evidenceHash == "" {
		return "", fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	id := "strategy-risk-observation-" + evidenceHash[:32]
	tag, err := tx.Exec(ctx, insertSandboxStrategyRiskObservationSQL,
		id, work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		work.Instrument, snapshot.SnapshotHash, observation.MarketHash,
		facts.PolicyID, facts.PolicyVersion, facts.PolicyHash,
		observation.AccountDrawdown.String(), observation.UTCDayLoss.String(),
		observation.Rolling24HourLoss.String(), observation.StrategyLoss.String(),
		observation.AssetExposure.String(), observation.CombinedExposure.String(),
		observation.ExchangeExposure.String(), observation.Reserve.String(),
		observation.ReservedCapital.String(), observation.Spread.String(),
		observation.Slippage.String(), observation.OpenOrders, int64(observation.BookAge),
		int64(observation.QueueLag), int64(observation.ClockDrift), observation.QualityScore,
		observation.Gap, observation.StaleData, observation.ReconciliationFault,
		observation.AccountingFault, observation.UnknownOrder, observation.PersistenceFault,
		observation.DiskFault, observation.APIError, observation.LeaseLost,
		observation.ObservedAt, now, evidenceHash, work.Strategy, work.SessionRevision,
		work.ConfigurationID, work.ConfigurationHash, work.StrategySetHash,
		work.ArmID, work.ArmRevision,
	)
	if err != nil {
		return "", fmt.Errorf("sandbox_strategy_risk_observation_write_failed")
	}
	if tag.RowsAffected() == 0 {
		var recordedHash string
		err = tx.QueryRow(ctx, `
SELECT evidence_hash::text
FROM sandbox_strategy_risk_observations
WHERE id=$1`, id).Scan(&recordedHash)
		if err != nil || recordedHash != evidenceHash {
			return "", fmt.Errorf("sandbox_strategy_risk_observation_stale")
		}
	}
	return id, nil
}

// StrategyRiskObservation reads only a complete observation bound to the
// caller's exact live work item and immutable snapshot, market, and policy
// references. Missing or stale data stays unavailable; no zero-value health
// or loss substitute is constructed.
func (store *SandboxRuntimeDispatcherStore) StrategyRiskObservation(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	now time.Time,
) (sandbox.StrategyRiskObservation, error) {
	marketHash := sandbox.StrategyMarketEvidenceHash(market)
	if store == nil || ctx == nil || work.ValidAt(now) != nil || snapshot.Validate() != nil ||
		facts.ValidFor(work, snapshot, now) != nil || marketHash == "" {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	row, err := store.loadSandboxStrategyRiskObservation(ctx, work, snapshot, marketHash, facts, now)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_observation_unavailable")
	}
	if row.strategyRevision <= 0 || row.accountEpoch <= 0 || row.policyVersion <= 0 ||
		row.openOrders < 0 || uint64(row.openOrders) > uint64(^uint32(0)) ||
		row.qualityScore < 0 || row.qualityScore > 100 {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	parsed, err := parseSandboxStrategyRiskPercentages(row.percentages)
	if err != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	observation := row.observation
	observation.StrategySessionID = sandbox.SessionID(row.sessionID)
	observation.StrategyRevision = uint64(row.strategyRevision)
	observation.AccountID = sandbox.AccountID(row.accountID)
	observation.AccountEpoch = uint64(row.accountEpoch)
	observation.PolicyVersion = uint64(row.policyVersion)
	observation.AccountDrawdown, observation.UTCDayLoss = parsed[0], parsed[1]
	observation.Rolling24HourLoss, observation.StrategyLoss = parsed[2], parsed[3]
	observation.AssetExposure, observation.CombinedExposure = parsed[4], parsed[5]
	observation.ExchangeExposure, observation.Reserve = parsed[6], parsed[7]
	observation.ReservedCapital, observation.Spread, observation.Slippage = parsed[8], parsed[9], parsed[10]
	observation.OpenOrders = uint32(row.openOrders)
	observation.BookAge, observation.QueueLag = time.Duration(row.bookAge), time.Duration(row.queueLag)
	observation.ClockDrift = time.Duration(row.clockDrift)
	observation.QualityScore = uint8(row.qualityScore)
	observation.ObservedAt = observation.ObservedAt.UTC()
	if observation.EvidenceHash() != row.evidenceHash ||
		observation.ValidFor(work, snapshot, market, facts, now) != nil {
		return sandbox.StrategyRiskObservation{}, fmt.Errorf("sandbox_strategy_risk_observation_invalid")
	}
	return observation, nil
}

type sandboxStrategyRiskObservationRow struct {
	observation                                             sandbox.StrategyRiskObservation
	sessionID, accountID, evidenceHash                      string
	strategyRevision, accountEpoch, policyVersion           int64
	percentages                                             [11]string
	openOrders, bookAge, queueLag, clockDrift, qualityScore int64
}

func (store *SandboxRuntimeDispatcherStore) loadSandboxStrategyRiskObservation(ctx context.Context,
	work sandbox.StrategySessionWork, snapshot sandbox.AccountSnapshot, marketHash string,
	facts sandbox.StrategyRiskFacts, now time.Time,
) (sandboxStrategyRiskObservationRow, error) {
	var row sandboxStrategyRiskObservationRow
	err := store.pool.QueryRow(ctx, sandboxStrategyRiskObservationSQL,
		work.SessionID, work.Account.ID, work.Account.Epoch, work.StrategyRevision,
		work.Strategy, work.Instrument, work.SessionRevision, work.ConfigurationID,
		work.ConfigurationHash, work.StrategySetHash, work.ArmID, work.ArmRevision,
		now, snapshot.SnapshotHash, marketHash, facts.PolicyID, facts.PolicyVersion, facts.PolicyHash).Scan(
		&row.sessionID, &row.strategyRevision, &row.accountID, &row.accountEpoch,
		&row.observation.SnapshotHash, &row.observation.MarketHash, &row.observation.Instrument,
		&row.observation.PolicyID, &row.policyVersion, &row.observation.PolicyHash,
		&row.percentages[0], &row.percentages[1], &row.percentages[2], &row.percentages[3],
		&row.percentages[4], &row.percentages[5], &row.percentages[6], &row.percentages[7],
		&row.percentages[8], &row.percentages[9], &row.percentages[10], &row.openOrders,
		&row.bookAge, &row.queueLag, &row.clockDrift, &row.qualityScore,
		&row.observation.Gap, &row.observation.StaleData, &row.observation.ReconciliationFault,
		&row.observation.AccountingFault, &row.observation.UnknownOrder,
		&row.observation.PersistenceFault, &row.observation.DiskFault,
		&row.observation.APIError, &row.observation.LeaseLost,
		&row.observation.ObservedAt, &row.evidenceHash)
	return row, err
}

func parseSandboxStrategyRiskPercentages(values [11]string) ([11]domain.Percent, error) {
	var parsed [11]domain.Percent
	for index, value := range values {
		item, err := domain.ParsePercent(value)
		if err != nil {
			return [11]domain.Percent{}, err
		}
		parsed[index] = item
	}
	return parsed, nil
}

var _ sandbox.StrategyRiskObservationRecorder = (*SandboxRuntimeDispatcherStore)(nil)
var _ sandbox.StrategyRiskObservationSource = (*SandboxRuntimeDispatcherStore)(nil)
