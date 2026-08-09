package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/console"

	"github.com/jackc/pgx/v5"
)

func ownerControlExportRecord(
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
		err = ownerControlExportActivityRecord(ctx, tx, id, expectedRevision, record)
	case "report":
		err = operationalEvidenceExportReportRecord(ctx, tx, id, expectedRevision, record)
	case "lab_run":
		err = ownerControlExportJobRecord(ctx, tx, id, expectedRevision, record)
	case "incident":
		err = ownerControlExportIncidentRecord(ctx, tx, id, expectedRevision, record)
	case "audit":
		err = ownerControlExportAuditRecord(ctx, tx, id, expectedRevision, record)
	case "qualification":
		err = ownerControlExportQualificationRecord(ctx, tx, id, expectedRevision, record)
	default:
		return nil, console.ErrInvalidRequest
	}
	return record, err
}

func ownerControlExportActivityRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var sourceRevision string
	var view, sourceType, sourceID, outcome, reasonCode, reasonSummary, severity, correlationID string
	var occurredAt time.Time
	err := tx.QueryRow(ctx, `SELECT activity_revision,view_kind,source_type,source_id,
source_revision,outcome,reason_code,reason_summary,severity,correlation_id,occurred_at
FROM owner_console_activity_explanations WHERE id=$1`, id).Scan(&revision, &view, &sourceType,
		&sourceID, &sourceRevision, &outcome, &reasonCode, &reasonSummary, &severity,
		&correlationID, &occurredAt)
	if err != nil {
		return ownerControlNotFound(err)
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

func ownerControlExportJobRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var kind, state, failureCode, runID, inputHash, requestPayload, resultHash string
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT progress_revision,job_type,state,created_at,updated_at,
coalesce(failure_code,''),coalesce(run_id,''),payload_hash::text,
coalesce(request_payload::text,''),coalesce(result_payload->>'result_hash','')
FROM jobs WHERE id=$1`, id).Scan(
		&revision, &kind, &state, &createdAt, &updatedAt, &failureCode, &runID,
		&inputHash, &requestPayload, &resultHash,
	)
	if err != nil {
		return ownerControlNotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	record["kind"], record["state"], record["failure_code"] = kind, state, failureCode
	record["input_hash"], record["run_id"], record["result_hash"] = inputHash, runID, resultHash
	record["created_at"] = createdAt.UTC().Format(time.RFC3339Nano)
	record["updated_at"] = updatedAt.UTC().Format(time.RFC3339Nano)
	if kind != "backtest" && kind != "replay" {
		return nil
	}
	return ownerControlExportOfflineJobRecord(ctx, tx, kind, runID, requestPayload, record)
}

func ownerControlExportOfflineJobRecord(
	ctx context.Context, tx pgx.Tx, kind, runID, requestPayload string, record map[string]string,
) error {
	request, err := decodeOwnerConsoleOfflineRequest(kind, json.RawMessage(requestPayload))
	if err != nil {
		return err
	}
	record["configuration_id"] = request.ConfigurationID
	record["dataset_id"] = request.DatasetID
	record["research_generation_id"] = request.ResearchGenerationID
	record["strategy_version"] = request.StrategyVersion
	record["root_seed_hash"] = request.RootSeedHash
	if request.Speed != nil {
		record["speed"] = *request.Speed
	}
	if request.IncidentID != nil {
		record["incident_id"] = *request.IncidentID
	}
	if request.FirstOrdinal != nil {
		record["first_ordinal"], record["last_ordinal"] = *request.FirstOrdinal, *request.LastOrdinal
	}
	if runID == "" {
		return nil
	}
	return ownerControlExportRunManifest(ctx, tx, runID, record)
}

func ownerControlExportRunManifest(ctx context.Context, tx pgx.Tx, runID string, record map[string]string) error {
	var datasetRevision int64
	var manifestHash, codeCommit, datasetManifestHash, sourceCommit, configurationHash string
	var modelNamespaceID, startingBalanceHash, confidenceTier string
	err := tx.QueryRow(ctx, `SELECT manifest_hash::text,code_commit,dataset_manifest_hash::text,
dataset_revision,source_commit,configuration_hash::text,model_namespace_id,
starting_balance_hash::text,confidence_tier FROM run_manifests WHERE run_id=$1`, runID).Scan(
		&manifestHash, &codeCommit, &datasetManifestHash, &datasetRevision, &sourceCommit,
		&configurationHash, &modelNamespaceID, &startingBalanceHash, &confidenceTier,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	record["manifest_hash"], record["code_commit"] = manifestHash, codeCommit
	record["dataset_manifest_hash"], record["source_commit"] = datasetManifestHash, sourceCommit
	record["configuration_hash"], record["model_namespace_id"] = configurationHash, modelNamespaceID
	record["starting_balance_hash"], record["confidence_tier"] = startingBalanceHash, confidenceTier
	record["dataset_revision"] = strconv.FormatInt(datasetRevision, 10)
	return nil
}

func ownerControlExportIncidentRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var severity, state, reasonCode, owner string
	var revision int64
	var openedAt time.Time
	var resolvedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT severity,state,reason_code,coalesce(owner_user_id,''),
revision,opened_at,resolved_at FROM incidents WHERE id=$1`, id).Scan(
		&severity, &state, &reasonCode, &owner, &revision, &openedAt, &resolvedAt)
	if err != nil {
		return ownerControlNotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	record["severity"], record["state"], record["reason_code"] = severity, state, reasonCode
	record["owner_user_id"], record["revision"] = owner, strconv.FormatInt(revision, 10)
	record["opened_at"] = openedAt.UTC().Format(time.RFC3339Nano)
	if resolvedAt != nil {
		record["resolved_at"] = resolvedAt.UTC().Format(time.RFC3339Nano)
	}
	return operationalEvidenceExportIncidentEvidence(ctx, tx, id, record)
}

func ownerControlExportAuditRecord(
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
		return ownerControlNotFound(err)
	}
	record["event_type"], record["actor"] = eventType, actor
	record["causation_id"], record["correlation_id"] = causationID, correlationID
	record["recorded_at"] = recordedAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func ownerControlExportQualificationRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision int64
	var qualificationID, state, sourceSHA, configurationHash, imageDigest, serverIdentity string
	var createdAt, updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT qualification_id,state,revision,source_sha,
configuration_hash,coalesce(image_digest,''),coalesce(server_identity,''),created_at,updated_at
FROM owner_console_qualification_runs WHERE id=$1`, id).Scan(&qualificationID, &state, &revision,
		&sourceSHA, &configurationHash, &imageDigest, &serverIdentity, &createdAt, &updatedAt)
	if err != nil {
		return ownerControlNotFound(err)
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

func encodeOwnerControlExport(record map[string]string, format string) (string, string, error) {
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
			values[index] = safeOwnerControlSpreadsheetValue(record[key])
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

func safeOwnerControlSpreadsheetValue(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

func ownerControlNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return console.ErrNotFound
	}
	return err
}

func optionalOwnerControlString(value any) *string {
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}

var _ console.OwnerControlCommandService = (*OwnerConsoleStore)(nil)
