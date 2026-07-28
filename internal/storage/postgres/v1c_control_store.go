package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// CreateSandboxArm atomically consumes only already-authorized DB evidence,
// verifies the exact current account membership, enters ARMED, and appends the
// high-risk result audit.
func (store *V1CDispatcherStore) CreateSandboxArm(
	ctx context.Context,
	command sandbox.ArmCommand,
) (sandbox.Arm, error) {
	if err := command.Validate(); err != nil {
		return sandbox.Arm{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.Arm{}, fmt.Errorf("v1c_arm_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	state, revision, err := lockV1CArmSession(ctx, tx, command)
	if err != nil {
		return sandbox.Arm{}, err
	}
	accounts, err := lockV1CArmAccounts(ctx, tx, command.Arm.SessionID)
	if err != nil || !sameV1CAccounts(accounts, command.Arm.AccountIDs) {
		return sandbox.Arm{}, fmt.Errorf("v1c_arm_accounts_rejected")
	}
	beforeHash := v1CControlStateHash(
		string(command.Arm.SessionID),
		state,
		accounts,
		uint64(revision),
	)
	if err = insertV1CArm(ctx, tx, command); err != nil {
		return sandbox.Arm{}, err
	}
	if err = setV1CArmedState(ctx, tx, command, accounts); err != nil {
		return sandbox.Arm{}, err
	}
	afterHash := v1CControlStateHash(
		string(command.Arm.SessionID), "ARMED", accounts, uint64(revision+1),
	)
	if err = appendV1CArmAudit(
		ctx, tx, command, revision+1, beforeHash, afterHash,
	); err != nil {
		return sandbox.Arm{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.Arm{}, fmt.Errorf("v1c_arm_commit_failed")
	}
	return command.Arm, nil
}

func lockV1CArmSession(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.ArmCommand,
) (string, int64, error) {
	var state, createdBy string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT state,revision,created_by FROM v1c_sandbox_sessions
WHERE id=$1 AND revision=$2 FOR UPDATE`,
		command.Arm.SessionID, command.ExpectedSessionRevision,
	).Scan(&state, &revision, &createdBy)
	if err != nil || (state != "READY_PAUSED" && state != "PAUSED") ||
		createdBy != command.Arm.ActorUserID {
		return "", 0, fmt.Errorf("v1c_arm_session_rejected")
	}
	return state, revision, nil
}

func insertV1CArm(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.ArmCommand,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		command.Arm.ID,
		command.Arm.SessionID,
		command.AuthorizationID,
		command.Arm.ActorUserID,
		command.Arm.ActorSessionID,
		command.SourceHash,
		command.Arm.ReasonHash,
		command.Arm.CreatedAt,
		command.Arm.ExpiresAt,
		command.Arm.Revision,
	); err != nil {
		return fmt.Errorf("v1c_arm_insert_failed")
	}
	return nil
}

func setV1CArmedState(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.ArmCommand,
	accounts []sandbox.AccountID,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_sessions
SET state='ARMED',revision=revision+1,updated_at=$2
WHERE id=$1`, command.Arm.SessionID, command.Arm.CreatedAt); err != nil {
		return fmt.Errorf("v1c_arm_session_update_failed")
	}
	accountValues := make([]string, len(accounts))
	for index, account := range accounts {
		accountValues[index] = string(account)
	}
	tag, err := tx.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='ARMED',revision=revision+1,updated_at=$2
WHERE id=ANY($1) AND state='READY_PAUSED'`,
		accountValues,
		command.Arm.CreatedAt,
	)
	if err != nil || tag.RowsAffected() != int64(len(accounts)) {
		return fmt.Errorf("v1c_arm_account_update_failed")
	}
	return nil
}

func appendV1CArmAudit(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.ArmCommand,
	revision int64,
	beforeHash, afterHash string,
) error {
	return appendHighRiskAudit(ctx, tx, authentication.HighRiskAudit{
		ID:          command.Arm.ID + "-result-audit",
		ActorUserID: command.Arm.ActorUserID,
		SessionID:   command.Arm.ActorSessionID,
		Purpose:     authentication.PurposeSandboxArm,
		Outcome:     "sandbox_armed",
		SourceHash:  command.SourceHash,
		ReasonHash:  command.Arm.ReasonHash,
		Revision:    revision + 1,
		BeforeHash:  beforeHash,
		AfterHash:   afterHash,
		OccurredAt:  command.Arm.CreatedAt,
	})
}

func lockV1CArmAccounts(
	ctx context.Context,
	tx pgx.Tx,
	session sandbox.SessionID,
) ([]sandbox.AccountID, error) {
	rows, err := tx.Query(ctx, `
SELECT account.id
FROM v1c_sandbox_session_accounts membership
JOIN v1c_exchange_accounts account ON account.id=membership.account_id
WHERE membership.session_id=$1
  AND membership.account_epoch=account.current_epoch
  AND account.state='READY_PAUSED'
ORDER BY account.id
FOR UPDATE OF account`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]sandbox.AccountID, 0)
	for rows.Next() {
		var account sandbox.AccountID
		if err = rows.Scan(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func sameV1CAccounts(left, right []sandbox.AccountID) bool {
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	right = append([]sandbox.AccountID(nil), right...)
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// RiskUnlock returns one reconciled locked/quarantined account to
// READY_PAUSED. It never arms entry and cannot use another account epoch's
// reconciliation.
func (store *V1CDispatcherStore) RiskUnlock(
	ctx context.Context,
	command sandbox.RiskUnlockCommand,
) error {
	if err := command.Validate(); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_risk_unlock_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	state, revision, err := lockV1CRiskUnlockAccount(ctx, tx, command)
	if err != nil {
		return err
	}
	if err = requireV1CCleanReconciliation(ctx, tx, command); err != nil {
		return err
	}
	if err = insertV1CRiskUnlock(ctx, tx, command, state); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='READY_PAUSED',revision=revision+1,updated_at=$2
WHERE id=$1`, command.AccountID, command.Now); err != nil {
		return fmt.Errorf("v1c_risk_unlock_update_failed")
	}
	if err = appendV1CRiskUnlockAudit(ctx, tx, command, state, revision); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_risk_unlock_commit_failed")
	}
	return nil
}

func lockV1CRiskUnlockAccount(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.RiskUnlockCommand,
) (string, int64, error) {
	var state string
	var revision, epoch int64
	err := tx.QueryRow(ctx, `
SELECT state,revision,current_epoch FROM v1c_exchange_accounts
WHERE id=$1 AND revision=$2 FOR UPDATE`,
		command.AccountID, command.ExpectedRevision,
	).Scan(&state, &revision, &epoch)
	if err != nil || (state != "LOCKED" && state != "QUARANTINED") ||
		uint64(epoch) != command.ReconciliationEpoch {
		return "", 0, fmt.Errorf("v1c_risk_unlock_account_rejected")
	}
	return state, revision, nil
}

func requireV1CCleanReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.RiskUnlockCommand,
) error {
	var clean bool
	err := tx.QueryRow(ctx, `
SELECT state='clean' FROM v1c_reconciliations
WHERE id=$1 AND account_id=$2 AND account_epoch=$3 FOR SHARE`,
		command.ReconciliationID, command.AccountID, command.ReconciliationEpoch,
	).Scan(&clean)
	if err != nil || !clean {
		return fmt.Errorf("v1c_risk_unlock_reconciliation_rejected")
	}
	return nil
}

func insertV1CRiskUnlock(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.RiskUnlockCommand,
	priorState string,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_risk_unlocks(
 id,account_id,account_epoch,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,reconciliation_id,prior_state,resulting_state,
 unlocked_at,revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'READY_PAUSED',$11,1)`,
		command.ID,
		command.AccountID,
		command.ReconciliationEpoch,
		command.AuthorizationID,
		command.ActorUserID,
		command.ActorSessionID,
		command.SourceHash,
		command.ReasonHash,
		command.ReconciliationID,
		priorState,
		command.Now,
	); err != nil {
		return fmt.Errorf("v1c_risk_unlock_insert_failed")
	}
	return nil
}

func appendV1CRiskUnlockAudit(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.RiskUnlockCommand,
	priorState string,
	revision int64,
) error {
	beforeHash := v1CControlStateHash(
		string(command.AccountID),
		priorState,
		[]sandbox.AccountID{command.AccountID},
		uint64(revision),
	)
	afterHash := v1CControlStateHash(
		string(command.AccountID),
		"READY_PAUSED",
		[]sandbox.AccountID{command.AccountID},
		uint64(revision+1),
	)
	return appendHighRiskAudit(ctx, tx, authentication.HighRiskAudit{
		ID:          command.ID + "-result-audit",
		ActorUserID: command.ActorUserID,
		SessionID:   command.ActorSessionID,
		Purpose:     authentication.PurposeRiskUnlock,
		Outcome:     "risk_unlocked_ready_paused",
		SourceHash:  command.SourceHash,
		ReasonHash:  command.ReasonHash,
		Revision:    revision + 1,
		BeforeHash:  beforeHash,
		AfterHash:   afterHash,
		OccurredAt:  command.Now,
	})
}

func v1CControlStateHash(
	identity, state string,
	accounts []sandbox.AccountID,
	revision uint64,
) string {
	values := make([]string, len(accounts))
	for index, account := range accounts {
		values[index] = string(account)
	}
	sort.Strings(values)
	return stableV1CHash(
		identity,
		state,
		strings.Join(values, ","),
		fmt.Sprint(revision),
	)
}

func stableV1CHash(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

var _ sandbox.SandboxControlStore = (*V1CDispatcherStore)(nil)
