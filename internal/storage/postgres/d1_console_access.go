package postgres

import (
	"context"

	"errors"
	"sort"

	"strings"
	"time"

	"axiom/internal/api/console"

	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

func applyD1QualificationStart(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	commandID string,
	now time.Time,
) (map[string]any, error) {
	storageReady, err := d5HeavyWorkAllowed(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !storageReady {
		return nil, console.ErrPrecondition
	}
	revision, active, ownerRequired, err := d1QualificationDefinition(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if !active || !ownerRequired || revision != command.ExpectedRevision {
		return nil, console.ErrConflict
	}
	available, err := d1QualificationAvailable(ctx, tx, command.TargetID)
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
	runID, _ := a11Identifier("qualification")
	if _, err = tx.Exec(ctx, `
INSERT INTO v1d_qualification_runs(
  id,qualification_id,command_id,state,revision,source_sha,configuration_hash,
  image_digest,server_identity,started_by,created_at,updated_at
) VALUES ($1,$2,$3,'PREFLIGHT',1,$4,$5,$6,$7,$8,$9,$9)`, runID, command.TargetID,
		commandID, sourceSHA, configurationHash, optionalD1String(command.Payload["image_digest"]),
		optionalD1String(command.Payload["server_identity"]), principal.UserID, now); err != nil {
		return nil, err
	}
	return map[string]any{"qualification_id": command.TargetID, "run_id": runID,
		"state": "PREFLIGHT", "revision": 1}, nil
}

func d1QualificationDefinition(
	ctx context.Context, tx pgx.Tx, qualificationID string,
) (int64, bool, bool, error) {
	var revision int64
	var active, ownerRequired bool
	err := tx.QueryRow(ctx, `SELECT definition_revision,active,owner_start_required
FROM v1d_qualification_catalogue WHERE id=$1 FOR SHARE`, qualificationID).Scan(
		&revision, &active, &ownerRequired,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, console.ErrNotFound
	}
	return revision, active, ownerRequired, err
}

func d1QualificationAvailable(ctx context.Context, tx pgx.Tx, qualificationID string) (bool, error) {
	var activeRuns int
	err := tx.QueryRow(ctx, `SELECT count(*)::integer FROM v1d_qualification_runs
WHERE qualification_id=$1 AND state IN ('PREFLIGHT','QUEUED','RUNNING','ABORT_REQUESTED')`,
		qualificationID).Scan(&activeRuns)
	return activeRuns == 0, err
}

func applyD1QualificationAbort(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	now time.Time,
) (map[string]any, error) {
	var state string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT state,revision FROM v1d_qualification_runs WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(&state, &revision)
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
UPDATE v1d_qualification_runs SET state=$2,revision=revision+1,updated_at=$3,
  completed_at=CASE WHEN $2='CANCELED' THEN $3 ELSE completed_at END
WHERE id=$1`, command.TargetID, next, now); err != nil {
		return nil, err
	}
	_ = principal
	return map[string]any{"run_id": command.TargetID, "state": next, "revision": revision + 1}, nil
}

func applyD1RoleChange(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.D1Command,
	commandID string,
	now time.Time,
) (map[string]any, error) {
	revision, status, err := d1RoleTarget(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision || status != "active" {
		return nil, console.ErrConflict
	}
	roles, err := d1ValidatedRoles(command.Payload["roles"])
	if err != nil {
		return nil, err
	}
	prior, err := d1UserRoles(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if err = d1ProtectLastOwner(ctx, tx, prior, roles); err != nil {
		return nil, err
	}
	if err = d1ReplaceRoles(ctx, tx, command.TargetID, roles, now); err != nil {
		return nil, err
	}
	if err = d1RecordRoleChange(ctx, tx, principal, command, commandID, prior, roles, revision+1, now); err != nil {
		return nil, err
	}
	return map[string]any{"user_id": command.TargetID, "roles": roles, "role_revision": revision + 1}, nil
}

func d1RoleTarget(ctx context.Context, tx pgx.Tx, userID string) (int64, string, error) {
	var revision int64
	var status string
	err := tx.QueryRow(ctx, `SELECT role_revision,status FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(
		&revision, &status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", console.ErrNotFound
	}
	return revision, status, err
}

func d1ValidatedRoles(value any) ([]string, error) {
	rawRoles, ok := value.([]string)
	if !ok || len(rawRoles) == 0 || len(rawRoles) > 4 {
		return nil, console.ErrInvalidRequest
	}
	roles := append([]string(nil), rawRoles...)
	sort.Strings(roles)
	roles = dedupeD1Strings(roles)
	for _, role := range roles {
		if !strings.Contains(" researcher operator auditor owner ", " "+role+" ") {
			return nil, console.ErrInvalidRequest
		}
	}
	return roles, nil
}

func d1UserRoles(ctx context.Context, tx pgx.Tx, userID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT role_id FROM user_roles WHERE user_id=$1 ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func d1ProtectLastOwner(ctx context.Context, tx pgx.Tx, prior, roles []string) error {
	if !containsD1(prior, "owner") || containsD1(roles, "owner") {
		return nil
	}
	var owners int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT user_id)::integer FROM user_roles
JOIN users ON users.id=user_roles.user_id
WHERE role_id='owner' AND users.status='active'`).Scan(&owners)
	if err != nil {
		return err
	}
	if owners <= 1 {
		return console.ErrPrecondition
	}
	return nil
}

func d1ReplaceRoles(ctx context.Context, tx pgx.Tx, userID string, roles []string, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, role := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role_id,granted_at) VALUES ($1,$2,$3)`,
			userID, role, now); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE users SET role_revision=role_revision+1 WHERE id=$1`, userID)
	return err
}

func d1RecordRoleChange(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal, command console.D1Command,
	commandID string, prior, roles []string, revision int64, now time.Time,
) error {
	eventID, _ := a11Identifier("role-change")
	_, err := tx.Exec(ctx, `
INSERT INTO v1d_role_change_events(
  id,command_id,target_user_id,prior_roles,new_roles,role_revision,
  actor_user_id,reason,occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, eventID, commandID, command.TargetID,
		prior, roles, revision, principal.UserID, command.Reason, now)
	return err
}
