package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/simulation"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5"
)

// A11MetadataEvidence is the durable identity assigned to one public filter set.
type A11MetadataEvidence struct {
	ID       string
	Metadata domain.InstrumentMetadata
}

type a11ShadowResult struct {
	decision trend.Decision
	balances portfolio.Snapshot
	orders   []execution.Order
	events   []execution.OrderEvent
}

// RecordShadowDecision atomically publishes one decision and its authoritative projections.
func (store *A11ShadowStore) RecordShadowDecision(ctx context.Context, claim A11ShadowClaim,
	input trend.Input, result backtest.EventResult) error {
	decoded, err := decodeA11ShadowResult(input, result)
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyA11ShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return err
	}
	decisionPayload, _ := json.Marshal(decoded.decision)
	explanationPayload, _ := json.Marshal(decoded.decision.Explanation)
	inputPayload, _ := json.Marshal(input)
	outcome := "rejected"
	if decoded.decision.Candidate != nil {
		outcome = "accepted"
	}
	_, err = tx.Exec(ctx, `INSERT INTO decisions(id,run_id,configuration_id,strategy_version_id,outcome,
      reason_code,causation_id,decided_at,ingest_ordinal) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		decoded.decision.ID.Value(), claim.RunID, claim.ConfigurationID, claim.StrategyID, outcome, decoded.decision.ReasonCode,
		input.Evidence.CausationID, input.Now, input.Ordinal)
	if err != nil {
		return err
	}
	if err = insertA11TrendDecision(ctx, tx, input, decoded.decision, explanationPayload); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO decision_inputs(decision_id,input_kind,input_id,version,input_hash)
	      VALUES($1,'trend_input',$2,$3,$4)`, decoded.decision.ID.Value(), input.Evidence.CandleViewID,
		input.Evidence.CandleViewRevision, a11SHA256(inputPayload)); err != nil {
		return err
	}
	if err = storeA11ShadowExecution(ctx, tx, claim, input, decoded.decision, decoded.orders, decoded.events); err != nil {
		return err
	}
	if err = storeA11ShadowProjections(ctx, tx, claim, decoded.balances, input.Now); err != nil {
		return err
	}
	if err = insertA11ShadowOutbox(ctx, tx, input, decoded.decision.ID.Value(), decisionPayload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func decodeA11ShadowResult(input trend.Input, result backtest.EventResult) (a11ShadowResult, error) {
	var decoded a11ShadowResult
	if json.Unmarshal(result.Decision, &decoded.decision) != nil ||
		json.Unmarshal(result.Balances, &decoded.balances) != nil ||
		json.Unmarshal(result.Orders, &decoded.orders) != nil ||
		json.Unmarshal(result.ExecutionEvents, &decoded.events) != nil ||
		decoded.decision.ID.Value() == "" || input.Ordinal != result.Ordinal {
		return decoded, fmt.Errorf("a11_shadow_result_invalid")
	}
	accepted := decoded.decision.Candidate != nil
	if accepted != (len(decoded.orders) > 0 && len(decoded.events) > 0) ||
		(!accepted && (len(decoded.orders) != 0 || len(decoded.events) != 0)) {
		return decoded, fmt.Errorf("a11_shadow_execution_result_invalid")
	}
	return decoded, nil
}

func storeA11ShadowExecution(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim, input trend.Input,
	decision trend.Decision, orders []execution.Order, events []execution.OrderEvent) error {
	if len(orders) == 0 {
		return nil
	}
	const maximumInt64 = ^uint64(0) >> 1
	if claim.ClaimEpoch <= 0 || claim.ConfigurationHash == "" || len(events) > 999 || input.Ordinal == 0 ||
		input.Ordinal > (maximumInt64-1_000_000)/1_000_000 {
		return fmt.Errorf("a11_shadow_execution_identity_invalid")
	}
	exchangeID, err := a11ShadowExchangeID(ctx, tx)
	if err != nil {
		return err
	}
	journal, err := newA11ShadowFillJournal(claim)
	if err != nil {
		return err
	}
	byOrder, plans, err := groupA11ShadowExecution(orders, events)
	if err != nil {
		return err
	}
	for planID, planOrders := range plans {
		if err = insertA11ShadowPlan(ctx, tx, planID, decision.ID.Value(), input.Now, planOrders); err != nil {
			return err
		}
	}
	if err = storeA11ShadowOrders(ctx, tx, claim, input, decision, exchangeID, journal, orders, byOrder); err != nil {
		return err
	}
	return completeA11ShadowPlans(ctx, tx, plans, input.Now)
}

func groupA11ShadowExecution(orders []execution.Order,
	events []execution.OrderEvent) (map[string][]execution.OrderEvent, map[string][]execution.Order, error) {
	byOrder := make(map[string][]execution.OrderEvent, len(orders))
	for _, event := range events {
		byOrder[event.OrderID.Value()] = append(byOrder[event.OrderID.Value()], event)
	}
	known := make(map[string]struct{}, len(orders))
	plans := make(map[string][]execution.Order, len(orders))
	for _, order := range orders {
		known[order.Identity.ID.Value()] = struct{}{}
		plans[order.Identity.PlanID.Value()] = append(plans[order.Identity.PlanID.Value()], order)
	}
	for orderID := range byOrder {
		if _, exists := known[orderID]; !exists {
			return nil, nil, fmt.Errorf("a11_shadow_execution_event_orphaned")
		}
	}
	return byOrder, plans, nil
}

func storeA11ShadowOrders(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim, input trend.Input,
	decision trend.Decision, exchangeID string, journal *simulation.FillJournal, orders []execution.Order,
	byOrder map[string][]execution.OrderEvent) error {
	if decision.Candidate == nil {
		return fmt.Errorf("a11_shadow_candidate_missing")
	}
	baseOrdinal, eventIndex := input.Ordinal*1_000_000, uint64(0)
	var err error
	for _, order := range orders {
		orderEvents := byOrder[order.Identity.ID.Value()]
		if len(orderEvents) == 0 {
			return fmt.Errorf("a11_shadow_order_events_missing")
		}
		if err = insertA11ShadowOrder(ctx, tx, claim, order, decision.Candidate.LimitPrice, orderEvents[0].OccurredAt); err != nil {
			return err
		}
		if err = verifyA11ShadowOrderStream(order, orderEvents); err != nil {
			return err
		}
		for _, event := range orderEvents {
			eventIndex++
			ingestOrdinal := baseOrdinal + eventIndex*1000
			if err = applyA11ShadowOrderEvent(ctx, tx, claim, input, exchangeID, journal,
				order.Identity, event, ingestOrdinal); err != nil {
				return err
			}
		}
		if err = completeA11ShadowPlanLeg(ctx, tx, order); err != nil {
			return err
		}
	}
	return nil
}

func insertA11ShadowPlan(ctx context.Context, tx pgx.Tx, id, decisionID string, now time.Time,
	orders []execution.Order) error {
	if id == "" || len(orders) == 0 {
		return fmt.Errorf("a11_shadow_plan_invalid")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_plans(id,decision_id,state,recovery_state,revision,
      created_at,updated_at,dispatch_policy) VALUES($1,$2,'active',$3,1,$4,$4,'sequential')`,
		id, decisionID, a11SHA256([]byte("shadow-plan:"+id)), now); err != nil {
		return err
	}
	for index, order := range orders {
		instrumentID, err := a11ShadowInstrumentID(ctx, tx, order.Identity.Instrument)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO execution_plan_legs(plan_id,leg_index,instrument_id,side,
        quantity,state,order_id,client_order_id) VALUES($1,$2,$3,$4,$5,'planned',$6,$7)`, id, index,
			instrumentID, string(order.Identity.Side), order.Identity.Quantity.String(),
			order.Identity.ID.Value(), order.Identity.ClientOrderID); err != nil {
			return err
		}
	}
	return nil
}

func insertA11ShadowOrder(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim,
	order execution.Order, limitPrice domain.Price, createdAt time.Time) error {
	instrumentID, err := a11ShadowInstrumentID(ctx, tx, order.Identity.Instrument)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO orders(id,plan_id,account_id,client_order_id,account_epoch,
	  instrument_id,side,quantity,state,revision,created_at,updated_at,requested_limit_price,simulation_latency_ms)
	  VALUES($1,$2,$3,$4,$5,$6,$7,$8,'created',1,$9,$9,$10,0)`, order.Identity.ID.Value(),
		order.Identity.PlanID.Value(), claim.AccountID, order.Identity.ClientOrderID, claim.ClaimEpoch,
		instrumentID, string(order.Identity.Side), order.Identity.Quantity.String(), createdAt, limitPrice.String())
	return err
}

func verifyA11ShadowOrderStream(order execution.Order, events []execution.OrderEvent) error {
	reduced, applied, err := execution.ReduceOrderEvents(order.Identity, events)
	if err != nil || len(applied) != len(events) {
		return fmt.Errorf("a11_shadow_order_stream_invalid")
	}
	want, _ := json.Marshal(order)
	got, _ := json.Marshal(reduced)
	if string(want) != string(got) {
		return fmt.Errorf("a11_shadow_order_projection_mismatch")
	}
	return nil
}

func applyA11ShadowOrderEvent(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim, input trend.Input,
	exchangeID string, journal *simulation.FillJournal, identity execution.OrderIdentity,
	event execution.OrderEvent, ingestOrdinal uint64) error {
	var priorState string
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT state,revision FROM orders WHERE id=$1 FOR UPDATE`,
		identity.ID.Value()).Scan(&priorState, &revision); err != nil {
		return err
	}
	payload, _ := json.Marshal(event)
	newState := strings.ToLower(string(event.State))
	if _, err := tx.Exec(ctx, `INSERT INTO order_events(id,order_id,exchange_event_identity,prior_state,
      new_state,revision,causation_id,occurred_at,ingest_ordinal,event_hash,exchange_status,
      cumulative_quantity,canonical_payload) VALUES($1,$2,$1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		event.ID, identity.ID.Value(), priorState, newState, revision+1, input.Evidence.CausationID,
		event.OccurredAt, int64(ingestOrdinal), a11SHA256(payload), event.ExchangeStatus,
		event.CumulativeQuantity.String(), payload); err != nil {
		return err
	}
	fee, rebate, err := a11ShadowFeeTotals(event.Fees)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE orders SET state=$1,exchange_status=$2,cumulative_quantity=$3,
      cumulative_fee=$4,cumulative_rebate=$5,last_event_ordinal=$6,revision=revision+1,updated_at=$7
      WHERE id=$8 AND revision=$9`, newState, event.ExchangeStatus, event.CumulativeQuantity.String(),
		fee.String(), rebate.String(), int64(ingestOrdinal), event.OccurredAt, identity.ID.Value(), revision)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_order_reduce_failed")
	}
	for index, fill := range event.Fills {
		if index >= 999 {
			return fmt.Errorf("a11_shadow_event_fill_limit_exceeded")
		}
		fillOrdinal := ingestOrdinal + uint64(index) + 1
		if err = insertA11ShadowFill(ctx, tx, claim, input, exchangeID, journal,
			identity, fill, event.OccurredAt, fillOrdinal); err != nil {
			return err
		}
	}
	return nil
}

func insertA11ShadowFill(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim, input trend.Input,
	exchangeID string, journal *simulation.FillJournal, identity execution.OrderIdentity,
	fill execution.FillFact, occurredAt time.Time, ingestOrdinal uint64) error {
	payload, _ := json.Marshal(fill)
	_, err := tx.Exec(ctx, `INSERT INTO fills(id,order_id,exchange_id,exchange_fill_id,quantity,price,
      fee_quantity,fee_asset,occurred_at,rebate_quantity,ingest_ordinal,fill_hash)
      VALUES($1,$2,$3,$1,$4,$5,$6,$7,$8,$9,$10,$11)`, fill.ID.Value(), identity.ID.Value(), exchangeID,
		fill.Quantity.String(), fill.Price.String(), fill.Fee.String(), fill.FeeAsset, occurredAt,
		fill.Rebate.String(), int64(ingestOrdinal), a11SHA256(payload))
	if err != nil {
		return err
	}
	transaction, err := journal.Transaction(identity, fill)
	if err != nil {
		return err
	}
	orderID, fillID := identity.ID.Value(), fill.ID.Value()
	_, err = tx.Exec(ctx, `INSERT INTO journal_transactions(id,transaction_type,run_id,portfolio_id,
      order_id,fill_id,configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, transaction.ID.Value(), transaction.Type,
		claim.RunID, claim.PortfolioID, orderID, fillID, claim.ConfigurationID,
		transaction.CausationID.Value(), input.Evidence.CorrelationID, occurredAt,
		int64(ingestOrdinal))
	if err != nil {
		return err
	}
	for index, line := range transaction.Lines {
		if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(transaction_id,line_number,account_class,
        account_owner,asset_symbol,direction,quantity,lot_reference,rounding_metadata)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, transaction.ID.Value(), index+1,
			string(line.Account.Class), line.Account.Owner, line.Account.Asset, string(line.Direction),
			line.Quantity.String(), nullableA11Text(line.Lot), nullableA11Text(line.Rounding)); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO fill_journal_postings(fill_id,transaction_id,posting_kind)
      VALUES($1,$2,'fill')`, fillID, transaction.ID.Value())
	return err
}

func newA11ShadowFillJournal(claim A11ShadowClaim) (*simulation.FillJournal, error) {
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("a11_shadow_journal_identity_invalid")
	}
	return simulation.NewFillJournal(accounting.NewMemoryJournal(), simulation.JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: claim.AccountID,
		ConfigurationHash: claim.ConfigurationHash})
}

func a11ShadowFeeTotals(fees []execution.FeeFact) (domain.Fee, domain.Fee, error) {
	total, _ := domain.ParseFee("0")
	rebate, _ := domain.ParseFee("0")
	var err error
	for _, fee := range fees {
		if total, err = total.Add(fee.Total); err != nil {
			return domain.Fee{}, domain.Fee{}, err
		}
		if rebate, err = rebate.Add(fee.Rebate); err != nil {
			return domain.Fee{}, domain.Fee{}, err
		}
	}
	return total, rebate, nil
}

func completeA11ShadowPlanLeg(ctx context.Context, tx pgx.Tx, order execution.Order) error {
	tag, err := tx.Exec(ctx, `UPDATE execution_plan_legs SET state=$1 WHERE plan_id=$2 AND order_id=$3`,
		strings.ToLower(string(order.State)), order.Identity.PlanID.Value(), order.Identity.ID.Value())
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_plan_leg_failed")
	}
	return nil
}

func completeA11ShadowPlans(ctx context.Context, tx pgx.Tx, plans map[string][]execution.Order,
	now time.Time) error {
	for id, orders := range plans {
		state := "completed"
		for _, order := range orders {
			if order.State == execution.OrderUnknown || order.State == execution.OrderRecoveryRequired {
				state = "recovery_required"
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE execution_plans SET state=$1,final_disposition=$2,revision=revision+1,
        updated_at=$3 WHERE id=$4 AND state='active'`, state, state, now, id)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("a11_shadow_plan_completion_failed")
		}
	}
	return nil
}
