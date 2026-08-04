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

func (store *A11ConsoleStore) populateD3Shadow(ctx context.Context, item *generated.ShadowSessionResource) error {
	if item.RunId == nil || *item.RunId == "" {
		return nil
	}
	runID := *item.RunId
	decisions, err := store.d3ShadowDecisions(ctx, runID)
	if err != nil {
		return err
	}
	balances, positions, err := store.d3ShadowInventory(ctx, runID)
	if err != nil {
		return err
	}
	pnl, err := store.d3ShadowPnl(ctx, runID)
	if err != nil {
		return err
	}
	item.Decisions, item.Balances, item.Positions = &decisions, &balances, &positions
	item.PnlAttribution = &pnl
	if item.ExchangeId != nil {
		health, healthErr := store.d3ShadowHealth(ctx, *item.ExchangeId)
		if healthErr != nil && !errors.Is(healthErr, console.ErrNotFound) {
			return healthErr
		}
		if healthErr == nil {
			item.DataHealth = &health
		}
	}
	return nil
}

func (store *A11ConsoleStore) d3ShadowDecisions(ctx context.Context, runID string) ([]generated.ShadowDecisionSummary, error) {
	rows, err := store.pool.Query(ctx, `SELECT decision.id,decision.outcome,decision.reason_code,
coalesce(risk.outcome,'not_evaluated'),coalesce(risk.reason_code,'not_evaluated'),decision.decided_at
FROM decisions decision LEFT JOIN LATERAL (
 SELECT outcome,reason_code FROM risk_evaluations WHERE decision_id=decision.id
 ORDER BY evaluated_at DESC,id DESC LIMIT 1
) risk ON true WHERE decision.run_id=$1 ORDER BY decision.decided_at DESC,decision.id DESC LIMIT 50`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.ShadowDecisionSummary{}
	for rows.Next() {
		var item generated.ShadowDecisionSummary
		var riskOutcome string
		if err = rows.Scan(&item.Id, &item.Outcome, &item.ReasonCode, &riskOutcome,
			&item.RiskReasonCode, &item.OccurredAt); err != nil {
			return nil, err
		}
		item.RiskOutcome = generated.ShadowDecisionSummaryRiskOutcome(riskOutcome)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d3ShadowInventory(
	ctx context.Context,
	runID string,
) ([]generated.ShadowBalance, []generated.ShadowPosition, error) {
	balances := []generated.ShadowBalance{}
	rows, err := store.pool.Query(ctx, `SELECT balance.asset_symbol,balance.available::text,balance.reserved::text,
balance.revision,balance.updated_at FROM virtual_balances balance JOIN virtual_accounts account ON account.id=balance.account_id
WHERE account.run_id=$1 ORDER BY balance.asset_symbol,account.id`, runID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var item generated.ShadowBalance
		var revision int64
		if err = rows.Scan(&item.Asset, &item.Available, &item.Reserved, &revision, &item.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		balances = append(balances, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	positions := []generated.ShadowPosition{}
	rows, err = store.pool.Query(ctx, `SELECT position.instrument_id,position.quantity::text,
position.weighted_average_cost::text,position.realized_pnl::text,position.revision,position.updated_at
FROM positions position JOIN virtual_accounts account ON account.id=position.account_id
WHERE account.run_id=$1 ORDER BY position.instrument_id,account.id`, runID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item generated.ShadowPosition
		var revision int64
		if err = rows.Scan(&item.Instrument, &item.Quantity, &item.WeightedAverageCost,
			&item.RealizedPnl, &revision, &item.UpdatedAt); err != nil {
			return nil, nil, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		positions = append(positions, item)
	}
	return balances, positions, rows.Err()
}

func (store *A11ConsoleStore) d3ShadowPnl(ctx context.Context, runID string) (generated.ShadowPnlAttribution, error) {
	item := generated.ShadowPnlAttribution{ValuationBasis: generated.SealedLedgerFunctionalValue}
	err := store.pool.QueryRow(ctx, `SELECT
coalesce(sum(entry.functional_value) FILTER (WHERE entry.account_class='realized_pnl'),0)::text,
coalesce(sum(entry.functional_value) FILTER (WHERE entry.account_class='fee_expense'),0)::text,
coalesce(sum(entry.functional_value) FILTER (WHERE entry.account_class='spread_attribution'),0)::text,
coalesce(sum(entry.functional_value) FILTER (WHERE entry.account_class='slippage_attribution'),0)::text,
coalesce(sum(entry.functional_value) FILTER (WHERE entry.account_class='latency_attribution'),0)::text
FROM journal_transactions journal JOIN ledger_entries entry ON entry.transaction_id=journal.id
WHERE journal.run_id=$1 AND journal.sealed`, runID).Scan(&item.RealizedPnl, &item.FeeExpense,
		&item.Spread, &item.Slippage, &item.Latency)
	return item, err
}

func (store *A11ConsoleStore) d3ShadowHealth(ctx context.Context, exchange string) (generated.ShadowDataHealth, error) {
	var item generated.ShadowDataHealth
	err := store.pool.QueryRow(ctx, `SELECT exchange_id,state,reason,observed_at FROM public_connection_events
WHERE exchange_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1`, exchange).Scan(
		&item.Exchange, &item.State, &item.Reason, &item.ObservedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ShadowDataHealth{}, console.ErrNotFound
	}
	if err != nil {
		return generated.ShadowDataHealth{}, err
	}
	age := store.clock.Now().UTC.Sub(item.ObservedAt)
	item.Fresh = age >= 0 && age <= 5*time.Minute &&
		(item.State == generated.ShadowDataHealthState("HEALTHY") || item.State == generated.ShadowDataHealthState("SUBSCRIBED"))
	return item, nil
}
