package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

const (
	sandboxRuntimeHighRiskAuditLockSQL = `
SELECT pg_advisory_xact_lock(hashtext('axiom:sandbox_runtime:high-risk-audit'))`
	sandboxRuntimePreviousHighRiskAuditHashSQL = `
SELECT event_hash FROM sandbox_runtime_high_risk_audit_events
ORDER BY chain_sequence DESC LIMIT 1`
)

// AppendHighRiskAudit serializes and appends one hash-linked audit event.
func (store *SandboxRuntimeAuthenticationStore) AppendHighRiskAudit(
	ctx context.Context,
	audit authentication.HighRiskAudit,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("sandbox_runtime_audit_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = appendHighRiskAudit(ctx, tx, audit); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_audit_commit_failed")
	}
	return nil
}

func appendHighRiskAudit(
	ctx context.Context,
	tx pgx.Tx,
	audit authentication.HighRiskAudit,
) error {
	if _, err := tx.Exec(ctx, sandboxRuntimeHighRiskAuditLockSQL); err != nil {
		return fmt.Errorf("sandbox_runtime_audit_chain_failed")
	}
	var previous *string
	err := tx.QueryRow(ctx, sandboxRuntimePreviousHighRiskAuditHashSQL).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("sandbox_runtime_audit_chain_failed")
	}
	previousValue := ""
	if previous != nil {
		previousValue = *previous
	}
	eventHash := sandboxRuntimeAuditHash(previousValue, audit)
	var before, after, prior, targetRevision any
	if audit.BeforeHash != "" {
		before = audit.BeforeHash
	}
	if audit.AfterHash != "" {
		after = audit.AfterHash
	}
	if previousValue != "" {
		prior = previousValue
	}
	if audit.TargetRevision != nil {
		targetRevision = *audit.TargetRevision
	}
	_, err = tx.Exec(ctx, `
INSERT INTO sandbox_runtime_high_risk_audit_events(
  id,actor_user_id,session_id,purpose,outcome,source_hash,reason_hash,revision,
  target_revision,before_hash,after_hash,previous_hash,event_hash,occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		audit.ID, audit.ActorUserID, audit.SessionID, audit.Purpose, audit.Outcome,
		audit.SourceHash, audit.ReasonHash, audit.Revision, targetRevision,
		before, after, prior, eventHash, audit.OccurredAt)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_audit_insert_failed")
	}
	return nil
}

func sandboxRuntimeAuditHash(previous string, audit authentication.HighRiskAudit) string {
	targetRevision := ""
	if audit.TargetRevision != nil {
		targetRevision = strconv.FormatInt(*audit.TargetRevision, 10)
	}
	values := []string{
		previous, audit.ID, audit.ActorUserID, audit.SessionID, string(audit.Purpose),
		audit.Outcome, audit.SourceHash, audit.ReasonHash, fmt.Sprint(audit.Revision),
		targetRevision,
		audit.BeforeHash, audit.AfterHash, audit.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(hash[:])
}

var _ authentication.SandboxAuthorizationStore = (*SandboxRuntimeAuthenticationStore)(nil)
