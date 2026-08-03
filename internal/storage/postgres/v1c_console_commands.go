package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

var c6SubmissionLimits = sandbox.SubmissionLimits{
	MaximumOrderNotional: "10", MaximumDailyNotional: "50",
	MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
}

const (
	c6InsertAuditSQL = `
INSERT INTO audit_events(
 id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
) VALUES($1,$2,$3,$4,$4,$5,$6)`
	c6InsertCommandSQL = `
INSERT INTO command_requests(
 id,deduplication_key,payload_hash,state,created_at,actor_user_id,
 session_id,command_kind,target_type,target_id,reason,idempotency_key,
 expected_revision,correlation_id,causation_id,audit_event_id,updated_at
) VALUES(
 $1,$2,$3,'pending',$4,$5,$6,$7,$8,$9,$10,$11,$12,$1,$1,$13,$4
)`
	c6RejectCommandSQL = `
UPDATE command_requests
SET state='rejected',result_payload=$2,applied_at=$3,updated_at=$3,
    entity_revision=entity_revision+1
WHERE id=$1 AND state='pending'`
)

type c6PendingCommand struct {
	accepted generated.CommandAccepted
	hash     string
	created  bool
}

func (store *A11ConsoleStore) beginC6Command(
	ctx context.Context,
	principal authentication.Principal,
	key, kind, targetType, targetID, reason string,
	expectedRevision *int64,
	payload any,
) (c6PendingCommand, error) {
	_, hash, err := a11CommandPayload(payload)
	if err != nil {
		return c6PendingCommand{}, console.ErrInvalidRequest
	}
	tx, existing, found, err := store.lockC6Command(
		ctx, principal, key, hash,
	)
	if err != nil {
		return c6PendingCommand{}, err
	}
	if found {
		return c6PendingCommand{accepted: existing, hash: hash}, nil
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	commandID, err := a11Identifier("c6-command")
	if err != nil {
		return c6PendingCommand{}, err
	}
	now := store.clock.Now().UTC
	if err = insertC6Command(
		ctx, tx, principal, commandID, key, hash, kind, targetType,
		targetID, reason, expectedRevision, now,
	); err != nil {
		return c6PendingCommand{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c6PendingCommand{}, err
	}
	return newC6PendingCommand(commandID, targetID, hash, now), nil
}

func (store *A11ConsoleStore) lockC6Command(
	ctx context.Context,
	principal authentication.Principal,
	key, hash string,
) (pgx.Tx, generated.CommandAccepted, bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, generated.CommandAccepted{}, false, err
	}
	lockIdentity := principal.UserID + "\x00" + key
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`,
		lockIdentity,
	); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, generated.CommandAccepted{}, false, err
	}
	if existing, found, lookupErr := lookupA11Command(
		ctx, tx, principal.UserID, key, hash,
	); lookupErr != nil {
		_ = tx.Rollback(context.Background())
		return nil, generated.CommandAccepted{}, false, lookupErr
	} else if found {
		if err = tx.Commit(ctx); err != nil {
			return nil, generated.CommandAccepted{}, false, err
		}
		return nil, existing, true, nil
	}
	return tx, generated.CommandAccepted{}, false, nil
}

func newC6PendingCommand(
	commandID, targetID, hash string,
	now time.Time,
) c6PendingCommand {
	return c6PendingCommand{
		accepted: generated.CommandAccepted{
			Id: commandID, TargetId: targetID, CorrelationId: commandID,
			CreatedAt: now, Revision: "1",
			State: generated.CommandAcceptedStatePending,
		},
		hash: hash, created: true,
	}
}

func insertC6Command(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	commandID, key, hash, kind, targetType, targetID, reason string,
	expectedRevision *int64,
	now time.Time,
) error {
	auditID, err := a11Identifier("c6-audit")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx, c6InsertAuditSQL,
		auditID, kind, principal.UserID, commandID, hash, now,
	); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx, c6InsertCommandSQL,
		commandID, a11Dedupe(principal.UserID, key), hash, now,
		principal.UserID, principal.SessionID, kind, targetType, targetID,
		reason, key, expectedRevision, auditID,
	)
	return a11ConstraintError(err)
}

func (store *A11ConsoleStore) completeC6Command(
	ctx context.Context,
	command c6PendingCommand,
	principal authentication.Principal,
	kind, target string,
	result map[string]any,
) (generated.CommandAccepted, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	accepted, err := completeA11Command(
		ctx, tx, command.accepted.Id, "", principal, kind, target,
		command.hash, result, store.clock.Now().UTC, command.accepted.Id,
	)
	return commitA11Accepted(ctx, tx, accepted, err)
}

func (store *A11ConsoleStore) rejectC6Command(
	ctx context.Context,
	command c6PendingCommand,
	code string,
) {
	payload, _ := json.Marshal(map[string]string{"code": code})
	now := store.clock.Now().UTC
	_, _ = store.pool.Exec(ctx, c6RejectCommandSQL,
		command.accepted.Id, payload, now,
	)
}

func c6ExpectedRevision(value string) (*int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return nil, console.ErrInvalidRequest
	}
	return &revision, nil
}

func c6ReasonHash(reason string) string {
	return authentication.AuthorizationBindingHash(reason)
}

func validateC6Consumed(
	principal authentication.Principal,
	consumed authentication.ConsumedAuthorization,
	purpose authentication.AuthorizationPurpose,
	reason string,
) error {
	if consumed.ID == "" || consumed.UserID != principal.UserID ||
		consumed.SessionID != principal.SessionID ||
		consumed.Purpose != purpose ||
		consumed.ReasonHash != c6ReasonHash(reason) {
		return console.ErrPrecondition
	}
	return nil
}
