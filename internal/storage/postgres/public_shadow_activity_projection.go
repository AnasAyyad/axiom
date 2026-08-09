package postgres

import (
	"context"
	"errors"
	"time"

	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

func (store *OwnerConsoleStore) populatePublicShadowActivity(
	ctx context.Context,
	item *generated.ShadowSessionResource,
) error {
	var revision int64
	var state string
	var next *time.Time
	err := store.pool.QueryRow(ctx, `SELECT revision,activity_state,reason_code,summary,
	  next_evaluation_at,trigger_condition
	  FROM shadow_session_activity_observations
	  WHERE session_id=$1 ORDER BY revision DESC LIMIT 1`, item.Id).Scan(
		&revision, &state, &item.WaitingReasonCode, &item.WaitingReason, &next, &item.TriggerCondition,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		setPublicShadowActivityFallback(item)
		return nil
	}
	if err != nil {
		return err
	}
	item.ActivityState = generated.ShadowSessionResourceActivityState(state)
	if next != nil {
		value := generated.Timestamp(next.UTC())
		item.NextEvaluationAt = &value
	}
	inputs, err := store.publicShadowInputHealth(ctx, item.Id, revision)
	if err != nil {
		return err
	}
	item.InputHealth = inputs
	return nil
}

func (store *OwnerConsoleStore) publicShadowInputHealth(
	ctx context.Context,
	sessionID string,
	revision int64,
) ([]generated.ShadowInputHealth, error) {
	rows, err := store.pool.Query(ctx, `SELECT input.exchange_id,
	  instrument.base_asset || '/' || instrument.quote_asset,input.state,input.reason,input.fresh,
	  input.book_version::text,(input.age_nanoseconds/1000000)::text,input.observed_at
	  FROM shadow_session_input_health_observations input
	  JOIN instruments instrument ON instrument.id=input.instrument_id
	  WHERE input.session_id=$1 AND input.activity_revision=$2
	  ORDER BY input.exchange_id,instrument.base_asset,instrument.quote_asset`, sessionID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.ShadowInputHealth, 0, 3)
	for rows.Next() {
		var item generated.ShadowInputHealth
		var exchange, state string
		if err = rows.Scan(&exchange, &item.Instrument, &state, &item.Reason, &item.Fresh,
			&item.BookVersion, &item.AgeMilliseconds, &item.ObservedAt); err != nil {
			return nil, err
		}
		item.Exchange = generated.ShadowInputHealthExchange(exchange)
		item.State = generated.ShadowInputHealthState(state)
		item.ObservedAt = generated.Timestamp(time.Time(item.ObservedAt).UTC())
		items = append(items, item)
	}
	return items, rows.Err()
}

func setPublicShadowActivityFallback(item *generated.ShadowSessionResource) {
	item.InputHealth = []generated.ShadowInputHealth{}
	item.TriggerCondition = "The production-public shadow worker must prepare the selected feed and strategy inputs."
	switch item.State {
	case generated.ShadowSessionResourceStateQUEUED:
		item.ActivityState = generated.ShadowSessionResourceActivityStatePreparing
		item.WaitingReasonCode = "shadow_worker_pending"
		item.WaitingReason = "Waiting for the production-public shadow worker to claim and prepare this simulation."
	case generated.ShadowSessionResourceStatePAUSED:
		item.ActivityState = generated.ShadowSessionResourceActivityStatePaused
		item.WaitingReasonCode = "safety_controls_preparing"
		item.WaitingReason = "Virtual entries are paused while durable safety controls prepare or hold the session."
	case generated.ShadowSessionResourceStateRUNNING:
		item.ActivityState = generated.ShadowSessionResourceActivityStateWaiting
		item.WaitingReasonCode = "activity_observation_pending"
		item.WaitingReason = "Waiting for the first durable strategy activity and public-input health observation."
	case generated.ShadowSessionResourceStateFAILED:
		item.ActivityState = generated.ShadowSessionResourceActivityStateBlocked
		item.WaitingReasonCode = "shadow_runtime_failed"
		item.WaitingReason = "The shadow runtime stopped after a fail-closed error; review the bounded failure code."
	default:
		item.ActivityState = generated.ShadowSessionResourceActivityStateStopped
		item.WaitingReasonCode = "shadow_session_stopped"
		item.WaitingReason = "This shadow session is stopping or stopped; no new strategy evaluation will begin."
	}
}
