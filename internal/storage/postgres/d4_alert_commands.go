package postgres

import (
	"context"
	"errors"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

func applyD4AlertEscalation(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (map[string]any, error) {
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT state,revision FROM alerts WHERE id=$1 FOR UPDATE`,
		command.TargetID).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if state == "resolved" || revision != command.ExpectedRevision {
		return nil, console.ErrConflict
	}
	next := revision + 1
	id, _ := a11Identifier("alert-escalation")
	if _, err = tx.Exec(ctx, `INSERT INTO v1d_alert_escalations(
id,alert_id,revision,actor_user_id,reason,escalated_at
) VALUES($1,$2,$3,$4,$5,$6)`, id, command.TargetID, next, principal.UserID,
		command.Reason, now); err != nil {
		return nil, a11ConstraintError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts SET revision=$2,last_seen_at=$3 WHERE id=$1`,
		command.TargetID, next, now); err != nil {
		return nil, err
	}
	return map[string]any{"alert_id": command.TargetID, "state": state,
		"escalation_id": id, "revision": next}, nil
}

func applyD4AlertTest(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, commandID string, now time.Time,
) (map[string]any, error) {
	sink, enabled, revision, err := d4AlertRoute(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if !enabled || revision != command.ExpectedRevision {
		return nil, console.ErrPrecondition
	}
	testID, _ := a11Identifier("alert-route-test")
	if sink == "in_app" {
		err = recordD4InAppRouteTest(ctx, tx, testID, commandID, principal, command, now)
		return map[string]any{"route_test_id": testID, "state": "delivered"}, err
	}
	if sink != "webhook" {
		return nil, console.ErrPrecondition
	}
	return recordD4WebhookRouteTest(ctx, tx, testID, commandID, principal, command, now)
}

func d4AlertRoute(ctx context.Context, tx pgx.Tx, id string) (string, bool, int64, error) {
	var sink string
	var enabled bool
	var revision int64
	err := tx.QueryRow(ctx, `SELECT sink_name,enabled,revision FROM v1d_alert_routes
WHERE id=$1 FOR UPDATE`, id).Scan(&sink, &enabled, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		err = console.ErrNotFound
	}
	return sink, enabled, revision, err
}

func recordD4InAppRouteTest(
	ctx context.Context, tx pgx.Tx, testID, commandID string,
	principal authentication.Principal, command console.D1Command, now time.Time,
) error {
	_, err := tx.Exec(ctx, `INSERT INTO v1d_alert_route_tests(
id,command_id,route_id,state,requested_by,reason,requested_at,completed_at
) VALUES($1,$2,$3,'delivered',$4,$5,$6,$6)`, testID, commandID,
		command.TargetID, principal.UserID, command.Reason, now)
	return err
}

func recordD4WebhookRouteTest(
	ctx context.Context, tx pgx.Tx, testID, commandID string,
	principal authentication.Principal, command console.D1Command, now time.Time,
) (map[string]any, error) {
	alertID := "alert-test-" + commandID
	deliveryID := "alert-delivery-" + commandID
	dedup := a11Hash([]byte(commandID + "\x00webhook"))
	if _, err := tx.Exec(ctx, `INSERT INTO alerts(
id,incident_id,alert_type,state,created_at,acknowledged_at,resolved_at,severity,
reason_code,deduplication_key,correlation_id,last_seen_at,occurrences,revision
) VALUES($1,NULL,'route-test','resolved',$2,NULL,$2,'info','alert_delivery',$3,$4,$2,1,1)`,
		alertID, now, dedup, commandID); err != nil {
		return nil, a11ConstraintError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO alert_deliveries(
id,alert_id,sink_name,state,attempts,last_reason_code,next_attempt_at,created_at,revision
) VALUES($1,$2,'webhook','pending',0,NULL,$3,$3,1)`, deliveryID, alertID, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v1d_alert_route_tests(
id,command_id,route_id,alert_id,state,requested_by,reason,requested_at
) VALUES($1,$2,$3,$4,'pending',$5,$6,$7)`, testID, commandID, command.TargetID,
		alertID, principal.UserID, command.Reason, now); err != nil {
		return nil, err
	}
	return map[string]any{"route_test_id": testID, "alert_id": alertID,
		"delivery_id": deliveryID, "state": "pending"}, nil
}
