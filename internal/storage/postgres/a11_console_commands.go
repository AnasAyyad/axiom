package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RiskCommand atomically audits and applies a policy-gated global pause/resume.
func (store *A11ConsoleStore) RiskCommand(ctx context.Context, principal authentication.Principal, action, key string, body generated.RevisionCommandRequest) (generated.CommandAccepted, error) {
	payload, hash, err := a11CommandPayload(map[string]any{"action": action, "body": body})
	if err != nil || body.Reason == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:a11:risk'))`); err != nil {
		return generated.CommandAccepted{}, err
	}
	if existing, found, lookupErr := lookupA11Command(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.CommandAccepted{}, lookupErr
	} else if found {
		return existing, tx.Commit(ctx)
	}
	riskRevision, err := a11RiskRevision(ctx, tx, body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	correlation := commandID
	if err = insertA11Command(ctx, tx, commandID, principal, key, hash, action, "risk", "global", body.Reason, now, auditID, correlation); err != nil {
		return generated.CommandAccepted{}, err
	}
	current, next, err := nextA11RiskState(ctx, tx, action, now)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if next != current {
		eventID, _ := a11Identifier("risk-state")
		evidence := a11Hash(payload)
		if _, err = tx.Exec(ctx, `INSERT INTO risk_state_events(id,prior_state,next_state,reason_code,actor,evidence_hash,occurred_at,entity_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, eventID, current, next, "manual_"+action, principal.UserID, evidence, now, riskRevision+1); err != nil {
			return generated.CommandAccepted{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE api_entity_revisions SET revision=revision+1,updated_at=$1 WHERE entity_type='risk' AND entity_id='global'`, now); err != nil {
			return generated.CommandAccepted{}, err
		}
	}
	result := map[string]any{"state": next, "real_trading_enabled": false}
	accepted, err := completeA11Command(ctx, tx, commandID, auditID, principal, action, "global", hash, result, now, correlation)
	return commitA11Accepted(ctx, tx, accepted, err)
}

func commitA11Accepted(ctx context.Context, tx pgx.Tx, accepted generated.CommandAccepted, prior error) (generated.CommandAccepted, error) {
	if prior != nil {
		return generated.CommandAccepted{}, prior
	}
	if err := tx.Commit(ctx); err != nil {
		return generated.CommandAccepted{}, err
	}
	return accepted, nil
}

func a11RiskRevision(ctx context.Context, tx pgx.Tx, expected string) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM api_entity_revisions WHERE entity_type='risk' AND entity_id='global' FOR UPDATE`).Scan(&revision); err != nil {
		return 0, err
	}
	if strconv.FormatInt(revision, 10) != expected {
		return 0, console.ErrConflict
	}
	return revision, nil
}

func nextA11RiskState(ctx context.Context, tx pgx.Tx, action string, now time.Time) (string, string, error) {
	var current string
	_ = tx.QueryRow(ctx, `SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1`).Scan(&current)
	if current == "" {
		current = "PAUSED"
	}
	if action == "pause" && current != "LOCKED" {
		return current, "PAUSED", nil
	}
	if action != "resume" {
		if action == "pause" {
			return "", "", console.ErrPrecondition
		}
		return "", "", console.ErrInvalidRequest
	}
	if current != "PAUSED" {
		return "", "", console.ErrPrecondition
	}
	if ready, err := a11RiskRecoveryReady(ctx, tx, now); err != nil || !ready {
		return "", "", console.ErrPrecondition
	}
	return current, "NORMAL", nil
}

func a11RiskRecoveryReady(ctx context.Context, tx pgx.Tx, now time.Time) (bool, error) {
	var blockers int
	err := tx.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM incidents WHERE state<>'resolved' AND severity='critical')+
      (SELECT count(*) FROM reconciliation_cases WHERE state IN ('open','quarantined'))+
      (SELECT count(*) FROM quarantined_scopes WHERE released_at IS NULL)+
      (SELECT count(*) FROM orders WHERE state='unknown')+
      CASE WHEN NOT EXISTS (SELECT 1 FROM startup_recovery_attempts attempt WHERE attempt.state='ready_paused' AND
        (SELECT count(*) FROM startup_recovery_evidence evidence WHERE evidence.attempt_id=attempt.id)=14)
        THEN 1 ELSE 0 END+
      CASE WHEN NOT EXISTS (SELECT 1 FROM market_data_segments segment JOIN exchanges exchange ON exchange.id=segment.exchange_id
        WHERE exchange.id='binance' AND exchange.environment='production_public' AND segment.state='ready' AND segment.ended_at >= $1)
        THEN 1 ELSE 0 END`, now.Add(-5*time.Minute)).Scan(&blockers)
	return blockers == 0, err
}

// ControlJob records pause/resume/step intent and applies only valid lifecycle edges.
func (store *A11ConsoleStore) ControlJob(ctx context.Context, principal authentication.Principal, id, action, key string, body generated.RevisionCommandRequest) (generated.CommandAccepted, error) {
	_, hash, err := a11CommandPayload(map[string]any{"id": id, "action": action, "body": body})
	if err != nil || body.Reason == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupA11Command(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.CommandAccepted{}, lookupErr
	} else if found {
		return existing, tx.Commit(ctx)
	}
	var state string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT state,progress_revision FROM jobs WHERE id=$1 AND job_type='replay' FOR UPDATE`, id).Scan(&state, &revision); errors.Is(err, pgx.ErrNoRows) {
		return generated.CommandAccepted{}, console.ErrNotFound
	} else if err != nil {
		return generated.CommandAccepted{}, err
	}
	want, next, valid := a11ReplayTransition(action)
	if !valid {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	if strconv.FormatInt(revision, 10) != body.ExpectedRevision || state != want {
		return generated.CommandAccepted{}, console.ErrConflict
	}
	accepted, err := applyA11ReplayControl(ctx, tx, principal, id, action, key, hash, next, body,
		store.clock.Now().UTC)
	return commitA11Accepted(ctx, tx, accepted, err)
}

func applyA11ReplayControl(ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	id, action, key, hash, next string, body generated.RevisionCommandRequest,
	now time.Time) (generated.CommandAccepted, error) {
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	if err := insertA11Command(ctx, tx, commandID, principal, key, hash, action+"_replay", "replay", id,
		body.Reason, now, auditID, commandID); err != nil {
		return generated.CommandAccepted{}, err
	}
	_, err := tx.Exec(ctx, `UPDATE jobs SET state=$2,
      claim_owner=CASE WHEN $2='QUEUED' THEN NULL ELSE claim_owner END,
      claim_epoch=CASE WHEN $2='QUEUED' THEN NULL ELSE claim_epoch END,
      claim_expires_at=CASE WHEN $2='QUEUED' THEN NULL ELSE claim_expires_at END,
      single_step=CASE WHEN $2='QUEUED' THEN $4 ELSE single_step END,
      progress_revision=progress_revision+1,updated_at=$3 WHERE id=$1`, id, next, now, action == "step")
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	return completeA11Command(ctx, tx, commandID, auditID, principal, action+"_replay", id, hash,
		map[string]any{"job_id": id, "state": next, "single_step": action == "step"}, now, commandID)
}

func a11ReplayTransition(action string) (string, string, bool) {
	switch action {
	case "pause":
		return "RUNNING", "PAUSE_REQUESTED", true
	case "resume", "step":
		return "PAUSED", "QUEUED", true
	default:
		return "", "", false
	}
}

func a11CommandPayload(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return payload, a11Hash(payload), nil
}
func a11Hash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
func a11Dedupe(actor, key string) string { return a11Hash([]byte(actor + "\x00" + key)) }
func a11Identifier(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func a11ConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return console.ErrConflict
	}
	return err
}

func lookupA11Command(ctx context.Context, tx pgx.Tx, actor, key, hash string) (generated.CommandAccepted, bool, error) {
	var id, payloadHash, state, target, correlation string
	var created time.Time
	var revision int64
	err := tx.QueryRow(ctx, `SELECT id,payload_hash,state,coalesce(target_id,''),coalesce(correlation_id,id),created_at,entity_revision FROM command_requests WHERE actor_user_id=$1 AND idempotency_key=$2`, actor, key).Scan(&id, &payloadHash, &state, &target, &correlation, &created, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.CommandAccepted{}, false, nil
	}
	if err != nil {
		return generated.CommandAccepted{}, false, err
	}
	if payloadHash != hash {
		return generated.CommandAccepted{}, false, console.ErrIdempotencyConflict
	}
	return generated.CommandAccepted{Id: id, TargetId: target, CorrelationId: correlation, CreatedAt: created, Revision: strconv.FormatInt(revision, 10), State: generated.CommandAcceptedState(state)}, true, nil
}

func insertA11Command(ctx context.Context, tx pgx.Tx, id string, principal authentication.Principal, key, hash, kind, targetType, targetID, reason string, now time.Time, auditID, correlation string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events(id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, auditID, kind, principal.UserID, id, correlation, hash, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO command_requests(id,deduplication_key,payload_hash,state,created_at,actor_user_id,session_id,command_kind,target_type,target_id,reason,idempotency_key,correlation_id,causation_id,audit_event_id,updated_at)
    VALUES($1,$2,$3,'pending',$4,$5,$6,$7,$8,$9,$10,$11,$12,$1,$13,$4)`, id, a11Dedupe(principal.UserID, key), hash, now, principal.UserID, principal.SessionID, kind, targetType, targetID, reason, key, correlation, auditID)
	return a11ConstraintError(err)
}

func completeA11Command(ctx context.Context, tx pgx.Tx, id, auditID string, principal authentication.Principal, kind, target, hash string, result map[string]any, now time.Time, correlation string) (generated.CommandAccepted, error) {
	resultJSON, _ := json.Marshal(result)
	if _, err := tx.Exec(ctx, `UPDATE command_requests SET state='applied',result_payload=$2,applied_at=$3,updated_at=$3,entity_revision=entity_revision+1 WHERE id=$1`, id, string(resultJSON), now); err != nil {
		return generated.CommandAccepted{}, err
	}
	_ = auditID
	_ = principal
	_ = hash
	eventID, _ := a11Identifier("event")
	payloadHash := a11Hash(resultJSON)
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(id,topic,payload_hash,created_at,stream,schema_version,entity_type,entity_id,entity_revision,event_time,correlation_id,causation_id,payload) VALUES($1,$2,$3,$4,$5,'axiom.stream.v1',$6,$7,1,$4,$8,$9,$10)`, eventID, kind, payloadHash, now, a11StreamForKind(kind), "command", target, correlation, id, string(resultJSON)); err != nil {
		return generated.CommandAccepted{}, err
	}
	return generated.CommandAccepted{Id: id, TargetId: target, CorrelationId: correlation, CreatedAt: now, Revision: "2", State: generated.CommandAcceptedStateApplied}, nil
}

func a11StreamForKind(kind string) string {
	switch {
	case kind == "pause" || kind == "resume":
		return "risk"
	case kind == "create_shadow" || kind == "stop_shadow":
		return "shadow"
	default:
		return "job"
	}
}

var _ console.CommandService = (*A11ConsoleStore)(nil)
