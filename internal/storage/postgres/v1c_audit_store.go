package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

const (
	v1cHighRiskAuditLockSQL = `
SELECT pg_advisory_xact_lock(hashtext('axiom:v1c:high-risk-audit'))`
	v1cPreviousHighRiskAuditHashSQL = `
SELECT event_hash FROM v1c_high_risk_audit_events
ORDER BY chain_sequence DESC LIMIT 1`
)

// AppendHighRiskAudit serializes and appends one hash-linked audit event.
func (store *V1CAuthenticationStore) AppendHighRiskAudit(
	ctx context.Context,
	audit authentication.HighRiskAudit,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("v1c_audit_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = appendHighRiskAudit(ctx, tx, audit); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_audit_commit_failed")
	}
	return nil
}

func appendHighRiskAudit(
	ctx context.Context,
	tx pgx.Tx,
	audit authentication.HighRiskAudit,
) error {
	if _, err := tx.Exec(ctx, v1cHighRiskAuditLockSQL); err != nil {
		return fmt.Errorf("v1c_audit_chain_failed")
	}
	var previous *string
	err := tx.QueryRow(ctx, v1cPreviousHighRiskAuditHashSQL).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("v1c_audit_chain_failed")
	}
	previousValue := ""
	if previous != nil {
		previousValue = *previous
	}
	eventHash := v1cAuditHash(previousValue, audit)
	var before, after, prior any
	if audit.BeforeHash != "" {
		before = audit.BeforeHash
	}
	if audit.AfterHash != "" {
		after = audit.AfterHash
	}
	if previousValue != "" {
		prior = previousValue
	}
	_, err = tx.Exec(ctx, `
INSERT INTO v1c_high_risk_audit_events(
  id,actor_user_id,session_id,purpose,outcome,source_hash,reason_hash,revision,
  before_hash,after_hash,previous_hash,event_hash,occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		audit.ID, audit.ActorUserID, audit.SessionID, audit.Purpose, audit.Outcome,
		audit.SourceHash, audit.ReasonHash, audit.Revision, before, after, prior, eventHash, audit.OccurredAt)
	if err != nil {
		return fmt.Errorf("v1c_audit_insert_failed")
	}
	return nil
}

func v1cAuditHash(previous string, audit authentication.HighRiskAudit) string {
	values := []string{
		previous, audit.ID, audit.ActorUserID, audit.SessionID, string(audit.Purpose),
		audit.Outcome, audit.SourceHash, audit.ReasonHash, fmt.Sprint(audit.Revision),
		audit.BeforeHash, audit.AfterHash, audit.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(hash[:])
}

var _ authentication.SandboxAuthorizationStore = (*V1CAuthenticationStore)(nil)
