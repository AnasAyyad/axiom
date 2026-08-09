package postgres

import (
	"context"
	"errors"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

const startSandboxStrategySessionSQL = `
UPDATE sandbox_strategy_sessions strategy
SET state='running',started_at=$4,revision=strategy.revision+1
FROM sandbox_runtime_sandbox_sessions parent
WHERE strategy.id=$1
  AND strategy.sandbox_session_id=parent.id
  AND strategy.created_by=$2
  AND strategy.revision=$3
  AND strategy.state='prepared'
  AND parent.state='ARMED'
  AND EXISTS (
    SELECT 1 FROM sandbox_runtime_sandbox_arms arm
    WHERE arm.sandbox_session_id=parent.id
      AND arm.actor_user_id=$2
      AND arm.actor_session_id=$5
      AND arm.revoked_at IS NULL
      AND arm.created_at <= $4
      AND arm.expires_at > $4
  )
RETURNING revision`

const stopSandboxStrategySessionSQL = `
UPDATE sandbox_strategy_sessions
SET state='stopped',stopped_at=$4,revision=revision+1
WHERE id=$1 AND created_by=$2 AND revision=$3
  AND state IN ('prepared','running','blocked')
RETURNING revision`

// StartSandboxStrategySession records an owner-authorized transition only
// after the existing parent session and its current arm are still valid. It
// does not submit an order or communicate with an exchange.
func (store *OwnerConsoleStore) StartSandboxStrategySession(
	ctx context.Context,
	principal authentication.Principal,
	id, key string,
	body generated.SandboxStrategySessionStartRequest,
	consumed authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil || validateSandboxQualificationConsumed(principal, consumed, authentication.PurposeSandboxArm, body.Reason) != nil {
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	pending, err := store.beginSandboxQualificationCommand(ctx, principal, key, "sandbox.strategy_session_start",
		"sandbox_strategy_session", id, body.Reason, expected,
		map[string]any{"strategy_session_id": id, "expected_revision": body.ExpectedRevision,
			"authorization_id": consumed.ID, "reason": body.Reason})
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	now := store.clock.Now().UTC
	var revision int64
	err = store.pool.QueryRow(ctx, startSandboxStrategySessionSQL,
		id, principal.UserID, *expected, now, principal.SessionID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_strategy_session_start_rejected")
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	return store.completeSandboxQualificationCommand(ctx, pending, principal, "sandbox.strategy_session_start", id,
		map[string]any{"strategy_session_id": id, "state": "running", "revision": revision})
}

// StopSandboxStrategySession is deliberately independent of arm validity so
// an owner can always halt new strategy entries after expiry or revocation.
func (store *OwnerConsoleStore) StopSandboxStrategySession(
	ctx context.Context,
	principal authentication.Principal,
	id, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(ctx, principal, key, "sandbox.strategy_session_stop",
		"sandbox_strategy_session", id, body.Reason, expected,
		map[string]any{"strategy_session_id": id, "body": body})
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	now := store.clock.Now().UTC
	var revision int64
	err = store.pool.QueryRow(ctx, stopSandboxStrategySessionSQL,
		id, principal.UserID, *expected, now).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_strategy_session_stop_rejected")
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	return store.completeSandboxQualificationCommand(ctx, pending, principal, "sandbox.strategy_session_stop", id,
		map[string]any{"strategy_session_id": id, "state": "stopped", "revision": revision})
}

var _ console.SandboxCommandService = (*OwnerConsoleStore)(nil)
