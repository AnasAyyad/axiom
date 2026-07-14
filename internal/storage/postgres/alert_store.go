package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"axiom/internal/alerting"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertStore is the SQLC-backed durable in-app alert repository.
type AlertStore struct{ pool *pgxpool.Pool }

// NewAlertStore binds durable alert operations to a PostgreSQL pool.
func NewAlertStore(pool *pgxpool.Pool) (*AlertStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("alert_store_invalid")
	}
	return &AlertStore{pool: pool}, nil
}

// Upsert atomically deduplicates an alert and appends its audit event.
func (store *AlertStore) Upsert(ctx context.Context, alert alerting.Alert) (alerting.Alert, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_store_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	row, err := queries.UpsertAlert(ctx, generated.UpsertAlertParams{
		ID: alert.ID, AlertType: alert.Component, CreatedAt: timestamp(alert.LastSeenAt),
		Severity: string(alert.Severity), ReasonCode: string(alert.Reason),
		DeduplicationKey: alert.DeduplicationKey, CorrelationID: alert.CorrelationID,
	})
	if err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_store_upsert_failed")
	}
	stored, err := alertFromRow(row)
	if err != nil {
		return alerting.Alert{}, err
	}
	if err = insertAlertAudit(ctx, queries, "alert_opened", "system", stored.ID, stored.CorrelationID, stored.Revision, stored.LastSeenAt, string(stored.Reason)); err != nil {
		return alerting.Alert{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_store_commit_failed")
	}
	return stored, nil
}

// Acknowledge transactionally updates state, history, and immutable audit.
func (store *AlertStore) Acknowledge(ctx context.Context, alertID, actor, reason string, at time.Time) (alerting.Alert, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_store_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	row, err := queries.AcknowledgeAlert(ctx, generated.AcknowledgeAlertParams{ID: alertID, AcknowledgedAt: timestamp(at)})
	if err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_acknowledgement_failed")
	}
	if _, err = queries.InsertAlertAcknowledgement(ctx, generated.InsertAlertAcknowledgementParams{
		AlertID: alertID, Revision: row.Revision, Actor: actor, Reason: reason, AcknowledgedAt: timestamp(at),
	}); err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_acknowledgement_failed")
	}
	if err = insertAlertAudit(ctx, queries, "alert_acknowledged", actor, alertID, alertID, uint64(row.Revision), at, reason); err != nil {
		return alerting.Alert{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return alerting.Alert{}, fmt.Errorf("alert_acknowledgement_failed")
	}
	return alertFromRow(row)
}

// PrepareDelivery durably creates or reopens one sink delivery.
func (store *AlertStore) PrepareDelivery(ctx context.Context, alert alerting.Alert, sink string, at time.Time) (string, error) {
	hash := sha256.Sum256([]byte(alert.ID + "\x00" + sink))
	id := "alert_delivery_" + hex.EncodeToString(hash[:12])
	row, err := generated.New(store.pool).UpsertAlertDelivery(ctx, generated.UpsertAlertDeliveryParams{
		ID: id, AlertID: alert.ID, SinkName: sink, NextAttemptAt: timestamp(at),
	})
	if err != nil {
		return "", fmt.Errorf("alert_delivery_prepare_failed")
	}
	return row.ID, nil
}

// CompleteDelivery records one delivered or retryable-failed attempt.
func (store *AlertStore) CompleteDelivery(ctx context.Context, deliveryID string, delivered bool, reason string, at time.Time) error {
	state := "failed"
	next := at.Add(time.Minute)
	deliveredAt := pgtype.Timestamptz{}
	var reasonCode *string
	if delivered {
		state, next, deliveredAt = "delivered", at, timestamp(at)
	} else {
		reasonCode = &reason
	}
	_, err := generated.New(store.pool).MarkAlertDelivery(ctx, generated.MarkAlertDeliveryParams{
		ID: deliveryID, State: state, LastReasonCode: reasonCode,
		NextAttemptAt: timestamp(next), DeliveredAt: deliveredAt,
	})
	if err != nil {
		return fmt.Errorf("alert_delivery_complete_failed")
	}
	return nil
}

// DueDeliveries reconstructs bounded sanitized retry work.
func (store *AlertStore) DueDeliveries(ctx context.Context, at time.Time, limit int32) ([]alerting.PendingDelivery, error) {
	rows, err := generated.New(store.pool).ListDueAlertDeliveries(ctx, generated.ListDueAlertDeliveriesParams{
		NextAttemptAt: timestamp(at), Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("alert_delivery_list_failed")
	}
	result := make([]alerting.PendingDelivery, 0, len(rows))
	for _, row := range rows {
		if !row.AlertCreatedAt.Valid || !row.AlertLastSeenAt.Valid || row.AlertOccurrences < 1 {
			return nil, fmt.Errorf("alert_delivery_row_invalid")
		}
		result = append(result, alerting.PendingDelivery{
			ID: row.ID, SinkName: row.SinkName,
			Alert: alerting.Alert{
				ID: row.AlertID, Severity: alerting.Severity(row.Severity), Reason: alerting.Reason(row.ReasonCode),
				Component: row.AlertType, CorrelationID: row.CorrelationID,
				CreatedAt: row.AlertCreatedAt.Time.UTC(), LastSeenAt: row.AlertLastSeenAt.Time.UTC(),
				Occurrences: uint64(row.AlertOccurrences),
			},
		})
	}
	return result, nil
}

func alertFromRow(row *generated.Alert) (alerting.Alert, error) {
	dedup, ok := row.DeduplicationKey.(string)
	if !ok || !row.CreatedAt.Valid || !row.LastSeenAt.Valid || row.Occurrences < 1 || row.Revision < 1 {
		return alerting.Alert{}, fmt.Errorf("alert_store_row_invalid")
	}
	return alerting.Alert{
		ID: row.ID, DeduplicationKey: dedup, Severity: alerting.Severity(row.Severity),
		Reason: alerting.Reason(row.ReasonCode), Component: row.AlertType,
		CorrelationID: row.CorrelationID, CreatedAt: row.CreatedAt.Time.UTC(),
		LastSeenAt: row.LastSeenAt.Time.UTC(), Occurrences: uint64(row.Occurrences), Revision: uint64(row.Revision),
	}, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func insertAlertAudit(ctx context.Context, queries *generated.Queries, eventType, actor, causationID, correlationID string, revision uint64, at time.Time, detail string) error {
	canonical := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", eventType, causationID, correlationID, revision, detail)
	eventHash := sha256.Sum256([]byte(canonical))
	idHash := sha256.Sum256([]byte("audit\x00" + canonical))
	_, err := queries.InsertAuditEvent(ctx, generated.InsertAuditEventParams{
		ID: "audit_event_" + hex.EncodeToString(idHash[:12]), EventType: eventType, Actor: actor,
		CausationID: causationID, CorrelationID: correlationID,
		EventHash: hex.EncodeToString(eventHash[:]), RecordedAt: timestamp(at),
	})
	if err != nil {
		return fmt.Errorf("alert_audit_failed")
	}
	return nil
}
