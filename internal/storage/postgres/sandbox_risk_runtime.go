package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/alerting"
	"axiom/internal/domain"
	"axiom/internal/risk"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SandboxRiskRuntime restores the global central-risk posture for each
// automatic evaluation and persists every escalation under one serialized
// database revision. It owns no exchange adapter or credential.
type SandboxRiskRuntime struct {
	pool   *pgxpool.Pool
	clock  domain.Clock
	alerts *AlertStore
}

// NewSandboxRiskRuntime creates the durable central-risk boundary used by an
// authenticated sandbox engine. The clock is used only for bounded alerts;
// state transitions retain the evaluator's exact UTC timestamp.
func NewSandboxRiskRuntime(pool *pgxpool.Pool, clock domain.Clock) (*SandboxRiskRuntime, error) {
	if pool == nil || clock == nil {
		return nil, fmt.Errorf("sandbox_risk_runtime_invalid")
	}
	alerts, err := NewAlertStore(pool)
	if err != nil {
		return nil, fmt.Errorf("sandbox_risk_runtime_invalid")
	}
	return &SandboxRiskRuntime{pool: pool, clock: clock, alerts: alerts}, nil
}

// SandboxStrategyRiskEngine restores the exact current durable posture. A
// missing event means PAUSED, but an inconsistent revision never receives a
// guessed state.
func (runtime *SandboxRiskRuntime) SandboxStrategyRiskEngine(
	ctx context.Context,
	now time.Time,
) (*risk.Engine, error) {
	if runtime == nil || runtime.pool == nil || runtime.clock == nil || ctx == nil ||
		now.IsZero() || now.Location() != time.UTC {
		return nil, fmt.Errorf("sandbox_risk_runtime_invalid")
	}
	var revision int64
	if err := runtime.pool.QueryRow(ctx, `
SELECT revision FROM api_entity_revisions
WHERE entity_type='risk' AND entity_id='global'`).Scan(&revision); err != nil || revision <= 0 {
		return nil, fmt.Errorf("sandbox_risk_state_unavailable")
	}
	state, eventRevision, found, err := latestSandboxRiskState(ctx, runtime.pool)
	if err != nil || (found && eventRevision != revision) || (!found && revision != 1) {
		return nil, fmt.Errorf("sandbox_risk_state_inconsistent")
	}
	if !found {
		state = risk.StatePaused
	}
	engine, err := risk.NewRestoredEngine(state, runtime, runtime)
	if err != nil {
		return nil, fmt.Errorf("sandbox_risk_state_invalid")
	}
	return engine, nil
}

// Append is the risk engine's immutable transition audit sink. Locking the
// shared entity revision makes a stale process fail rather than overwrite a
// newer posture from another sandbox engine or the owner console.
func (runtime *SandboxRiskRuntime) Append(event risk.AuditEvent) error {
	if runtime == nil || runtime.pool == nil || !validSandboxRiskAuditEvent(event) {
		return fmt.Errorf("sandbox_risk_transition_invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := runtime.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("sandbox_risk_transition_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var revision int64
	if err = tx.QueryRow(ctx, `
SELECT revision FROM api_entity_revisions
WHERE entity_type='risk' AND entity_id='global'
FOR UPDATE`).Scan(&revision); err != nil || revision <= 0 {
		return fmt.Errorf("sandbox_risk_transition_revision_unavailable")
	}
	if err = validateSandboxRiskTransition(ctx, tx, revision, event); err != nil {
		return err
	}
	evidenceHash := sandboxRiskTransitionHash(event)
	nextRevision := revision + 1
	id := fmt.Sprintf("sandbox-risk-state-%d-%s", nextRevision, evidenceHash[:16])
	if _, err = tx.Exec(ctx, `
INSERT INTO risk_state_events(
 id,prior_state,next_state,reason_code,actor,evidence_hash,occurred_at,entity_revision
) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, event.Prior, event.Next, event.ReasonCode, event.Actor,
		evidenceHash, event.At, nextRevision); err != nil {
		return fmt.Errorf("sandbox_risk_transition_insert_failed")
	}
	tag, err := tx.Exec(ctx, `
UPDATE api_entity_revisions
SET revision=$1,updated_at=$2
WHERE entity_type='risk' AND entity_id='global' AND revision=$3`,
		nextRevision, event.At, revision)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_risk_transition_revision_conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_risk_transition_commit_failed")
	}
	return nil
}

func validateSandboxRiskTransition(ctx context.Context, tx pgx.Tx, revision int64,
	event risk.AuditEvent,
) error {
	current, eventRevision, occurredAt, found, err := latestSandboxRiskStateTx(ctx, tx)
	if err != nil || (found && eventRevision != revision) || (!found && revision != 1) {
		return fmt.Errorf("sandbox_risk_transition_state_inconsistent")
	}
	if !found {
		current = risk.StatePaused
	}
	if current != event.Prior || (found && event.At.Before(occurredAt)) {
		return fmt.Errorf("sandbox_risk_transition_stale")
	}
	return nil
}

// Emit maintains one bounded active alert for each stable central-risk reason,
// action, and state. A resolved condition may reopen; repeated evaluations do
// not create an unbounded alert stream.
func (runtime *SandboxRiskRuntime) Emit(reason string, action risk.Action, state risk.State) error {
	if runtime == nil || runtime.pool == nil || runtime.clock == nil || runtime.alerts == nil ||
		!validSandboxRiskReason(reason) || !validSandboxRiskAction(action) || !validSandboxRiskState(state) {
		return fmt.Errorf("sandbox_risk_alert_invalid")
	}
	now := runtime.clock.Now()
	if now.Validate() != nil {
		return fmt.Errorf("sandbox_risk_alert_invalid")
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{reason, string(action), string(state)}, "\x00")))
	id := "sandbox-risk-alert-" + hex.EncodeToString(digest[:16])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	severity := alerting.SeverityWarning
	if state == risk.StateLocked || action == risk.ActionLockEngine || action == risk.ActionQuarantine {
		severity = alerting.SeverityCritical
	}
	if _, err := runtime.alerts.Upsert(ctx, alerting.Alert{
		ID:               id,
		DeduplicationKey: hex.EncodeToString(digest[:]),
		Severity:         severity,
		Reason:           alerting.Reason(reason),
		Component:        "central-risk",
		CorrelationID:    id,
		CreatedAt:        now.UTC,
		LastSeenAt:       now.UTC,
	}); err != nil {
		return fmt.Errorf("sandbox_risk_alert_persist_failed")
	}
	return nil
}

type sandboxRiskStateQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func latestSandboxRiskState(
	ctx context.Context,
	pool *pgxpool.Pool,
) (risk.State, int64, bool, error) {
	state, revision, _, found, err := latestSandboxRiskStateQuery(ctx, pool)
	return state, revision, found, err
}

func latestSandboxRiskStateTx(
	ctx context.Context,
	tx pgx.Tx,
) (risk.State, int64, time.Time, bool, error) {
	return latestSandboxRiskStateQuery(ctx, tx)
}

func latestSandboxRiskStateQuery(
	ctx context.Context,
	query sandboxRiskStateQuery,
) (risk.State, int64, time.Time, bool, error) {
	var raw string
	var revision int64
	var occurredAt time.Time
	err := query.QueryRow(ctx, `
SELECT next_state,entity_revision,occurred_at
FROM risk_state_events
ORDER BY entity_revision DESC
LIMIT 1`).Scan(&raw, &revision, &occurredAt)
	if err == pgx.ErrNoRows {
		return "", 0, time.Time{}, false, nil
	}
	occurredAt = occurredAt.UTC()
	state := risk.State(raw)
	if err != nil || revision <= 0 || occurredAt.IsZero() ||
		!validSandboxRiskState(state) {
		return "", 0, time.Time{}, false, fmt.Errorf("sandbox_risk_state_invalid")
	}
	return state, revision, occurredAt, true, nil
}

func validSandboxRiskAuditEvent(event risk.AuditEvent) bool {
	return event.Type == "risk_state_transition" && validSandboxRiskReason(event.ReasonCode) &&
		event.Actor != "" && len(event.Actor) <= 160 && validSandboxRiskState(event.Prior) &&
		validSandboxRiskState(event.Next) && event.Prior != event.Next && !event.At.IsZero() &&
		event.At.Location() == time.UTC
}

func validSandboxRiskReason(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func validSandboxRiskState(state risk.State) bool {
	return state == risk.StateNormal || state == risk.StateCautious ||
		state == risk.StatePaused || state == risk.StateLocked
}

func validSandboxRiskAction(action risk.Action) bool {
	return action == risk.ActionApprove || action == risk.ActionReject ||
		action == risk.ActionPauseStrategy || action == risk.ActionPauseInstrument ||
		action == risk.ActionPauseExchange || action == risk.ActionLockEngine ||
		action == risk.ActionQuarantine
}

func sandboxRiskTransitionHash(event risk.AuditEvent) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		event.Type,
		event.ReasonCode,
		string(event.Prior),
		string(event.Next),
		event.Actor,
		event.At.Format(time.RFC3339Nano),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

var _ risk.AuditSink = (*SandboxRiskRuntime)(nil)
var _ risk.AlertSink = (*SandboxRiskRuntime)(nil)
