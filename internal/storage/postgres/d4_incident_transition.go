package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

func applyD4IncidentTransition(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (map[string]any, error) {
	current, revision, err := d4IncidentState(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	valid := current == "open" && command.State == "acknowledged" ||
		(current == "open" || current == "acknowledged") && command.State == "resolved"
	if revision != command.ExpectedRevision || !valid {
		return nil, console.ErrConflict
	}
	if command.State == "resolved" {
		if err = d4RecordResolution(ctx, tx, principal, command, now); err != nil {
			return nil, err
		}
	}
	next := revision + 1
	_, err = tx.Exec(ctx, `UPDATE incidents SET state=$2,revision=$3,updated_at=$4,
acknowledged_at=CASE WHEN $2='acknowledged' THEN $4 ELSE acknowledged_at END,
resolved_at=CASE WHEN $2='resolved' THEN $4 ELSE resolved_at END WHERE id=$1`,
		command.TargetID, command.State, next, now)
	if err != nil {
		return nil, err
	}
	if err = appendD4IncidentEvent(ctx, tx, command.TargetID, next, command.State,
		principal.UserID, command.Reason, "", "", d4ResolutionDetail(command),
		command.TargetID, now); err != nil {
		return nil, err
	}
	return map[string]any{"incident_id": command.TargetID, "state": command.State, "revision": next}, nil
}

func d4RecordResolution(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) error {
	evidence, ok := command.Payload["resolution_evidence"].(string)
	if !ok || len(evidence) < 3 {
		return console.ErrPrecondition
	}
	_, err := tx.Exec(ctx, `INSERT INTO v1d_incident_resolution_evidence(
incident_id,evidence,evidence_hash,recorded_by,recorded_at
) VALUES($1,$2,$3,$4,$5)`, command.TargetID, evidence, a11Hash([]byte(evidence)), principal.UserID, now)
	return a11ConstraintError(err)
}

func d4ResolutionDetail(command console.D1Command) string {
	if value, ok := command.Payload["resolution_evidence"].(string); ok {
		return value
	}
	return command.Reason
}

func d4IncidentState(ctx context.Context, tx pgx.Tx, id string) (string, int64, error) {
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT state,revision FROM incidents WHERE id=$1 FOR UPDATE`, id).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, console.ErrNotFound
	}
	return state, revision, err
}

func appendD4IncidentEvent(
	ctx context.Context, tx pgx.Tx, incidentID string, revision int64,
	eventType, actor, reason, referenceType, referenceID, detail, correlation string,
	now time.Time,
) error {
	var previous *string
	err := tx.QueryRow(ctx, `SELECT event_hash::text FROM v1d_incident_events
WHERE incident_id=$1 ORDER BY incident_revision DESC LIMIT 1`, incidentID).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	prior := ""
	if previous != nil {
		prior = *previous
	}
	hash := a11Hash([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		prior, incidentID, revision, eventType, actor, reason, referenceType, referenceID, detail)))
	id, _ := a11Identifier("incident-event")
	_, err = tx.Exec(ctx, `INSERT INTO v1d_incident_events(
id,incident_id,incident_revision,event_type,actor,reason,reference_type,
reference_id,detail,previous_hash,event_hash,correlation_id,occurred_at
) VALUES($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),nullif($9,''),
nullif($10,''),$11,$12,$13)`, id, incidentID, revision, eventType, actor, reason,
		referenceType, referenceID, detail, prior, hash, correlation, now)
	return err
}

func d4ActiveUser(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')`, id).Scan(&exists)
	return exists, err
}

func d4PayloadRevision(value any) (int64, error) {
	text, ok := value.(string)
	if !ok {
		return 0, console.ErrInvalidRequest
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, console.ErrInvalidRequest
	}
	return parsed, nil
}

func d4IncidentSeverity(value string) bool {
	return value == "warning" || value == "error" || value == "critical"
}
