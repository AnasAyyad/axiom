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

func applyOwnerControlQualificationStart(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	commandID string,
	now time.Time,
) (map[string]any, error) {
	storageReady, err := operationalReadinessHeavyWorkAllowed(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !storageReady {
		return nil, console.ErrPrecondition
	}
	revision, active, ownerRequired, err := ownerControlQualificationDefinition(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if !active || !ownerRequired || revision != command.ExpectedRevision {
		return nil, console.ErrConflict
	}
	available, err := ownerControlQualificationAvailable(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, console.ErrConflict
	}
	sourceSHA, sourceOK := command.Payload["source_sha"].(string)
	configurationHash, hashOK := command.Payload["configuration_hash"].(string)
	if !sourceOK || !hashOK {
		return nil, console.ErrInvalidRequest
	}
	runID, _ := ownerConsoleIdentifier("qualification")
	if _, err = tx.Exec(ctx, `
INSERT INTO owner_console_qualification_runs(
  id,qualification_id,command_id,state,revision,source_sha,configuration_hash,
  image_digest,server_identity,started_by,created_at,updated_at
) VALUES ($1,$2,$3,'PREFLIGHT',1,$4,$5,$6,$7,$8,$9,$9)`, runID, command.TargetID,
		commandID, sourceSHA, configurationHash, optionalOwnerControlString(command.Payload["image_digest"]),
		optionalOwnerControlString(command.Payload["server_identity"]), principal.UserID, now); err != nil {
		return nil, err
	}
	return map[string]any{"qualification_id": command.TargetID, "run_id": runID,
		"state": "PREFLIGHT", "revision": 1}, nil
}

func ownerControlQualificationDefinition(
	ctx context.Context, tx pgx.Tx, qualificationID string,
) (int64, bool, bool, error) {
	var revision int64
	var active, ownerRequired bool
	err := tx.QueryRow(ctx, `SELECT definition_revision,active,owner_start_required
FROM owner_console_qualification_catalogue WHERE id=$1 FOR SHARE`, qualificationID).Scan(
		&revision, &active, &ownerRequired,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, console.ErrNotFound
	}
	return revision, active, ownerRequired, err
}

func ownerControlQualificationAvailable(ctx context.Context, tx pgx.Tx, qualificationID string) (bool, error) {
	var activeRuns int
	err := tx.QueryRow(ctx, `SELECT count(*)::integer FROM owner_console_qualification_runs
WHERE qualification_id=$1 AND state IN ('PREFLIGHT','QUEUED','RUNNING','ABORT_REQUESTED')`,
		qualificationID).Scan(&activeRuns)
	return activeRuns == 0, err
}

func applyOwnerControlQualificationAbort(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	now time.Time,
) (map[string]any, error) {
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT state,revision FROM owner_console_qualification_runs WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(&state, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision ||
		!strings.Contains(" PREFLIGHT QUEUED RUNNING ", " "+state+" ") {
		return nil, console.ErrConflict
	}
	next := "ABORT_REQUESTED"
	if state == "PREFLIGHT" || state == "QUEUED" {
		next = "CANCELED"
	}
	if _, err = tx.Exec(ctx, `
UPDATE owner_console_qualification_runs SET state=$2,revision=revision+1,updated_at=$3,
  completed_at=CASE WHEN $2='CANCELED' THEN $3 ELSE completed_at END
WHERE id=$1`, command.TargetID, next, now); err != nil {
		return nil, err
	}
	_ = principal
	return map[string]any{"run_id": command.TargetID, "state": next, "revision": revision + 1}, nil
}
