package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

func applyD4IncidentCreate(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (map[string]any, error) {
	severity, severityOK := command.Payload["severity"].(string)
	reasonCode, reasonOK := command.Payload["reason_code"].(string)
	owner, ownerOK := command.Payload["owner_user_id"].(string)
	if command.Action != "create" || command.ExpectedRevision != 1 || !severityOK ||
		!reasonOK || !ownerOK || !d4IncidentSeverity(severity) || reasonCode == "" {
		return nil, console.ErrInvalidRequest
	}
	if exists, err := d4ActiveUser(ctx, tx, owner); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, console.ErrPrecondition
	}
	_, err := tx.Exec(ctx, `INSERT INTO incidents(
id,severity,state,reason_code,owner_user_id,revision,opened_at,updated_at
) VALUES($1,$2,'open',$3,$4,1,$5,$5)`, command.TargetID, severity, reasonCode, owner, now)
	if err != nil {
		return nil, a11ConstraintError(err)
	}
	if err = appendD4IncidentEvent(ctx, tx, command.TargetID, 1, "opened", principal.UserID,
		command.Reason, "", "", reasonCode, command.TargetID, now); err != nil {
		return nil, err
	}
	return map[string]any{"incident_id": command.TargetID, "state": "open", "revision": 1}, nil
}

func applyD4IncidentUpdate(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (map[string]any, error) {
	state, revision, err := d4IncidentState(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision || state == "resolved" {
		return nil, console.ErrConflict
	}
	eventType, referenceType, referenceID, detail, err := applyD4IncidentAction(
		ctx, tx, principal, command, now)
	if err != nil {
		return nil, err
	}
	next := revision + 1
	if _, err = tx.Exec(ctx, `UPDATE incidents SET revision=$2,updated_at=$3 WHERE id=$1`,
		command.TargetID, next, now); err != nil {
		return nil, err
	}
	if err = appendD4IncidentEvent(ctx, tx, command.TargetID, next, eventType,
		principal.UserID, command.Reason, referenceType, referenceID, detail,
		command.TargetID, now); err != nil {
		return nil, err
	}
	return map[string]any{"incident_id": command.TargetID, "state": state,
		"revision": next, "event_type": eventType}, nil
}

func applyD4IncidentAction(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (string, string, string, string, error) {
	switch command.Action {
	case "assign_owner":
		return d4AssignIncidentOwner(ctx, tx, command)
	case "add_remediation":
		note, ok := command.Payload["note"].(string)
		if !ok || len(note) < 3 {
			return "", "", "", "", console.ErrInvalidRequest
		}
		return "remediation_added", "", "", note, nil
	case "link_alert":
		return d4LinkIncidentAlert(ctx, tx, principal, command, now)
	case "link_activity":
		return d4LinkIncidentActivity(ctx, tx, principal, command, now)
	case "link_replay":
		return d4LinkIncidentReplay(ctx, tx, principal, command, now)
	default:
		return "", "", "", "", console.ErrInvalidRequest
	}
}

func d4AssignIncidentOwner(ctx context.Context, tx pgx.Tx, command console.D1Command) (string, string, string, string, error) {
	owner, ok := command.Payload["owner_user_id"].(string)
	if !ok {
		return "", "", "", "", console.ErrInvalidRequest
	}
	exists, err := d4ActiveUser(ctx, tx, owner)
	if err != nil || !exists {
		if err == nil {
			err = console.ErrPrecondition
		}
		return "", "", "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET owner_user_id=$2 WHERE id=$1`, command.TargetID, owner); err != nil {
		return "", "", "", "", err
	}
	return "owner_assigned", "user", owner, "ownership assigned", nil
}

func d4LinkIncidentAlert(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (string, string, string, string, error) {
	reference, ok := command.Payload["reference_id"].(string)
	if !ok {
		return "", "", "", "", console.ErrInvalidRequest
	}
	result, err := tx.Exec(ctx, `INSERT INTO v1d_incident_alert_links(
incident_id,alert_id,linked_by,linked_at
) SELECT $1,id,$3,$4 FROM alerts WHERE id=$2 ON CONFLICT DO NOTHING`,
		command.TargetID, reference, principal.UserID, now)
	if err != nil || result.RowsAffected() != 1 {
		if err == nil {
			err = console.ErrPrecondition
		}
		return "", "", "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts SET incident_id=$2 WHERE id=$1`, reference, command.TargetID); err != nil {
		return "", "", "", "", err
	}
	return "alert_linked", "alert", reference, "alert linked", nil
}

func d4LinkIncidentActivity(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (string, string, string, string, error) {
	reference, ok := command.Payload["reference_id"].(string)
	if !ok {
		return "", "", "", "", console.ErrInvalidRequest
	}
	result, err := tx.Exec(ctx, `INSERT INTO v1d_incident_activity_links(
incident_id,activity_id,linked_by,linked_at
) SELECT $1,id,$3,$4 FROM v1d_activity_projection WHERE id=$2 ON CONFLICT DO NOTHING`,
		command.TargetID, reference, principal.UserID, now)
	if err != nil || result.RowsAffected() != 1 {
		if err == nil {
			err = console.ErrPrecondition
		}
		return "", "", "", "", err
	}
	return "activity_linked", "activity", reference, "activity linked", nil
}

func d4LinkIncidentReplay(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.D1Command, now time.Time,
) (string, string, string, string, error) {
	dataset, datasetOK := command.Payload["dataset_id"].(string)
	first, firstErr := d4PayloadRevision(command.Payload["first_ordinal"])
	last, lastErr := d4PayloadRevision(command.Payload["last_ordinal"])
	source, sourceOK := command.Payload["source_identity"].(string)
	if !datasetOK || !sourceOK || firstErr != nil || lastErr != nil || last < first {
		return "", "", "", "", console.ErrInvalidRequest
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dataset_manifests manifest
JOIN dataset_segments member ON member.dataset_id=manifest.id
JOIN market_data_segments segment ON segment.id=member.segment_id
WHERE manifest.id=$1 AND manifest.state='qualified' AND manifest.dataset_kind='decision_inputs'
GROUP BY manifest.id HAVING min(segment.first_ordinal)<=$2 AND max(segment.last_ordinal)>=$3)`,
		dataset, first, last).Scan(&valid)
	if err != nil || !valid {
		if err == nil {
			err = console.ErrPrecondition
		}
		return "", "", "", "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v1d_incident_replay_inputs(
incident_id,dataset_id,first_ordinal,last_ordinal,source_identity,linked_by,linked_at
) VALUES($1,$2,$3,$4,$5,$6,$7)`, command.TargetID, dataset, first, last,
		source, principal.UserID, now)
	if err != nil {
		return "", "", "", "", a11ConstraintError(err)
	}
	return "replay_linked", "dataset", dataset, fmt.Sprintf("ordinals %d-%d", first, last), nil
}
