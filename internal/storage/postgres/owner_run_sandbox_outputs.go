package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

const sandboxRunTimelineSQL = `
SELECT occurred_at,stable_id,payload
FROM (
  SELECT evaluation.occurred_at,evaluation.id AS stable_id,
    jsonb_build_object(
      'event_type','strategy_evaluation',
      'exchange',membership.exchange,
      'state',evaluation.state,
      'reason',evaluation.reason,
      'evidence_hash',evaluation.evidence_hash::text,
      'occurred_at',to_char(evaluation.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )::text AS payload
  FROM sandbox_strategy_session_evaluations evaluation
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=evaluation.strategy_session_id
   AND membership.account_id=evaluation.account_id
  WHERE evaluation.strategy_session_id=$1
  UNION ALL
  SELECT decision.occurred_at,decision.id,
    jsonb_build_object(
      'event_type','strategy_decision',
      'exchange',membership.exchange,
      'instrument',decision.instrument,
      'decision',convert_from(decision.canonical_decision,'UTF8')::jsonb,
      'decision_hash',decision.decision_hash::text,
      'plan_created',decision.plan_id IS NOT NULL,
      'occurred_at',to_char(decision.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )::text
  FROM sandbox_strategy_decisions decision
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=decision.strategy_session_id
   AND membership.account_id=decision.account_id
  WHERE decision.strategy_session_id=$1
  UNION ALL
  SELECT outbound.updated_at,outbound.id,
    jsonb_build_object(
      'event_type','sandbox_order',
      'exchange',account.exchange,
      'order_id',outbound.order_id,
      'instrument',outbound.instrument,
      'side',outbound.side,
      'quantity',outbound.quantity::text,
      'limit_price',outbound.limit_price::text,
      'order_style',outbound.order_style,
      'intent_action',outbound.intent_action,
      'state',outbound.order_state,
      'request_hash',outbound.request_hash::text,
      'occurred_at',to_char(outbound.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )::text
  FROM sandbox_strategy_sessions strategy_session
  JOIN sandbox_runtime_submission_plans plan
    ON plan.sandbox_session_id=strategy_session.sandbox_session_id
   AND plan.intent_kind='STRATEGY'
  JOIN sandbox_runtime_submission_outbox outbound ON outbound.plan_id=plan.id
  JOIN sandbox_runtime_exchange_accounts account ON account.id=outbound.account_id
  WHERE strategy_session.id=$1
  UNION ALL
  SELECT accounting.occurred_at,accounting.id,
    jsonb_build_object(
      'event_type','sandbox_fill',
      'exchange',accounting.exchange,
      'fill_id',accounting.fill_id,
      'order_id',accounting.order_id,
      'instrument',accounting.instrument,
      'side',accounting.side,
      'quantity',accounting.quantity::text,
      'price',accounting.price::text,
      'notional',accounting.notional::text,
      'fee',accounting.fee::text,
      'fee_asset',accounting.fee_asset,
      'journal_state',CASE WHEN accounting.sealed THEN 'sealed' ELSE 'unsealed' END,
      'evidence_hash',accounting.evidence_hash::text,
      'occurred_at',to_char(accounting.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )::text
  FROM sandbox_accounting_transactions accounting
  WHERE accounting.strategy_session_id=$1
  UNION ALL
  SELECT valuation.observed_at,valuation.id,
    jsonb_build_object(
      'event_type','risk_valuation',
      'exchange',membership.exchange,
      'purpose',valuation.purpose,
      'instrument',valuation.instrument,
      'account_equity',valuation.account_equity::text,
      'strategy_total_pnl',valuation.strategy_total_pnl::text,
      'open_orders',valuation.open_orders,
      'accounting_state',valuation.accounting_state,
      'evidence_hash',valuation.evidence_hash::text,
      'occurred_at',to_char(valuation.observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    )::text
  FROM sandbox_strategy_risk_valuations valuation
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=valuation.strategy_session_id
   AND membership.account_id=valuation.account_id
  WHERE valuation.strategy_session_id=$1
) event
ORDER BY occurred_at,stable_id
LIMIT 500`

const sandboxRunDecisionsSQL = `
SELECT row_number() OVER (ORDER BY decision.occurred_at,decision.id),
  jsonb_build_object(
    'exchange',membership.exchange,
    'instrument',decision.instrument,
    'event_ordinal',decision.event_ordinal,
    'decision',convert_from(decision.canonical_decision,'UTF8')::jsonb,
    'decision_hash',decision.decision_hash::text,
    'plan_created',decision.plan_id IS NOT NULL,
    'occurred_at',to_char(decision.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
  )::text
FROM sandbox_strategy_decisions decision
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=decision.strategy_session_id
 AND membership.account_id=decision.account_id
WHERE decision.strategy_session_id=$1
ORDER BY decision.occurred_at,decision.id
LIMIT 500`

const sandboxRunOrdersSQL = `
SELECT row_number() OVER (ORDER BY outbound.approved_at,outbound.id),
  jsonb_build_object(
    'exchange',account.exchange,
    'order_id',outbound.order_id,
    'instrument',outbound.instrument,
    'side',outbound.side,
    'quantity',outbound.quantity::text,
    'limit_price',outbound.limit_price::text,
    'order_style',outbound.order_style,
    'intent_action',outbound.intent_action,
    'state',outbound.order_state,
    'request_hash',outbound.request_hash::text,
    'approved_at',to_char(outbound.approved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    'updated_at',to_char(outbound.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
  )::text
FROM sandbox_strategy_sessions strategy_session
JOIN sandbox_runtime_submission_plans plan
  ON plan.sandbox_session_id=strategy_session.sandbox_session_id
 AND plan.intent_kind='STRATEGY'
JOIN sandbox_runtime_submission_outbox outbound ON outbound.plan_id=plan.id
JOIN sandbox_runtime_exchange_accounts account ON account.id=outbound.account_id
WHERE strategy_session.id=$1
ORDER BY outbound.approved_at,outbound.id
LIMIT 500`

const sandboxRunFillsSQL = `
SELECT row_number() OVER (ORDER BY accounting.occurred_at,accounting.id),
  jsonb_build_object(
    'exchange',accounting.exchange,
    'fill_id',accounting.fill_id,
    'order_id',accounting.order_id,
    'instrument',accounting.instrument,
    'side',accounting.side,
    'quantity',accounting.quantity::text,
    'price',accounting.price::text,
    'notional',accounting.notional::text,
    'fee',accounting.fee::text,
    'rebate',accounting.rebate::text,
    'fee_asset',accounting.fee_asset,
    'journal_state',CASE WHEN accounting.sealed THEN 'sealed' ELSE 'unsealed' END,
    'evidence_hash',accounting.evidence_hash::text,
    'occurred_at',to_char(accounting.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
  )::text
FROM sandbox_accounting_transactions accounting
WHERE accounting.strategy_session_id=$1
ORDER BY accounting.occurred_at,accounting.id
LIMIT 500`

func (store *OwnerConsoleStore) sandboxRunOutputs(
	ctx context.Context,
	id, requested string,
) (generated.RunOutputPage, error) {
	_, exposed, ok := ownerRunOutputKind(requested)
	if !ok {
		return generated.RunOutputPage{}, console.ErrInvalidRequest
	}
	switch requested {
	case "event":
		return store.sandboxTimelineOutputs(ctx, id, exposed)
	case "decision":
		return store.sandboxOrdinalOutputs(ctx, sandboxRunDecisionsSQL, id, exposed)
	case "order":
		return store.sandboxOrdinalOutputs(ctx, sandboxRunOrdersSQL, id, exposed)
	case "projection":
		return store.sandboxOrdinalOutputs(ctx, sandboxRunFillsSQL, id, exposed)
	default:
		return generated.RunOutputPage{}, console.ErrInvalidRequest
	}
}

func (store *OwnerConsoleStore) sandboxTimelineOutputs(
	ctx context.Context,
	id string,
	kind generated.RunOutputKind,
) (generated.RunOutputPage, error) {
	rows, err := store.pool.Query(ctx, sandboxRunTimelineSQL, id)
	if err != nil {
		return generated.RunOutputPage{}, err
	}
	defer rows.Close()
	page := generated.RunOutputPage{Items: make([]generated.RunOutput, 0)}
	var ordinal int64
	for rows.Next() {
		var stableID, payload string
		var occurredAt time.Time
		if err = rows.Scan(&occurredAt, &stableID, &payload); err != nil {
			return generated.RunOutputPage{}, err
		}
		ordinal++
		output, outputErr := projectedRunOutput(ordinal, kind, payload)
		if outputErr != nil || stableID == "" {
			return generated.RunOutputPage{}, fmt.Errorf("sandbox_run_timeline_invalid")
		}
		page.Items = append(page.Items, output)
	}
	return page, rows.Err()
}

func (store *OwnerConsoleStore) sandboxOrdinalOutputs(
	ctx context.Context,
	query, id string,
	kind generated.RunOutputKind,
) (generated.RunOutputPage, error) {
	rows, err := store.pool.Query(ctx, query, id)
	if err != nil {
		return generated.RunOutputPage{}, err
	}
	defer rows.Close()
	page := generated.RunOutputPage{Items: make([]generated.RunOutput, 0)}
	for rows.Next() {
		var ordinal int64
		var payload string
		if err = rows.Scan(&ordinal, &payload); err != nil {
			return generated.RunOutputPage{}, err
		}
		output, outputErr := projectedRunOutput(ordinal, kind, payload)
		if outputErr != nil {
			return generated.RunOutputPage{}, outputErr
		}
		page.Items = append(page.Items, output)
	}
	return page, rows.Err()
}

func projectedRunOutput(
	ordinal int64,
	kind generated.RunOutputKind,
	payload string,
) (generated.RunOutput, error) {
	if ordinal <= 0 || !json.Valid([]byte(payload)) {
		return generated.RunOutput{}, fmt.Errorf("run_output_projection_invalid")
	}
	digest := sha256.Sum256([]byte(payload))
	return generated.RunOutput{
		Ordinal:          strconv.FormatInt(ordinal, 10),
		Kind:             kind,
		ContentHash:      hex.EncodeToString(digest[:]),
		CanonicalPayload: payload,
	}, nil
}
