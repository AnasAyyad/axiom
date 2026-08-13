package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const sandboxQualificationOrderCommandTargetSQL = `
SELECT account_id,account_epoch,client_order_id,updated_at
FROM sandbox_runtime_submission_outbox WHERE order_id=$1`

type sandboxQualificationOrderCommandTarget struct {
	accountID, clientOrderID string
	epoch                    int64
	updatedAt                time.Time
}

// QueueSandboxOrderCommand queues only query/cancel work for the credential
// owning engine. It intentionally has no create command kind.
func (store *OwnerConsoleStore) QueueSandboxOrderCommand(
	ctx context.Context,
	principal authentication.Principal,
	orderID, action, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil || (action != "cancel" && action != "query") {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.order_"+action, "sandbox_order",
		orderID, body.Reason, expected,
		map[string]any{"order_id": orderID, "action": action, "body": body},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	target, err := store.sandboxQualificationOrderCommandTarget(ctx, orderID)
	if errors.Is(err, console.ErrNotFound) {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_not_found")
		return generated.CommandAccepted{}, console.ErrNotFound
	}
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if target.updatedAt.UnixNano() != *expected {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_revision_conflict")
		return generated.CommandAccepted{}, console.ErrConflict
	}
	kind := sandbox.EngineCommandQuery
	if action == "cancel" {
		kind = sandbox.EngineCommandCancel
	}
	if err = store.sandbox_runtime.QueueEngineCommand(ctx, sandbox.EngineCommand{
		ID: pending.accepted.Id, AccountID: sandbox.AccountID(target.accountID),
		AccountEpoch: uint64(target.epoch), Kind: kind,
		ClientOrderID: target.clientOrderID, State: sandbox.EngineCommandPending,
		RequestedAt: pending.accepted.CreatedAt,
	}); err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_engine_command_rejected")
		return generated.CommandAccepted{}, sandboxQualificationConsoleError(err)
	}
	return store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.order_"+action, orderID,
		map[string]any{"order_id": orderID, "state": "PENDING", "action": action},
	)
}

func (store *OwnerConsoleStore) sandboxQualificationOrderCommandTarget(
	ctx context.Context,
	orderID string,
) (sandboxQualificationOrderCommandTarget, error) {
	var target sandboxQualificationOrderCommandTarget
	err := store.pool.QueryRow(ctx, sandboxQualificationOrderCommandTargetSQL, orderID).Scan(
		&target.accountID, &target.epoch, &target.clientOrderID, &target.updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return target, console.ErrNotFound
	}
	return target, err
}

// QueueSandboxAccountReconciliation remains available in every account state.
func (store *OwnerConsoleStore) QueueSandboxAccountReconciliation(
	ctx context.Context,
	principal authentication.Principal,
	accountID, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.reconcile", "sandbox_account",
		accountID, body.Reason, expected,
		map[string]any{"account_id": accountID, "body": body},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	var epoch, revision int64
	err = store.pool.QueryRow(ctx, `
SELECT current_epoch,revision FROM sandbox_runtime_exchange_accounts WHERE id=$1`,
		accountID,
	).Scan(&epoch, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_account_not_found")
		return generated.CommandAccepted{}, console.ErrNotFound
	}
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if revision != *expected {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_account_revision_conflict")
		return generated.CommandAccepted{}, console.ErrConflict
	}
	if err = store.sandbox_runtime.QueueEngineCommand(ctx, sandbox.EngineCommand{
		ID: pending.accepted.Id, AccountID: sandbox.AccountID(accountID),
		AccountEpoch: uint64(epoch), Kind: sandbox.EngineCommandReconcile,
		State: sandbox.EngineCommandPending, RequestedAt: pending.accepted.CreatedAt,
	}); err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_reconcile_rejected")
		return generated.CommandAccepted{}, sandboxQualificationConsoleError(err)
	}
	return store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.reconcile", accountID,
		map[string]any{"account_id": accountID, "state": "PENDING"},
	)
}

func sandboxQualificationResultMap(value any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	if err = json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func sandboxQualificationStableIdentifier(prefix, value string) string {
	hash := stableSandboxRuntimeHash(prefix, value)
	return prefix + "-" + hash[:32]
}

func sandboxQualificationConsoleError(err error) error {
	value := err.Error()
	switch {
	case strings.Contains(value, "cap"):
		return console.ErrQuota
	case strings.Contains(value, "rejected"),
		strings.Contains(value, "blocked"),
		strings.Contains(value, "unavailable"):
		return console.ErrPrecondition
	default:
		return console.ErrUnavailable
	}
}

var _ console.SandboxCommandService = (*OwnerConsoleStore)(nil)
