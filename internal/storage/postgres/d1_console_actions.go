package postgres

import (
	"context"

	"errors"

	"strings"
	"time"

	"axiom/internal/api/console"

	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

func applyD1Alert(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	if command.Action == "escalate" {
		return applyD4AlertEscalation(ctx, tx, principal, command, now)
	}
	if command.Action != "acknowledge" {
		return nil, console.ErrInvalidRequest
	}
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT state,revision FROM alerts WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if command.ExpectedRevision != revision || state == "resolved" {
		return nil, console.ErrConflict
	}
	next := revision + 1
	if _, err = tx.Exec(ctx, `
INSERT INTO alert_acknowledgements(alert_id,revision,actor,reason,acknowledged_at)
VALUES ($1,$2,$3,$4,$5)`, command.TargetID, next, principal.UserID, command.Reason, now); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `
UPDATE alerts SET state='acknowledged',acknowledged_at=$2,revision=$3 WHERE id=$1`, command.TargetID, now, next); err != nil {
		return nil, err
	}
	return map[string]any{"alert_id": command.TargetID, "state": "acknowledged", "revision": next}, nil
}

func applyD1Report(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	commandID string,
	now time.Time,
) (map[string]any, error) {
	if command.Action != "create" || command.ExpectedRevision != 1 {
		return nil, console.ErrConflict
	}
	return queueD4Report(ctx, tx, principal, command, commandID, now, nil, nil)
}

func applyD1ExportDelete(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	if command.Action != "delete" || command.ExpectedRevision != 1 {
		return nil, console.ErrConflict
	}
	var deleted *time.Time
	var holds int
	err := tx.QueryRow(ctx, `SELECT deleted_at FROM v1d_export_artifacts
	    WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*)::integer FROM v1d_artifact_holds
	    WHERE artifact_id=$1 AND released_at IS NULL`, command.TargetID).Scan(&holds); err != nil {
		return nil, err
	}
	if deleted != nil || holds > 0 {
		return nil, console.ErrPrecondition
	}
	if _, err = tx.Exec(ctx, `
UPDATE v1d_export_artifacts SET content=NULL,deleted_at=$2,deletion_reason=$3 WHERE id=$1`,
		command.TargetID, now, command.Reason); err != nil {
		return nil, err
	}
	eventID, _ := a11Identifier("artifact-access")
	if _, err = tx.Exec(ctx, `
INSERT INTO v1d_artifact_access_events(
  id,artifact_id,actor_user_id,action,reason,correlation_id,occurred_at
) VALUES ($1,$2,$3,'deleted',$4,$1,$5)`, eventID, command.TargetID,
		principal.UserID, command.Reason, now); err != nil {
		return nil, err
	}
	return map[string]any{"artifact_id": command.TargetID, "deleted": true}, nil
}

func applyD1ArtifactHold(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	if command.ExpectedRevision != 1 {
		return nil, console.ErrConflict
	}
	holdType, typeOK := command.Payload["hold_type"].(string)
	reference, referenceOK := command.Payload["reference_id"].(string)
	if !typeOK || !referenceOK || !strings.Contains(" incident reproducibility ", " "+holdType+" ") {
		return nil, console.ErrInvalidRequest
	}
	var referenceExists bool
	query := `SELECT EXISTS(SELECT 1 FROM incidents WHERE id=$1)`
	if holdType == "reproducibility" {
		query = `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1)`
	}
	if err := tx.QueryRow(ctx, query, reference).Scan(&referenceExists); err != nil {
		return nil, err
	}
	if !referenceExists {
		return nil, console.ErrPrecondition
	}
	var available bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
      SELECT 1 FROM v1d_export_artifacts WHERE id=$1 AND deleted_at IS NULL
    )`, command.TargetID).Scan(&available); err != nil || !available {
		return nil, console.ErrPrecondition
	}
	holdID, _ := a11Identifier("artifact-hold")
	if _, err := tx.Exec(ctx, `
INSERT INTO v1d_artifact_holds(
  id,artifact_id,hold_type,reference_id,reason,actor_user_id,created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)`, holdID, command.TargetID, holdType,
		reference, command.Reason, principal.UserID, now); err != nil {
		return nil, a11ConstraintError(err)
	}
	eventID, _ := a11Identifier("artifact-access")
	if _, err := tx.Exec(ctx, `
INSERT INTO v1d_artifact_access_events(
  id,artifact_id,actor_user_id,action,reason,correlation_id,occurred_at
) VALUES ($1,$2,$3,'held',$4,$1,$5)`, eventID, command.TargetID,
		principal.UserID, command.Reason, now); err != nil {
		return nil, err
	}
	return map[string]any{"artifact_id": command.TargetID, "hold_id": holdID, "held": true}, nil
}

func applyD1Incident(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	return applyD4IncidentTransition(ctx, tx, principal, command, now)
}

func applyD1ConfigurationActivation(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
      SELECT 1 FROM configuration_versions WHERE id=$1
    )`, command.TargetID).Scan(&exists); err != nil || !exists {
		return nil, console.ErrNotFound
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM configuration_activations`).Scan(&revision); err != nil {
		return nil, err
	}
	if command.ExpectedRevision != revision {
		return nil, console.ErrConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at)
VALUES ($1,$2,$3,$4)`, command.TargetID, principal.UserID, command.Reason, now); err != nil {
		return nil, err
	}
	return map[string]any{"configuration_id": command.TargetID, "state": "active", "revision": revision}, nil
}

func applyD1LabControl(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	var state, jobType string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT state,job_type,progress_revision FROM jobs WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(
		&state, &jobType, &revision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision || strings.HasPrefix(jobType, "report:") {
		return nil, console.ErrConflict
	}
	next, reproduce, err := d1LabTransition(command.Action, state)
	if err != nil {
		return nil, err
	}
	if reproduce {
		return applyD1LabReproduce(ctx, tx, principal, command, jobType, now)
	}
	if _, err = tx.Exec(ctx, `
UPDATE jobs SET state=$2,progress_revision=progress_revision+1,updated_at=$3,
  completed_at=CASE WHEN $2='CANCELED' THEN $3 ELSE completed_at END,
  claim_owner=CASE WHEN $2 IN ('QUEUED','CANCELED') THEN NULL ELSE claim_owner END,
  claim_epoch=CASE WHEN $2 IN ('QUEUED','CANCELED') THEN NULL ELSE claim_epoch END,
  claim_expires_at=CASE WHEN $2 IN ('QUEUED','CANCELED') THEN NULL ELSE claim_expires_at END
WHERE id=$1`, command.TargetID, next, now); err != nil {
		return nil, err
	}
	return map[string]any{"job_id": command.TargetID, "state": next, "revision": revision + 1}, nil
}

func d1LabTransition(action, state string) (string, bool, error) {
	switch action {
	case "pause":
		if state == "RUNNING" {
			return "PAUSE_REQUESTED", false, nil
		}
	case "resume":
		if state == "PAUSED" {
			return "QUEUED", false, nil
		}
	case "cancel":
		if state == "QUEUED" {
			return "CANCELED", false, nil
		}
		if state == "RUNNING" || state == "PAUSE_REQUESTED" || state == "PAUSED" {
			return "CANCEL_REQUESTED", false, nil
		}
	case "reproduce":
		if state == "SUCCEEDED" || state == "FAILED" || state == "CANCELED" {
			return "", true, nil
		}
	}
	return "", false, console.ErrPrecondition
}

func applyD1LabReproduce(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	jobType string,
	now time.Time,
) (map[string]any, error) {
	storageReady, err := d5HeavyWorkAllowed(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !storageReady {
		return nil, console.ErrQuota
	}
	var payload []byte
	var payloadHash string
	var maxAttempts int32
	if err = tx.QueryRow(ctx, `
SELECT request_payload,payload_hash,max_attempts FROM jobs WHERE id=$1`, command.TargetID).Scan(
		&payload, &payloadHash, &maxAttempts,
	); err != nil {
		return nil, err
	}
	newID, _ := a11Identifier("reproduction")
	if _, err := tx.Exec(ctx, `
INSERT INTO jobs(
  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
  owner_user_id,request_payload,max_attempts
) VALUES ($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,$8)`, newID, jobType,
		a11Dedupe(principal.UserID, command.IdempotencyKey+":reproduction"),
		payloadHash, now, principal.UserID, string(payload), maxAttempts); err != nil {
		return nil, err
	}
	return map[string]any{"source_job_id": command.TargetID, "job_id": newID, "state": "QUEUED"}, nil
}
