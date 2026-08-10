package postgres

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const sandboxRuntimeEngineCommandClaimTTL = 30 * time.Second

// QueueEngineCommand appends an idempotent query, cancel, or reconcile request.
func (store *SandboxRuntimeDispatcherStore) QueueEngineCommand(
	ctx context.Context,
	command sandbox.EngineCommand,
) error {
	if command.State == "" {
		command.State = sandbox.EngineCommandPending
	}
	if command.Validate() != nil {
		return fmt.Errorf("sandbox_runtime_engine_command_invalid")
	}
	tag, err := store.pool.Exec(ctx, `
INSERT INTO sandbox_runtime_engine_commands(
 id,account_id,account_epoch,kind,client_order_id,state,requested_at
) VALUES ($1,$2,$3,$4,$5,'PENDING',$6)
ON CONFLICT (id) DO NOTHING`,
		command.ID,
		command.AccountID,
		command.AccountEpoch,
		command.Kind,
		nullableText(command.ClientOrderID),
		command.RequestedAt,
	)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_engine_command_insert_failed")
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	err = store.pool.QueryRow(ctx, `
SELECT account_id=$2 AND account_epoch=$3 AND kind=$4
       AND client_order_id IS NOT DISTINCT FROM $5
       AND requested_at=$6
FROM sandbox_runtime_engine_commands
WHERE id=$1`,
		command.ID,
		command.AccountID,
		command.AccountEpoch,
		command.Kind,
		nullableText(command.ClientOrderID),
		command.RequestedAt,
	).Scan(&same)
	if err != nil || !same {
		return fmt.Errorf("sandbox_runtime_engine_command_identity_conflict")
	}
	return nil
}

// ClaimEngineCommands fences a bounded page to the only live account owner.
func (store *SandboxRuntimeDispatcherStore) ClaimEngineCommands(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]sandbox.EngineCommand, error) {
	if account == "" || epoch == 0 || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC ||
		limit < 1 || limit > 16 {
		return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_invalid")
	}
	tx, err := store.pool.BeginTx(
		ctx,
		pgx.TxOptions{IsoLevel: pgx.ReadCommitted},
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(
		ctx, tx, account, owner, fence, now,
	); err != nil {
		return nil, err
	}
	rows, err := claimEngineCommandRows(
		ctx, tx, account, epoch, owner, fence, now, limit,
	)
	if err != nil {
		return nil, err
	}
	commands, err := scanClaimedEngineCommands(rows, limit)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_commit_failed")
	}
	return commands, nil
}

func validateEngineCommandLease(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	owner string,
	fence uint64,
	now time.Time,
) error {
	var leaseValid bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM sandbox_runtime_account_leases
  WHERE account_id=$1 AND owner=$2 AND fencing_token=$3
    AND expires_at>$4
)`,
		account,
		owner,
		fence,
		now,
	).Scan(&leaseValid); err != nil || !leaseValid {
		return fmt.Errorf("sandbox_runtime_engine_command_claim_fence_invalid")
	}
	return nil
}

func claimEngineCommandRows(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) (pgx.Rows, error) {
	rows, err := tx.Query(ctx, `
WITH candidates AS (
  SELECT id
  FROM sandbox_runtime_engine_commands
  WHERE account_id=$1 AND account_epoch=$2
    AND (
      state='PENDING' OR
      (state='CLAIMED' AND claim_expires_at<=$4)
    )
  ORDER BY requested_at,id
  FOR UPDATE SKIP LOCKED
  LIMIT $3
)
UPDATE sandbox_runtime_engine_commands command
SET state='CLAIMED',claim_owner=$5,fencing_token=$6,
    claimed_at=$4,claim_expires_at=$7
FROM candidates
WHERE command.id=candidates.id
RETURNING command.id,command.account_id,command.account_epoch,
          command.kind,command.client_order_id,command.requested_at`,
		account,
		epoch,
		limit,
		now,
		owner,
		fence,
		now.Add(sandboxRuntimeEngineCommandClaimTTL),
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_failed")
	}
	return rows, nil
}

func scanClaimedEngineCommands(
	rows pgx.Rows,
	limit int,
) ([]sandbox.EngineCommand, error) {
	defer rows.Close()
	commands := make([]sandbox.EngineCommand, 0, limit)
	for rows.Next() {
		var command sandbox.EngineCommand
		var clientOrderID *string
		if err := rows.Scan(
			&command.ID,
			&command.AccountID,
			&command.AccountEpoch,
			&command.Kind,
			&clientOrderID,
			&command.RequestedAt,
		); err != nil {
			return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_failed")
		}
		command.ClientOrderID = valueOrEmpty(clientOrderID)
		command.State = sandbox.EngineCommandClaimed
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox_runtime_engine_command_claim_failed")
	}
	return commands, nil
}

// CompleteEngineCommand records only a redacted evidence hash.
func (store *SandboxRuntimeDispatcherStore) CompleteEngineCommand(
	ctx context.Context,
	id string,
	fence uint64,
	succeeded bool,
	evidenceHash string,
	now time.Time,
) error {
	if id == "" || fence == 0 || len(evidenceHash) != 64 ||
		now.IsZero() || now.Location() != time.UTC {
		return fmt.Errorf("sandbox_runtime_engine_command_complete_invalid")
	}
	if _, err := hex.DecodeString(evidenceHash); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_command_complete_invalid")
	}
	state := sandbox.EngineCommandFailed
	if succeeded {
		state = sandbox.EngineCommandSucceeded
	}
	tag, err := store.pool.Exec(ctx, `
UPDATE sandbox_runtime_engine_commands command
SET state=$3,completed_at=$4,evidence_hash=$5
WHERE command.id=$1
  AND command.state='CLAIMED'
  AND command.fencing_token=$2
  AND EXISTS(
    SELECT 1 FROM sandbox_runtime_account_leases lease
    WHERE lease.account_id=command.account_id
      AND lease.fencing_token=$2
      AND lease.expires_at>$4
  )`,
		id,
		fence,
		state,
		now,
		evidenceHash,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_engine_command_complete_failed")
	}
	return nil
}
