package postgres

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const multiExchangeConsoleInsertFaultSQL = `INSERT INTO multi_exchange_console_replay_fault_schedules(
  id,replay_id,command_id,schedule_revision,fault_kind,event_ordinal,delay_nanos,
  repeatable,reason,actor_user_id,session_id,payload_hash,simulation_only,created_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true,$13)`

const multiExchangeConsoleInsertExportSQL = `INSERT INTO multi_exchange_console_report_exports(
  id,report_id,command_id,format,content_type,content,payload_hash,actor_user_id,
  session_id,simulation_only,created_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10)`

type multiExchangeConsoleFaultCommand struct {
	principal                authentication.Principal
	body                     generated.ReplayFaultRequest
	replayID, key, hash      string
	expected, ordinal, delay int64
}

// ScheduleReplayFault appends one deterministic fault to a queued replay.
func (store *OwnerConsoleStore) ScheduleReplayFault(
	ctx context.Context, principal authentication.Principal, replayID, key string,
	body generated.ReplayFaultRequest,
) (generated.ReplayFaultResource, error) {
	_, hash, err := ownerConsoleCommandPayload(map[string]any{"replay_id": replayID, "body": body})
	if err != nil {
		return generated.ReplayFaultResource{}, console.ErrInvalidRequest
	}
	command, err := newMultiExchangeConsoleFaultCommand(principal, replayID, key, hash, body)
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupMultiExchangeConsoleFaultCommand(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.ReplayFaultResource{}, lookupErr
	} else if found {
		return existing, commitMultiExchangeConsoleFault(ctx, tx, nil)
	}
	resource, err := store.scheduleMultiExchangeConsoleFaultTx(ctx, tx, command)
	return resource, commitMultiExchangeConsoleFault(ctx, tx, err)
}

func newMultiExchangeConsoleFaultCommand(principal authentication.Principal, replayID, key, hash string,
	body generated.ReplayFaultRequest) (multiExchangeConsoleFaultCommand, error) {
	expected, expectedErr := strconv.ParseInt(body.ExpectedRevision, 10, 64)
	ordinal, ordinalErr := strconv.ParseInt(body.Ordinal, 10, 64)
	delay, delayErr := strconv.ParseInt(body.DelayNanos, 10, 64)
	validDelay := (body.Fault == generated.Latency && delay > 0) ||
		(body.Fault != generated.Latency && delay == 0)
	if expectedErr != nil || ordinalErr != nil || delayErr != nil || expected < 0 ||
		ordinal <= 0 || delay < 0 || !body.Fault.Valid() || !validDelay ||
		len(strings.TrimSpace(body.Reason)) < 8 || len(body.Reason) > 500 {
		return multiExchangeConsoleFaultCommand{}, console.ErrInvalidRequest
	}
	return multiExchangeConsoleFaultCommand{principal: principal, replayID: replayID, key: key, hash: hash,
		body: body, expected: expected, ordinal: ordinal, delay: delay}, nil
}

func (store *OwnerConsoleStore) scheduleMultiExchangeConsoleFaultTx(
	ctx context.Context, tx pgx.Tx, command multiExchangeConsoleFaultCommand,
) (generated.ReplayFaultResource, error) {
	var err error
	now := store.clock.Now().UTC
	revision, err := lockMultiExchangeConsoleFaultRevision(ctx, tx, command.replayID, now)
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	if revision != command.expected {
		return generated.ReplayFaultResource{}, console.ErrConflict
	}
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	faultID, _ := multiExchangeConsoleIdentifier("multi_exchange_console-fault")
	if err = insertOwnerConsoleCommand(ctx, tx, commandID, command.principal, command.key, command.hash,
		"replay.fault.schedule", "replay", command.replayID, command.body.Reason, now, auditID, commandID); err != nil {
		return generated.ReplayFaultResource{}, err
	}
	repeatable := command.body.Repeatable != nil && *command.body.Repeatable
	if _, err = tx.Exec(ctx, multiExchangeConsoleInsertFaultSQL,
		faultID, command.replayID, commandID, revision+1, command.body.Fault,
		command.ordinal, command.delay, repeatable, command.body.Reason,
		command.principal.UserID, command.principal.SessionID, command.hash, now); err != nil {
		return generated.ReplayFaultResource{}, multiExchangeConsoleConstraintError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE multi_exchange_console_replay_fault_schedule_states
	  SET revision=revision+1,updated_at=$2 WHERE replay_id=$1 AND revision=$3`,
		command.replayID, now, revision); err != nil {
		return generated.ReplayFaultResource{}, err
	}
	if _, err = completeOwnerConsoleCommand(ctx, tx, commandID, auditID, command.principal,
		"replay.fault.schedule", command.replayID, command.hash, map[string]any{
			"id": faultID, "fault": command.body.Fault, "ordinal": command.ordinal,
			"simulation_only": true,
		}, now, commandID); err != nil {
		return generated.ReplayFaultResource{}, err
	}
	resource := generated.ReplayFaultResource{Id: faultID, ReplayId: command.replayID,
		Fault: command.body.Fault, Ordinal: command.body.Ordinal, DelayNanos: command.body.DelayNanos,
		Repeatable: repeatable, Revision: strconv.FormatInt(revision+1, 10),
		SimulationOnly: true, CreatedAt: now}
	return resource, nil
}

func lockMultiExchangeConsoleFaultRevision(
	ctx context.Context, tx pgx.Tx, replayID string, now time.Time,
) (int64, error) {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM jobs WHERE id=$1 AND job_type='replay' FOR UPDATE`,
		replayID).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return 0, console.ErrNotFound
	} else if err != nil {
		return 0, err
	}
	if state != "QUEUED" {
		return 0, console.ErrPrecondition
	}
	if _, err := tx.Exec(ctx, `INSERT INTO multi_exchange_console_replay_fault_schedule_states(replay_id,revision,updated_at)
	  VALUES($1,0,$2) ON CONFLICT (replay_id) DO NOTHING`, replayID, now); err != nil {
		return 0, err
	}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM multi_exchange_console_replay_fault_schedule_states
	  WHERE replay_id=$1 FOR UPDATE`, replayID).Scan(&revision)
	return revision, err
}

func lookupMultiExchangeConsoleFaultCommand(
	ctx context.Context, tx pgx.Tx, actor, key, hash string,
) (generated.ReplayFaultResource, bool, error) {
	var commandID, payloadHash string
	err := tx.QueryRow(ctx, `SELECT id,payload_hash FROM command_requests
	  WHERE actor_user_id=$1 AND idempotency_key=$2`, actor, key).Scan(&commandID, &payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ReplayFaultResource{}, false, nil
	}
	if err != nil {
		return generated.ReplayFaultResource{}, false, err
	}
	if payloadHash != hash {
		return generated.ReplayFaultResource{}, false, console.ErrIdempotencyConflict
	}
	var item generated.ReplayFaultResource
	var revision int64
	err = tx.QueryRow(ctx, `SELECT id,replay_id,fault_kind,event_ordinal,delay_nanos,
	  repeatable,schedule_revision,created_at FROM multi_exchange_console_replay_fault_schedules
	  WHERE command_id=$1`, commandID).Scan(&item.Id, &item.ReplayId, &item.Fault,
		&item.Ordinal, &item.DelayNanos, &item.Repeatable, &revision, &item.CreatedAt)
	if err != nil {
		return generated.ReplayFaultResource{}, false, err
	}
	item.Revision = strconv.FormatInt(revision, 10)
	item.SimulationOnly = true
	return item, true, nil
}

func commitMultiExchangeConsoleFault(ctx context.Context, tx pgx.Tx, prior error) error {
	if prior != nil {
		return prior
	}
	return tx.Commit(ctx)
}

// ExportReport persists a deterministic simulation-only research export.
func (store *OwnerConsoleStore) ExportReport(
	ctx context.Context, principal authentication.Principal, reportID, key string,
	body generated.ReportExportRequest,
) (generated.ReportExportResource, error) {
	if !body.Format.Valid() {
		return generated.ReportExportResource{}, console.ErrInvalidRequest
	}
	_, hash, err := ownerConsoleCommandPayload(map[string]any{"report_id": reportID, "body": body})
	if err != nil {
		return generated.ReportExportResource{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupMultiExchangeConsoleExportCommand(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.ReportExportResource{}, lookupErr
	} else if found {
		return existing, commitMultiExchangeConsoleFault(ctx, tx, nil)
	}
	resource, err := store.exportMultiExchangeConsoleReportTx(ctx, tx, principal, reportID, key, hash, body)
	return resource, commitMultiExchangeConsoleFault(ctx, tx, err)
}

func (store *OwnerConsoleStore) exportMultiExchangeConsoleReportTx(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	reportID, key, hash string, body generated.ReportExportRequest,
) (generated.ReportExportResource, error) {
	report, err := readMultiExchangeConsoleChampionByID(ctx, tx, reportID)
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	content, contentType, err := renderMultiExchangeConsoleReport(report, string(body.Format))
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	exportID, _ := multiExchangeConsoleIdentifier("multi_exchange_console-export")
	if err = insertOwnerConsoleCommand(ctx, tx, commandID, principal, key, hash, "report.export",
		"research_report", reportID, "Export immutable simulation-only research report",
		now, auditID, commandID); err != nil {
		return generated.ReportExportResource{}, err
	}
	payloadHash := ownerConsoleHash([]byte(content))
	if _, err = tx.Exec(ctx, multiExchangeConsoleInsertExportSQL, exportID, reportID, commandID,
		body.Format, contentType, content, payloadHash, principal.UserID, principal.SessionID,
		now); err != nil {
		return generated.ReportExportResource{}, multiExchangeConsoleConstraintError(err)
	}
	if _, err = completeOwnerConsoleCommand(ctx, tx, commandID, auditID, principal, "report.export",
		reportID, hash, map[string]any{"id": exportID, "format": body.Format,
			"simulation_only": true}, now, commandID); err != nil {
		return generated.ReportExportResource{}, err
	}
	resource := generated.ReportExportResource{Id: exportID, ReportId: reportID,
		Format:      generated.ReportExportResourceFormat(body.Format),
		ContentType: generated.ReportExportResourceContentType(contentType),
		Content:     content, PayloadHash: payloadHash, Revision: "1",
		SimulationOnly: true, CreatedAt: now}
	return resource, nil
}

func readMultiExchangeConsoleChampionByID(
	ctx context.Context, tx pgx.Tx, reportID string,
) (generated.ChampionChallengerReport, error) {
	row := tx.QueryRow(ctx, `SELECT report.id,report.champion_strategy_version_id,
	  report.challenger_strategy_version_id,report.champion_suite_id,
	  report.challenger_suite_id,challenger.confidence_label,
	  challenger.viability_disposition,report.disposition,report.disclaimer_policy,
	  report.manifest_hash,report.created_at
	FROM research_promotion_champion_challenger_reports report
	JOIN research_promotion_validation_suites challenger ON challenger.id=report.challenger_suite_id
	WHERE report.id=$1`, reportID)
	item, err := scanMultiExchangeConsoleChampion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ChampionChallengerReport{}, console.ErrNotFound
	}
	return item, err
}

func renderMultiExchangeConsoleReport(report generated.ChampionChallengerReport, format string) (string, string, error) {
	switch format {
	case "json":
		value, err := json.Marshal(report)
		return string(value) + "\n", "application/json", err
	case "csv":
	default:
		return "", "", console.ErrInvalidRequest
	}
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	_ = writer.Write([]string{"id", "champion_strategy_version", "challenger_strategy_version",
		"disposition", "confidence", "viability", "manifest_hash", "disclaimer", "created_at"})
	_ = writer.Write([]string{report.Id, report.ChampionStrategyVersion,
		report.ChallengerStrategyVersion, report.Disposition, report.Confidence,
		report.Viability, report.ManifestHash, report.Disclaimer,
		report.CreatedAt.UTC().Format(time.RFC3339Nano)})
	writer.Flush()
	return builder.String(), "text/csv", writer.Error()
}

func lookupMultiExchangeConsoleExportCommand(
	ctx context.Context, tx pgx.Tx, actor, key, hash string,
) (generated.ReportExportResource, bool, error) {
	var commandID, payloadHash string
	err := tx.QueryRow(ctx, `SELECT id,payload_hash FROM command_requests
	  WHERE actor_user_id=$1 AND idempotency_key=$2`, actor, key).Scan(&commandID, &payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ReportExportResource{}, false, nil
	}
	if err != nil {
		return generated.ReportExportResource{}, false, err
	}
	if payloadHash != hash {
		return generated.ReportExportResource{}, false, console.ErrIdempotencyConflict
	}
	var item generated.ReportExportResource
	err = tx.QueryRow(ctx, `SELECT id,report_id,format,content_type,content,payload_hash,
	  simulation_only,created_at FROM multi_exchange_console_report_exports WHERE command_id=$1`, commandID).Scan(
		&item.Id, &item.ReportId, &item.Format, &item.ContentType, &item.Content,
		&item.PayloadHash, &item.SimulationOnly, &item.CreatedAt)
	if err != nil {
		return generated.ReportExportResource{}, false, err
	}
	item.Revision = "1"
	return item, true, nil
}

func multiExchangeConsoleIdentifier(prefix string) (string, error) {
	value, err := ownerConsoleIdentifier(prefix)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(value, "-", 3)
	return parts[0] + "-" + parts[1] + "-" + parts[2][:24], nil
}

func multiExchangeConsoleConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23514") {
		return console.ErrConflict
	}
	return err
}
