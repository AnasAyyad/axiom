package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"sort"

	"strings"
	"time"

	"axiom/internal/api/console"

	"github.com/jackc/pgx/v5"
)

func d1ExportRecord(
	ctx context.Context,
	tx pgx.Tx,
	resourceType, id string,
	expectedRevision int64,
) (map[string]string, error) {
	record := map[string]string{"resource_type": resourceType, "resource_id": id,
		"real_trading_enabled": "false"}
	var err error
	switch resourceType {
	case "activity":
		err = d1ExportActivityRecord(ctx, tx, id, expectedRevision, record)
	case "report", "lab_run":
		err = d1ExportJobRecord(ctx, tx, id, expectedRevision, record)
	case "incident":
		err = d1ExportIncidentRecord(ctx, tx, id, expectedRevision, record)
	case "audit":
		err = d1ExportAuditRecord(ctx, tx, id, expectedRevision, record)
	case "qualification":
		err = d1ExportQualificationRecord(ctx, tx, id, expectedRevision, record)
	default:
		return nil, console.ErrInvalidRequest
	}
	return record, err
}

func d1ExportActivityRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var sourceRevision string
	var view, sourceType, sourceID, outcome, reasonCode, reasonSummary, severity, correlationID string
	var occurredAt time.Time
	err := tx.QueryRow(ctx, `SELECT activity_revision,view_kind,source_type,source_id,
source_revision,outcome,reason_code,reason_summary,severity,correlation_id,occurred_at
FROM v1d_activity_explanations WHERE id=$1`, id).Scan(&revision, &view, &sourceType,
		&sourceID, &sourceRevision, &outcome, &reasonCode, &reasonSummary, &severity,
		&correlationID, &occurredAt)
	if err != nil {
		return d1NotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	record["view"], record["source_type"], record["source_id"] = view, sourceType, sourceID
	record["source_revision"], record["outcome"] = sourceRevision, outcome
	record["reason_code"], record["reason_summary"] = reasonCode, reasonSummary
	record["severity"], record["correlation_id"] = severity, correlationID
	record["occurred_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func d1ExportJobRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var kind, state, failureCode string
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT progress_revision,job_type,state,created_at,updated_at,
coalesce(failure_code,'') FROM jobs WHERE id=$1`, id).Scan(
		&revision, &kind, &state, &createdAt, &updatedAt, &failureCode,
	)
	if err != nil {
		return d1NotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	record["kind"], record["state"], record["failure_code"] = kind, state, failureCode
	record["created_at"] = createdAt.UTC().Format(time.RFC3339Nano)
	record["updated_at"] = updatedAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func d1ExportIncidentRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var severity, state, reasonCode string
	var openedAt time.Time
	var resolvedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT severity,state,reason_code,opened_at,resolved_at
FROM incidents WHERE id=$1`, id).Scan(&severity, &state, &reasonCode, &openedAt, &resolvedAt)
	if err != nil {
		return d1NotFound(err)
	}
	if map[string]int64{"open": 1, "acknowledged": 2, "resolved": 3}[state] != expected {
		return console.ErrConflict
	}
	record["severity"], record["state"], record["reason_code"] = severity, state, reasonCode
	record["opened_at"] = openedAt.UTC().Format(time.RFC3339Nano)
	if resolvedAt != nil {
		record["resolved_at"] = resolvedAt.UTC().Format(time.RFC3339Nano)
	}
	return nil
}

func d1ExportAuditRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	if expected != 1 {
		return console.ErrConflict
	}
	var eventType, actor, causationID, correlationID string
	var recordedAt time.Time
	err := tx.QueryRow(ctx, `SELECT event_type,actor,causation_id,correlation_id,recorded_at
FROM audit_events WHERE id=$1`, id).Scan(
		&eventType, &actor, &causationID, &correlationID, &recordedAt,
	)
	if err != nil {
		return d1NotFound(err)
	}
	record["event_type"], record["actor"] = eventType, actor
	record["causation_id"], record["correlation_id"] = causationID, correlationID
	record["recorded_at"] = recordedAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func d1ExportQualificationRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var qualificationID, state, sourceSHA, configurationHash, imageDigest, serverIdentity string
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT qualification_id,state,revision,source_sha,
configuration_hash,coalesce(image_digest,''),coalesce(server_identity,''),created_at,updated_at
FROM v1d_qualification_runs WHERE id=$1`, id).Scan(&qualificationID, &state, &revision,
		&sourceSHA, &configurationHash, &imageDigest, &serverIdentity, &createdAt, &updatedAt)
	if err != nil {
		return d1NotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	record["qualification_id"], record["state"] = qualificationID, state
	record["source_sha"], record["configuration_hash"] = sourceSHA, configurationHash
	record["image_digest"], record["server_identity"] = imageDigest, serverIdentity
	record["created_at"] = createdAt.UTC().Format(time.RFC3339Nano)
	record["updated_at"] = updatedAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func encodeD1Export(record map[string]string, format string) (string, string, error) {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	switch format {
	case "json", "jsonl":
		payload, err := json.Marshal(record)
		if format == "jsonl" {
			payload = append(payload, '\n')
			return string(payload), "application/x-ndjson", err
		}
		return string(payload), "application/json", err
	case "txt":
		var builder strings.Builder
		for _, key := range keys {
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(strings.ReplaceAll(record[key], "\n", " "))
			builder.WriteByte('\n')
		}
		return builder.String(), "text/plain", nil
	case "csv":
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		values := make([]string, len(keys))
		for index, key := range keys {
			values[index] = safeD1SpreadsheetValue(record[key])
		}
		if err := writer.Write(keys); err != nil {
			return "", "", err
		}
		if err := writer.Write(values); err != nil {
			return "", "", err
		}
		writer.Flush()
		return buffer.String(), "text/csv", writer.Error()
	default:
		return "", "", console.ErrInvalidRequest
	}
}

func safeD1SpreadsheetValue(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func d1NotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return console.ErrNotFound
	}
	return err
}

func optionalD1String(value any) *string {
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}

func dedupeD1Strings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func containsD1(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

var _ console.D1CommandService = (*A11ConsoleStore)(nil)
