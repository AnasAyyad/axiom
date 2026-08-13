package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
)

// strategyRiskFactsSQL resolves the immutable, currently effective global
// policy and computes its reserve against the exact snapshot supplied by the
// owning engine. The ceiling at the supported decimal scale is deliberate:
// a fractional reserve is never rounded down before a strategy sizes an
// entry.
const strategyRiskFactsSQL = `
WITH current_risk_state AS (
  SELECT coalesce(
    (SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),
    'PAUSED'
  ) AS state
), current_policy AS (
  SELECT policy.id,policy.version,policy.policy_hash::text,
         limits.minimum_reserve::text AS minimum_reserve,
         limits.maximum_reserved_capital::text AS maximum_reserved_capital,
         limits.account_drawdown::text,limits.utc_day_loss::text,
         limits.rolling_24_hour_loss::text,limits.strategy_loss::text,
         limits.asset_exposure::text,limits.combined_exposure::text,
         limits.exchange_exposure::text,limits.maximum_spread::text,
         limits.maximum_slippage::text,limits.maximum_open_orders,
         limits.maximum_book_age_microseconds,limits.maximum_queue_lag_microseconds,
         limits.maximum_clock_drift_microseconds,limits.minimum_quality_score
  FROM risk_policies policy
  JOIN risk_policy_limits limits
    ON limits.policy_id=policy.id AND limits.policy_version=policy.version
  CROSS JOIN current_risk_state
  WHERE policy.scope_kind='global'
    AND policy.scope_id='platform'
    AND policy.state='NORMAL'
    AND policy.effective_at<=$1
    AND current_risk_state.state='NORMAL'
  ORDER BY policy.version DESC
  LIMIT 1
)
SELECT id,version,policy_hash,
       (ceil((($2::numeric+$3::numeric)*minimum_reserve::numeric)*1000000000000000000)
        /1000000000000000000)::text,
       (ceil((($2::numeric+$3::numeric)*maximum_reserved_capital::numeric)*1000000000000000000)
        /1000000000000000000)::text,
       account_drawdown,utc_day_loss,rolling_24_hour_loss,strategy_loss,
       asset_exposure,combined_exposure,exchange_exposure,minimum_reserve,
       maximum_reserved_capital,maximum_spread,maximum_slippage,
       maximum_open_orders,maximum_book_age_microseconds,
       maximum_queue_lag_microseconds,maximum_clock_drift_microseconds,
       minimum_quality_score
FROM current_policy`

// StrategyRiskFacts returns the only reserve facts that automatic strategy
// sizing may use. It reads an immutable policy version and the snapshot-bound
// USDT balance supplied by the authenticated engine; absent, paused, stale,
// malformed, or non-global policy data remains unavailable rather than being
// reconstructed from defaults.
func (store *SandboxRuntimeDispatcherStore) StrategyRiskFacts(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	now time.Time,
) (sandbox.StrategyRiskFacts, error) {
	if store == nil || ctx == nil || work.ValidAt(now) != nil ||
		snapshot.Validate() != nil || snapshot.AccountID != work.Account.ID ||
		snapshot.Epoch != work.Account.Epoch || snapshot.ObservedAt.After(now) ||
		now.Sub(snapshot.ObservedAt) > 250*time.Millisecond {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
	}
	available, reserved, err := sandboxStrategySettlementBalance(snapshot)
	if err != nil {
		return sandbox.StrategyRiskFacts{}, err
	}
	row, err := store.loadSandboxStrategyRiskFacts(ctx, now, available, reserved)
	if err != nil {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_unavailable")
	}
	parsedLimits, err := parseSandboxStrategyRiskPercentages(row.percentages)
	if err != nil {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
	}
	minimumReserve, err := domain.ParseMoney(row.reserve)
	if err != nil {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
	}
	maximumReservedMoney, err := domain.ParseMoney(row.maximumReserved)
	if err != nil {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
	}
	facts := buildSandboxStrategyRiskFacts(row, work, snapshot, parsedLimits,
		minimumReserve, maximumReservedMoney, now)
	if facts.ValidFor(work, snapshot, now) != nil {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
	}
	return facts, nil
}

type sandboxStrategyRiskFactsRow struct {
	policyID, policyHash, reserve, maximumReserved                        string
	version                                                               int64
	percentages                                                           [11]string
	maximumOpenOrders, maximumBookAge, maximumQueueLag, maximumClockDrift int64
	minimumQuality                                                        int64
}

func (store *SandboxRuntimeDispatcherStore) loadSandboxStrategyRiskFacts(ctx context.Context, now time.Time,
	available, reserved domain.Balance,
) (sandboxStrategyRiskFactsRow, error) {
	var row sandboxStrategyRiskFactsRow
	err := store.pool.QueryRow(ctx, strategyRiskFactsSQL, now, available.String(), reserved.String()).Scan(
		&row.policyID, &row.version, &row.policyHash, &row.reserve, &row.maximumReserved,
		&row.percentages[0], &row.percentages[1], &row.percentages[2], &row.percentages[3],
		&row.percentages[4], &row.percentages[5], &row.percentages[6], &row.percentages[7],
		&row.percentages[8], &row.percentages[9], &row.percentages[10], &row.maximumOpenOrders,
		&row.maximumBookAge, &row.maximumQueueLag, &row.maximumClockDrift, &row.minimumQuality)
	if err != nil || row.version <= 0 || row.maximumOpenOrders <= 0 ||
		uint64(row.maximumOpenOrders) > uint64(^uint32(0)) || row.maximumBookAge <= 0 ||
		row.maximumQueueLag <= 0 || row.maximumClockDrift <= 0 || row.minimumQuality <= 0 || row.minimumQuality > 100 {
		return sandboxStrategyRiskFactsRow{}, fmt.Errorf("sandbox_strategy_risk_facts_unavailable")
	}
	return row, nil
}

func buildSandboxStrategyRiskFacts(row sandboxStrategyRiskFactsRow, work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot, limits [11]domain.Percent, minimumReserve, maximumReserved domain.Money,
	now time.Time,
) sandbox.StrategyRiskFacts {
	facts := sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: snapshot.SnapshotHash, PolicyID: row.policyID, PolicyVersion: uint64(row.version),
		PolicyHash: row.policyHash, MinimumReserve: minimumReserve, MaximumReserved: maximumReserved, ObservedAt: now}
	facts.Policy = risk.Policy{ID: facts.PolicyID, Version: facts.PolicyVersion,
		Scope: risk.Scope{Kind: risk.ScopeGlobal, ID: "platform"}, State: risk.StateNormal,
		Limits: risk.Limits{AccountDrawdown: limits[0], DayLoss: limits[1], RollingLoss: limits[2],
			StrategyLoss: limits[3], AssetExposure: limits[4], CombinedExposure: limits[5],
			ExchangeExposure: limits[6], MinimumReserve: limits[7], ReservedCapital: limits[8],
			MaximumSpread: limits[9], MaximumSlippage: limits[10], MaximumOpenOrders: uint32(row.maximumOpenOrders),
			MaximumBookAge:    time.Duration(row.maximumBookAge) * time.Microsecond,
			MaximumQueueLag:   time.Duration(row.maximumQueueLag) * time.Microsecond,
			MaximumClockDrift: time.Duration(row.maximumClockDrift) * time.Microsecond,
			MinimumQuality:    uint8(row.minimumQuality)}}
	return facts
}

func sandboxStrategySettlementBalance(snapshot sandbox.AccountSnapshot) (domain.Balance, domain.Balance, error) {
	var available, reserved domain.Balance
	found := false
	for _, balance := range snapshot.Balances {
		if balance.Asset != "USDT" {
			continue
		}
		if found {
			return domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_strategy_risk_facts_invalid")
		}
		available, reserved, found = balance.Available, balance.Reserved, true
	}
	if !found {
		return domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_strategy_risk_facts_unavailable")
	}
	return available, reserved, nil
}

var _ sandbox.StrategyRiskFactsSource = (*SandboxRuntimeDispatcherStore)(nil)
