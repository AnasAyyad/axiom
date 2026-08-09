package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/sandbox"
)

// StrategySessionSagaWork loads every account membership for one already
// fenced coordinator work item. It grants no peer fence or submission
// authority: each account's current lease, arm, admission, and dispatcher
// checks remain independently mandatory before a multi-leg plan can commit.
func (store *SandboxRuntimeDispatcherStore) StrategySessionSagaWork(
	ctx context.Context,
	coordinator sandbox.StrategySessionWork,
	now time.Time,
) ([]sandbox.StrategySessionWork, error) {
	want, err := sandboxStrategySagaWorkCount(coordinator)
	if err != nil {
		return nil, err
	}
	if store == nil || store.pool == nil || ctx == nil || coordinator.ValidAt(now) != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_work_invalid")
	}
	rows, err := store.pool.Query(ctx, strategySessionSagaWorkSQL,
		coordinator.SessionID, coordinator.Strategy, coordinator.Instrument,
		coordinator.ConfigurationID, coordinator.ConfigurationHash,
		coordinator.StrategySetHash, coordinator.SessionRevision,
		coordinator.StrategyRevision, coordinator.ArmID,
		coordinator.ArmRevision, now)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_work_unavailable")
	}
	defer rows.Close()
	items := make([]sandbox.StrategySessionWork, 0, want)
	seen := make(map[sandbox.Exchange]struct{}, want)
	coordinatorFound := false
	for rows.Next() {
		item, scanErr := scanStrategySessionWork(rows, now)
		if scanErr != nil {
			if scanErr.Error() == "sandbox_strategy_session_work_invalid" {
				return nil, fmt.Errorf("sandbox_strategy_saga_work_invalid")
			}
			return nil, fmt.Errorf("sandbox_strategy_saga_work_scan_failed")
		}
		if _, duplicate := seen[item.Account.Exchange]; duplicate {
			return nil, fmt.Errorf("sandbox_strategy_saga_work_invalid")
		}
		seen[item.Account.Exchange] = struct{}{}
		coordinatorFound = coordinatorFound || item == coordinator
		items = append(items, item)
	}
	if rows.Err() != nil || !validSandboxSagaWorkTopology(seen, len(items), want, coordinatorFound) {
		return nil, fmt.Errorf("sandbox_strategy_saga_work_unavailable")
	}
	return items, nil
}

func validSandboxSagaWorkTopology(seen map[sandbox.Exchange]struct{}, count, want int,
	coordinatorFound bool,
) bool {
	if count != want || !coordinatorFound {
		return false
	}
	if want != 2 {
		return true
	}
	_, binance := seen[sandbox.ExchangeBinance]
	_, bybit := seen[sandbox.ExchangeBybit]
	return binance && bybit
}

func sandboxStrategySagaWorkCount(coordinator sandbox.StrategySessionWork) (int, error) {
	if coordinator.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		if coordinator.Account.Exchange != sandbox.ExchangeBinance {
			return 0, fmt.Errorf("sandbox_strategy_saga_coordinator_invalid")
		}
		return 2, nil
	}
	if coordinator.Strategy != sandbox.StrategyTriangular {
		return 0, fmt.Errorf("sandbox_strategy_saga_work_invalid")
	}
	return 1, nil
}

// StrategySessionSagaEligibility restores the exact public readiness records
// for all books required by one saga account. The rows must belong to the same
// startup cycle as the independently validated account admission and must all
// be fresh at the coordinator's decision instant.
func (store *SandboxRuntimeDispatcherStore) StrategySessionSagaEligibility(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	startupCycle uint64,
	instruments []string,
	now time.Time,
) ([]sandbox.EligibilitySnapshot, error) {
	requested, err := validSandboxSagaEligibilityInstruments(instruments)
	if store == nil || store.pool == nil || ctx == nil || work.ValidAt(now) != nil ||
		startupCycle == 0 || err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
	}
	rows, err := store.pool.Query(ctx, strategySessionSagaEligibilitySQL,
		work.Account.ID, int64(work.Account.Epoch), work.Account.Exchange,
		int64(startupCycle), requested, now)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_unavailable")
	}
	defer rows.Close()
	result := make([]sandbox.EligibilitySnapshot, 0, len(requested))
	for rows.Next() {
		var encoded []byte
		var observedAt time.Time
		if err = rows.Scan(&encoded, &observedAt); err != nil {
			return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_scan_failed")
		}
		var eligibility sandbox.EligibilitySnapshot
		if json.Unmarshal(encoded, &eligibility) != nil || !eligibility.Eligible ||
			eligibility.Exchange != string(work.Account.Exchange) ||
			eligibility.ObservedAt.IsZero() || eligibility.ObservedAt.Location() != time.UTC ||
			!eligibility.ObservedAt.Equal(observedAt.UTC()) ||
			eligibility.ObservedAt.After(now) || now.Sub(eligibility.ObservedAt) > 250*time.Millisecond {
			return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
		}
		result = append(result, eligibility)
	}
	if rows.Err() != nil || len(result) != len(requested) {
		return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_unavailable")
	}
	for index, instrument := range requested {
		if result[index].Instrument != instrument {
			return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
		}
	}
	return result, nil
}

func validSandboxSagaEligibilityInstruments(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 3 {
		return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if value != "BTCUSDT" && value != "ETHUSDT" && value != "ETHBTC" {
			return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
		}
		if index > 0 && result[index-1] == value {
			return nil, fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
		}
	}
	return result, nil
}

const strategySessionSagaWorkSQL = `
SELECT strategy.id,strategy.strategy_id,strategy.instrument,
       parent.configuration_id,configuration.configuration_hash,parent.strategy_set_hash,
       membership.account_epoch,membership.account_id,membership.exchange,
       parent.revision,strategy.revision,arm.id,arm.revision,
       strategy.started_at,arm.expires_at
FROM sandbox_strategy_sessions strategy
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy.id
JOIN sandbox_runtime_sandbox_session_accounts parent_membership
  ON parent_membership.session_id=parent.id
 AND parent_membership.account_id=membership.account_id
 AND parent_membership.account_epoch=membership.account_epoch
JOIN sandbox_runtime_exchange_accounts account
  ON account.id=membership.account_id
 AND account.current_epoch=membership.account_epoch
JOIN sandbox_runtime_account_leases lease
  ON lease.account_id=account.id
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE strategy.id=$1
  AND strategy.strategy_id=$2
  AND strategy.instrument=$3
  AND parent.configuration_id=$4
  AND configuration.configuration_hash=$5
  AND parent.strategy_set_hash=$6
  AND parent.revision=$7
  AND strategy.revision=$8
  AND arm.id=$9
  AND arm.revision=$10
  AND parent.state='ARMED'
  AND strategy.state='running'
  AND arm.revoked_at IS NULL
  AND arm.created_at<=$11
  AND arm.expires_at>$11
  AND lease.expires_at>$11
ORDER BY membership.exchange,membership.account_id`

const strategySessionSagaEligibilitySQL = `
SELECT eligibility,observed_at
FROM sandbox_runtime_engine_market_observations
WHERE account_id=$1
  AND account_epoch=$2
  AND exchange=$3
  AND startup_cycle=$4
  AND instrument=ANY($5::text[])
  AND (eligibility->>'eligible')::boolean
  AND observed_at<=$6
  AND $6-observed_at<=interval '250 milliseconds'
ORDER BY instrument`
