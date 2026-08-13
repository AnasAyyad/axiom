package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// strategyOwnedInventorySQL selects only fills from durable STRATEGY plans
// belonging to the exact running, armed strategy session. Account-wide fills,
// manually created sandbox orders, and other strategy sessions are excluded.
const strategyOwnedInventorySQL = `
SELECT DISTINCT ON (outbox.order_id)
       outbox.side,fill.native_fill_id_hash::text,fill.canonical_fill,fill.occurred_at
FROM sandbox_strategy_sessions strategy
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
JOIN sandbox_runtime_submission_plans plan
  ON plan.sandbox_session_id=parent.id AND plan.intent_kind='STRATEGY'
JOIN sandbox_runtime_submission_outbox outbox
  ON outbox.plan_id=plan.id
JOIN sandbox_runtime_exchange_fills fill
  ON fill.account_id=outbox.account_id
 AND fill.account_epoch=outbox.account_epoch
 AND fill.order_id=outbox.order_id
WHERE strategy.id=$1
  AND strategy.state='running'
  AND strategy.revision=$2
  AND parent.state='ARMED'
  AND parent.revision=$3
  AND parent.configuration_id=$4
  AND configuration.configuration_hash=$5
  AND parent.strategy_set_hash=$6
  AND arm.id=$7
  AND arm.revision=$8
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$9
  AND outbox.account_id=$10
  AND outbox.account_epoch=$11
  AND outbox.instrument=$12
  AND fill.occurred_at<=$9
ORDER BY outbox.order_id,fill.occurred_at DESC,fill.native_fill_id_hash DESC`

// StrategyOwnedInventory derives an exact base-asset balance from the latest
// immutable cumulative fill snapshot for each session-owned order, not from
// exchange account-wide holdings. Any malformed, future, or oversold evidence
// causes a fail-closed error.
func (store *SandboxRuntimeDispatcherStore) StrategyOwnedInventory(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	asset domain.AssetSymbol,
	now time.Time,
) (sandbox.StrategyOwnedInventory, error) {
	if store == nil || ctx == nil || work.ValidAt(now) != nil ||
		now.IsZero() || now.Location() != time.UTC ||
		strategyOwnedInventoryAsset(work.Instrument) != asset {
		return sandbox.StrategyOwnedInventory{}, fmt.Errorf("strategy_owned_inventory_invalid")
	}
	rows, err := store.pool.Query(ctx, strategyOwnedInventorySQL,
		work.SessionID, work.StrategyRevision, work.SessionRevision,
		work.ConfigurationID, work.ConfigurationHash, work.StrategySetHash,
		work.ArmID, work.ArmRevision, now,
		work.Account.ID, work.Account.Epoch, work.Instrument,
	)
	if err != nil {
		return sandbox.StrategyOwnedInventory{}, fmt.Errorf("strategy_owned_inventory_unavailable")
	}
	defer rows.Close()
	available, evidence, err := scanStrategyOwnedInventory(rows, work, asset, now)
	if err != nil {
		return sandbox.StrategyOwnedInventory{}, err
	}
	result := sandbox.StrategyOwnedInventory{SessionID: work.SessionID,
		AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch, Asset: asset,
		Available: available, EvidenceHash: stableSandboxRuntimeHash(strings.Join(evidence, "\x00")),
		ObservedAt: now}
	if result.ValidFor(sandbox.StrategySessionAdmission{Work: work, ApprovedAt: now}, asset) != nil {
		return sandbox.StrategyOwnedInventory{}, fmt.Errorf("strategy_owned_inventory_invalid")
	}
	return result, nil
}

func scanStrategyOwnedInventory(rows pgx.Rows, work sandbox.StrategySessionWork,
	asset domain.AssetSymbol, now time.Time,
) (domain.Balance, []string, error) {
	available, err := domain.ParseBalance("0")
	if err != nil {
		return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
	}
	evidence := []string{string(work.SessionID), string(work.Account.ID),
		fmt.Sprintf("%d", work.Account.Epoch), string(asset)}
	for rows.Next() {
		var side, fillHash string
		var raw []byte
		var occurredAt time.Time
		if err = rows.Scan(&side, &fillHash, &raw, &occurredAt); err != nil ||
			occurredAt.IsZero() || occurredAt.Location() != time.UTC || occurredAt.After(now) {
			return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
		}
		var event execution.OrderEvent
		if json.Unmarshal(raw, &event) != nil || len(event.Fills) == 0 || event.CumulativeQuantity.String() == "" {
			return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
		}
		quantity, quantityErr := domain.ParseBalance(event.CumulativeQuantity.String())
		if quantityErr != nil || fillHash == "" {
			return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
		}
		switch side {
		case "buy":
			available, err = available.Add(quantity)
		case "sell":
			available, err = available.Subtract(quantity)
		default:
			return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
		}
		if err != nil {
			return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_invalid")
		}
		evidence = append(evidence, side, fillHash, quantity.String(), occurredAt.Format(time.RFC3339Nano))
	}
	if err = rows.Err(); err != nil {
		return domain.Balance{}, nil, fmt.Errorf("strategy_owned_inventory_unavailable")
	}
	return available, evidence, nil
}

func strategyOwnedInventoryAsset(instrument string) domain.AssetSymbol {
	switch instrument {
	case "BTCUSDT":
		return "BTC"
	case "ETHUSDT":
		return "ETH"
	default:
		return ""
	}
}

var _ sandbox.StrategyOwnedInventorySource = (*SandboxRuntimeDispatcherStore)(nil)
