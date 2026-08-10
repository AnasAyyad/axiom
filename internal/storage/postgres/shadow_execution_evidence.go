package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/simulation"

	"github.com/jackc/pgx/v5"
)

func storePublicShadowOrders(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, exchangeID string, journal *simulation.FillJournal, orders []execution.Order,
	byOrder map[string][]execution.OrderEvent) error {
	if !evidence.accepted {
		return fmt.Errorf("owner_console_shadow_candidate_missing")
	}
	baseOrdinal, eventIndex := evidence.ordinal*1_000_000, uint64(0)
	var err error
	for _, order := range orders {
		orderEvents := byOrder[order.Identity.ID.Value()]
		if len(orderEvents) == 0 {
			return fmt.Errorf("owner_console_shadow_order_events_missing")
		}
		if err = insertPublicShadowOrder(ctx, tx, claim, order, evidence.limitPrice, orderEvents[0].OccurredAt); err != nil {
			return err
		}
		if err = verifyPublicShadowOrderStream(order, orderEvents); err != nil {
			return err
		}
		for _, event := range orderEvents {
			eventIndex++
			ingestOrdinal := baseOrdinal + eventIndex*1000
			if err = applyPublicShadowOrderEvent(ctx, tx, claim, evidence, exchangeID, journal,
				order.Identity, event, ingestOrdinal); err != nil {
				return err
			}
		}
		if err = completePublicShadowPlanLeg(ctx, tx, order); err != nil {
			return err
		}
	}
	return nil
}

func insertPublicShadowPlan(ctx context.Context, tx pgx.Tx, id, decisionID string, now time.Time,
	orders []execution.Order) error {
	if id == "" || len(orders) == 0 {
		return fmt.Errorf("owner_console_shadow_plan_invalid")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_plans(id,decision_id,state,recovery_state,revision,
      created_at,updated_at,dispatch_policy) VALUES($1,$2,'active',$3,1,$4,$4,'sequential')`,
		id, decisionID, ownerConsoleSHA256([]byte("shadow-plan:"+id)), now); err != nil {
		return err
	}
	for index, order := range orders {
		instrumentID, err := publicShadowInstrumentID(ctx, tx, order.Identity.Instrument)
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

func insertPublicShadowOrder(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	order execution.Order, limitPrice domain.Price, createdAt time.Time) error {
	instrumentID, err := publicShadowInstrumentID(ctx, tx, order.Identity.Instrument)
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

func verifyPublicShadowOrderStream(order execution.Order, events []execution.OrderEvent) error {
	reduced, applied, err := execution.ReduceOrderEvents(order.Identity, events)
	if err != nil || len(applied) != len(events) {
		return fmt.Errorf("owner_console_shadow_order_stream_invalid")
	}
	want, _ := json.Marshal(order)
	got, _ := json.Marshal(reduced)
	if string(want) != string(got) {
		return fmt.Errorf("owner_console_shadow_order_projection_mismatch")
	}
	return nil
}

func applyPublicShadowOrderEvent(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim, evidence publicShadowDecisionEvidence,
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
		event.ID, identity.ID.Value(), priorState, newState, revision+1, evidence.causationID,
		event.OccurredAt, int64(ingestOrdinal), ownerConsoleSHA256(payload), event.ExchangeStatus,
		event.CumulativeQuantity.String(), payload); err != nil {
		return err
	}
	fee, rebate, err := publicShadowFeeTotals(event.Fees)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE orders SET state=$1,exchange_status=$2,cumulative_quantity=$3,
      cumulative_fee=$4,cumulative_rebate=$5,last_event_ordinal=$6,revision=revision+1,updated_at=$7
      WHERE id=$8 AND revision=$9`, newState, event.ExchangeStatus, event.CumulativeQuantity.String(),
		fee.String(), rebate.String(), int64(ingestOrdinal), event.OccurredAt, identity.ID.Value(), revision)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_order_reduce_failed")
	}
	for index, fill := range event.Fills {
		if index >= 999 {
			return fmt.Errorf("owner_console_shadow_event_fill_limit_exceeded")
		}
		fillOrdinal := ingestOrdinal + uint64(index) + 1
		if err = insertPublicShadowFill(ctx, tx, claim, evidence, exchangeID, journal,
			identity, fill, event.OccurredAt, fillOrdinal); err != nil {
			return err
		}
	}
	return nil
}

func insertPublicShadowFill(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim, evidence publicShadowDecisionEvidence,
	exchangeID string, journal *simulation.FillJournal, identity execution.OrderIdentity,
	fill execution.FillFact, occurredAt time.Time, ingestOrdinal uint64) error {
	payload, _ := json.Marshal(fill)
	_, err := tx.Exec(ctx, `INSERT INTO fills(id,order_id,exchange_id,exchange_fill_id,quantity,price,
      fee_quantity,fee_asset,occurred_at,rebate_quantity,ingest_ordinal,fill_hash)
      VALUES($1,$2,$3,$1,$4,$5,$6,$7,$8,$9,$10,$11)`, fill.ID.Value(), identity.ID.Value(), exchangeID,
		fill.Quantity.String(), fill.Price.String(), fill.Fee.String(), fill.FeeAsset, occurredAt,
		fill.Rebate.String(), int64(ingestOrdinal), ownerConsoleSHA256(payload))
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
		transaction.CausationID.Value(), evidence.correlationID, occurredAt,
		int64(ingestOrdinal))
	if err != nil {
		return err
	}
	for index, line := range transaction.Lines {
		if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(transaction_id,line_number,account_class,
        account_owner,asset_symbol,direction,quantity,lot_reference,rounding_metadata)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, transaction.ID.Value(), index+1,
			string(line.Account.Class), line.Account.Owner, line.Account.Asset, string(line.Direction),
			line.Quantity.String(), nullableOwnerConsoleText(line.Lot), nullableOwnerConsoleText(line.Rounding)); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO fill_journal_postings(fill_id,transaction_id,posting_kind)
      VALUES($1,$2,'fill')`, fillID, transaction.ID.Value())
	return err
}

func newPublicShadowFillJournal(claim PublicShadowClaim) (*simulation.FillJournal, error) {
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("owner_console_shadow_journal_identity_invalid")
	}
	return simulation.NewFillJournal(accounting.NewMemoryJournal(), simulation.JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: claim.AccountID,
		ConfigurationHash: claim.ConfigurationHash})
}

func publicShadowFeeTotals(fees []execution.FeeFact) (domain.Fee, domain.Fee, error) {
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

func completePublicShadowPlanLeg(ctx context.Context, tx pgx.Tx, order execution.Order) error {
	tag, err := tx.Exec(ctx, `UPDATE execution_plan_legs SET state=$1 WHERE plan_id=$2 AND order_id=$3`,
		strings.ToLower(string(order.State)), order.Identity.PlanID.Value(), order.Identity.ID.Value())
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_plan_leg_failed")
	}
	return nil
}

func completePublicShadowPlans(ctx context.Context, tx pgx.Tx, plans map[string][]execution.Order,
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
			return fmt.Errorf("owner_console_shadow_plan_completion_failed")
		}
	}
	return nil
}
