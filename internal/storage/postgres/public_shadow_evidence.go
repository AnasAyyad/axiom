package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5"
)

// PublicShadowMetadataEvidence is the durable identity assigned to one public filter set.
type PublicShadowMetadataEvidence struct {
	ID              string
	Metadata        domain.InstrumentMetadata
	MaximumQuantity domain.Quantity
}

type publicShadowResult struct {
	balances portfolio.Snapshot
	orders   []execution.Order
	events   []execution.OrderEvent
}

type publicShadowDecisionEvidence struct {
	decisionID      string
	reasonCode      string
	inputKind       string
	inputID         string
	inputRevision   uint64
	correlationID   string
	causationID     string
	now             time.Time
	ordinal         uint64
	accepted        bool
	limitPrice      domain.Price
	inputPayload    []byte
	decisionPayload []byte
	outboxTopic     string
	outboxStream    string
	outboxEntity    string
}

// RecordShadowDecision atomically publishes one decision and its authoritative projections.
func (store *PublicShadowStore) RecordShadowDecision(ctx context.Context, claim PublicShadowClaim,
	input trend.Input, result backtest.EventResult) error {
	var decision trend.Decision
	if json.Unmarshal(result.Decision, &decision) != nil || decision.ID.Value() == "" {
		return fmt.Errorf("owner_console_shadow_result_invalid")
	}
	inputPayload, _ := json.Marshal(input)
	decisionPayload, _ := json.Marshal(decision)
	evidence := publicShadowDecisionEvidence{decisionID: decision.ID.Value(), reasonCode: decision.ReasonCode,
		inputKind: "trend_input", inputID: input.Evidence.CandleViewID,
		inputRevision: input.Evidence.CandleViewRevision, correlationID: input.Evidence.CorrelationID,
		causationID: input.Evidence.CausationID, now: input.Now, ordinal: input.Ordinal,
		accepted: decision.Candidate != nil, inputPayload: inputPayload, decisionPayload: decisionPayload,
		outboxTopic: "trend.decision", outboxStream: "trend", outboxEntity: "trend_decision"}
	if decision.Candidate != nil {
		evidence.limitPrice = decision.Candidate.LimitPrice
	}
	explanationPayload, _ := json.Marshal(decision.Explanation)
	return store.recordShadowStrategyDecision(ctx, claim, evidence, result, func(tx pgx.Tx) error {
		return insertOwnerConsoleTrendDecision(ctx, tx, input, decision, explanationPayload)
	})
}

// RecordMeanReversionShadowDecision preserves the exact selected-venue input
// without pretending that it is a qualified two-exchange coherent view.
func (store *PublicShadowStore) RecordMeanReversionShadowDecision(ctx context.Context, claim PublicShadowClaim,
	input meanreversion.Input, result backtest.EventResult) error {
	var decision meanreversion.Decision
	if json.Unmarshal(result.Decision, &decision) != nil || decision.ID.Value() == "" {
		return fmt.Errorf("owner_console_shadow_result_invalid")
	}
	inputPayload, _ := json.Marshal(input)
	decisionPayload, _ := json.Marshal(decision)
	evidence := publicShadowDecisionEvidence{decisionID: decision.ID.Value(), reasonCode: decision.ReasonCode,
		inputKind: "mean_reversion_input", inputID: input.Evidence.CoherentViewID,
		inputRevision: input.Evidence.PrimaryCandleViewRevision, correlationID: input.Evidence.CorrelationID,
		causationID: input.Evidence.CausationID, now: input.Now, ordinal: input.Ordinal,
		accepted: decision.Candidate != nil, inputPayload: inputPayload, decisionPayload: decisionPayload,
		outboxTopic: "mean_reversion.decision", outboxStream: "strategy",
		outboxEntity: "mean_reversion_decision"}
	if decision.Candidate != nil {
		evidence.limitPrice = decision.Candidate.LimitPrice
	}
	return store.recordShadowStrategyDecision(ctx, claim, evidence, result, nil)
}

func (store *PublicShadowStore) recordShadowStrategyDecision(ctx context.Context, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, result backtest.EventResult, insertSpecific func(pgx.Tx) error,
) error {
	decoded, err := decodePublicShadowResult(evidence, result)
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return err
	}
	outcome := "rejected"
	if evidence.accepted {
		outcome = "accepted"
	}
	_, err = tx.Exec(ctx, `INSERT INTO decisions(id,run_id,configuration_id,strategy_version_id,outcome,
      reason_code,causation_id,decided_at,ingest_ordinal) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		evidence.decisionID, claim.RunID, claim.ConfigurationID, claim.StrategyID, outcome, evidence.reasonCode,
		evidence.causationID, evidence.now, evidence.ordinal)
	if err != nil {
		return err
	}
	if insertSpecific != nil {
		if err = insertSpecific(tx); err != nil {
			return err
		}
	}
	if err = insertPublicShadowStrategyEvidence(ctx, tx, claim, evidence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO decision_inputs(decision_id,input_kind,input_id,version,input_hash)
	      VALUES($1,$2,$3,$4,$5)`, evidence.decisionID, evidence.inputKind, evidence.inputID,
		evidence.inputRevision, ownerConsoleSHA256(evidence.inputPayload)); err != nil {
		return err
	}
	if err = storePublicShadowExecution(ctx, tx, claim, evidence, decoded.orders, decoded.events); err != nil {
		return err
	}
	if err = storePublicShadowProjections(ctx, tx, claim, decoded.balances, evidence.now); err != nil {
		return err
	}
	if err = insertPublicShadowOutbox(ctx, tx, evidence); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func decodePublicShadowResult(evidence publicShadowDecisionEvidence, result backtest.EventResult) (publicShadowResult, error) {
	var decoded publicShadowResult
	if json.Unmarshal(result.Balances, &decoded.balances) != nil ||
		json.Unmarshal(result.Orders, &decoded.orders) != nil ||
		json.Unmarshal(result.ExecutionEvents, &decoded.events) != nil ||
		evidence.decisionID == "" || evidence.ordinal != result.Ordinal {
		return decoded, fmt.Errorf("owner_console_shadow_result_invalid")
	}
	if evidence.accepted != (len(decoded.orders) > 0 && len(decoded.events) > 0) ||
		(!evidence.accepted && (len(decoded.orders) != 0 || len(decoded.events) != 0)) {
		return decoded, fmt.Errorf("owner_console_shadow_execution_result_invalid")
	}
	return decoded, nil
}

func storePublicShadowExecution(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, orders []execution.Order, events []execution.OrderEvent) error {
	if len(orders) == 0 {
		return nil
	}
	const maximumInt64 = ^uint64(0) >> 1
	if claim.ClaimEpoch <= 0 || claim.ConfigurationHash == "" || len(events) > 999 || evidence.ordinal == 0 ||
		evidence.ordinal > (maximumInt64-1_000_000)/1_000_000 {
		return fmt.Errorf("owner_console_shadow_execution_identity_invalid")
	}
	exchangeID, err := publicShadowExchangeID(ctx, tx, claim.ExchangeID)
	if err != nil {
		return err
	}
	journal, err := newPublicShadowFillJournal(claim)
	if err != nil {
		return err
	}
	byOrder, plans, err := groupPublicShadowExecution(orders, events)
	if err != nil {
		return err
	}
	for planID, planOrders := range plans {
		if err = insertPublicShadowPlan(ctx, tx, planID, evidence.decisionID, evidence.now, planOrders); err != nil {
			return err
		}
	}
	if err = storePublicShadowOrders(ctx, tx, claim, evidence, exchangeID, journal, orders, byOrder); err != nil {
		return err
	}
	return completePublicShadowPlans(ctx, tx, plans, evidence.now)
}

func groupPublicShadowExecution(orders []execution.Order,
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
			return nil, nil, fmt.Errorf("owner_console_shadow_execution_event_orphaned")
		}
	}
	return byOrder, plans, nil
}
