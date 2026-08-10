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

var sandboxQualificationSubmissionLimits = sandbox.SubmissionLimits{
	MaximumOrderNotional: "10", MaximumDailyNotional: "50",
	MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
}

const (
	sandboxQualificationInsertAuditSQL = `
INSERT INTO audit_events(
 id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
) VALUES($1,$2,$3,$4,$4,$5,$6)`
	sandboxQualificationInsertCommandSQL = `
INSERT INTO command_requests(
 id,deduplication_key,payload_hash,state,created_at,actor_user_id,
 session_id,command_kind,target_type,target_id,reason,idempotency_key,
 expected_revision,correlation_id,causation_id,audit_event_id,updated_at
) VALUES(
 $1,$2,$3,'pending',$4,$5,$6,$7,$8,$9,$10,$11,$12,$1,$1,$13,$4
)`
	sandboxQualificationRejectCommandSQL = `
UPDATE command_requests
SET state='rejected',result_payload=$2,applied_at=$3,updated_at=$3,
    entity_revision=entity_revision+1
WHERE id=$1 AND state='pending'`
)

type sandboxQualificationPendingCommand struct {
	accepted generated.CommandAccepted
	hash     string
	created  bool
}

func (store *OwnerConsoleStore) beginSandboxQualificationCommand(
	ctx context.Context,
	principal authentication.Principal,
	key, kind, targetType, targetID, reason string,
	expectedRevision *int64,
	payload any,
) (sandboxQualificationPendingCommand, error) {
	_, hash, err := ownerConsoleCommandPayload(payload)
	if err != nil {
		return sandboxQualificationPendingCommand{}, console.ErrInvalidRequest
	}
	tx, existing, found, err := store.lockSandboxQualificationCommand(
		ctx, principal, key, hash,
	)
	if err != nil {
		return sandboxQualificationPendingCommand{}, err
	}
	if found {
		return sandboxQualificationPendingCommand{accepted: existing, hash: hash}, nil
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	commandID, err := ownerConsoleIdentifier("sandbox_qualification-command")
	if err != nil {
		return sandboxQualificationPendingCommand{}, err
	}
	now := store.clock.Now().UTC
	if err = insertSandboxQualificationCommand(
		ctx, tx, principal, commandID, key, hash, kind, targetType,
		targetID, reason, expectedRevision, now,
	); err != nil {
		return sandboxQualificationPendingCommand{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandboxQualificationPendingCommand{}, err
	}
	return newSandboxQualificationPendingCommand(commandID, targetID, hash, now), nil
}

func (store *OwnerConsoleStore) lockSandboxQualificationCommand(
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
	if existing, found, lookupErr := lookupOwnerConsoleCommand(
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

func newSandboxQualificationPendingCommand(
	commandID, targetID, hash string,
	now time.Time,
) sandboxQualificationPendingCommand {
	return sandboxQualificationPendingCommand{
		accepted: generated.CommandAccepted{
			Id: commandID, TargetId: targetID, CorrelationId: commandID,
			CreatedAt: now, Revision: "1",
			State: generated.CommandAcceptedStatePending,
		},
		hash: hash, created: true,
	}
}

func insertSandboxQualificationCommand(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	commandID, key, hash, kind, targetType, targetID, reason string,
	expectedRevision *int64,
	now time.Time,
) error {
	auditID, err := ownerConsoleIdentifier("sandbox_qualification-audit")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx, sandboxQualificationInsertAuditSQL,
		auditID, kind, principal.UserID, commandID, hash, now,
	); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx, sandboxQualificationInsertCommandSQL,
		commandID, ownerConsoleDedupe(principal.UserID, key), hash, now,
		principal.UserID, principal.SessionID, kind, targetType, targetID,
		reason, key, expectedRevision, auditID,
	)
	return ownerConsoleConstraintError(err)
}

func (store *OwnerConsoleStore) completeSandboxQualificationCommand(
	ctx context.Context,
	command sandboxQualificationPendingCommand,
	principal authentication.Principal,
	kind, target string,
	result map[string]any,
) (generated.CommandAccepted, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	accepted, err := completeOwnerConsoleCommand(
		ctx, tx, command.accepted.Id, "", principal, kind, target,
		command.hash, result, store.clock.Now().UTC, command.accepted.Id,
	)
	return commitOwnerConsoleAccepted(ctx, tx, accepted, err)
}

func (store *OwnerConsoleStore) rejectSandboxQualificationCommand(
	ctx context.Context,
	command sandboxQualificationPendingCommand,
	code string,
) {
	payload, _ := json.Marshal(map[string]string{"code": code})
	now := store.clock.Now().UTC
	_, _ = store.pool.Exec(ctx, sandboxQualificationRejectCommandSQL,
		command.accepted.Id, payload, now,
	)
}

func sandboxQualificationExpectedRevision(value string) (*int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return nil, console.ErrInvalidRequest
	}
	return &revision, nil
}

func sandboxQualificationReasonHash(reason string) string {
	return authentication.AuthorizationBindingHash(reason)
}

func validateSandboxQualificationConsumed(
	principal authentication.Principal,
	consumed authentication.ConsumedAuthorization,
	purpose authentication.AuthorizationPurpose,
	reason string,
) error {
	if consumed.ID == "" || consumed.UserID != principal.UserID ||
		consumed.SessionID != principal.SessionID ||
		consumed.Purpose != purpose ||
		consumed.ReasonHash != sandboxQualificationReasonHash(reason) {
		return console.ErrPrecondition
	}
	return nil
}
