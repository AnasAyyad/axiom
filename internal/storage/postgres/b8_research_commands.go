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

const b8InsertFaultSQL = `INSERT INTO b8_replay_fault_schedules(
  id,replay_id,command_id,schedule_revision,fault_kind,event_ordinal,delay_nanos,
  repeatable,reason,actor_user_id,session_id,payload_hash,simulation_only,created_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true,$13)`

const b8InsertExportSQL = `INSERT INTO b8_report_exports(
  id,report_id,command_id,format,content_type,content,payload_hash,actor_user_id,
  session_id,simulation_only,created_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10)`

type b8FaultCommand struct {
	principal                authentication.Principal
	body                     generated.ReplayFaultRequest
	replayID, key, hash      string
	expected, ordinal, delay int64
}

// ScheduleReplayFault appends one deterministic fault to a queued replay.
func (store *A11ConsoleStore) ScheduleReplayFault(
	ctx context.Context, principal authentication.Principal, replayID, key string,
	body generated.ReplayFaultRequest,
) (generated.ReplayFaultResource, error) {
	_, hash, err := a11CommandPayload(map[string]any{"replay_id": replayID, "body": body})
	if err != nil {
		return generated.ReplayFaultResource{}, console.ErrInvalidRequest
	}
	command, err := newB8FaultCommand(principal, replayID, key, hash, body)
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupB8FaultCommand(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.ReplayFaultResource{}, lookupErr
	} else if found {
		return existing, commitB8Fault(ctx, tx, nil)
	}
	resource, err := store.scheduleB8FaultTx(ctx, tx, command)
	return resource, commitB8Fault(ctx, tx, err)
}

func newB8FaultCommand(principal authentication.Principal, replayID, key, hash string,
	body generated.ReplayFaultRequest) (b8FaultCommand, error) {
	expected, expectedErr := strconv.ParseInt(body.ExpectedRevision, 10, 64)
	ordinal, ordinalErr := strconv.ParseInt(body.Ordinal, 10, 64)
	delay, delayErr := strconv.ParseInt(body.DelayNanos, 10, 64)
	validDelay := (body.Fault == generated.Latency && delay > 0) ||
		(body.Fault != generated.Latency && delay == 0)
	if expectedErr != nil || ordinalErr != nil || delayErr != nil || expected < 0 ||
		ordinal <= 0 || delay < 0 || !body.Fault.Valid() || !validDelay ||
		len(strings.TrimSpace(body.Reason)) < 8 || len(body.Reason) > 500 {
		return b8FaultCommand{}, console.ErrInvalidRequest
	}
	return b8FaultCommand{principal: principal, replayID: replayID, key: key, hash: hash,
		body: body, expected: expected, ordinal: ordinal, delay: delay}, nil
}

func (store *A11ConsoleStore) scheduleB8FaultTx(
	ctx context.Context, tx pgx.Tx, command b8FaultCommand,
) (generated.ReplayFaultResource, error) {
	var err error
	now := store.clock.Now().UTC
	revision, err := lockB8FaultRevision(ctx, tx, command.replayID, now)
	if err != nil {
		return generated.ReplayFaultResource{}, err
	}
	if revision != command.expected {
		return generated.ReplayFaultResource{}, console.ErrConflict
	}
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	faultID, _ := b8Identifier("b8-fault")
	if err = insertA11Command(ctx, tx, commandID, command.principal, command.key, command.hash,
		"replay.fault.schedule", "replay", command.replayID, command.body.Reason, now, auditID, commandID); err != nil {
		return generated.ReplayFaultResource{}, err
	}
	repeatable := command.body.Repeatable != nil && *command.body.Repeatable
	if _, err = tx.Exec(ctx, b8InsertFaultSQL,
		faultID, command.replayID, commandID, revision+1, command.body.Fault,
		command.ordinal, command.delay, repeatable, command.body.Reason,
		command.principal.UserID, command.principal.SessionID, command.hash, now); err != nil {
		return generated.ReplayFaultResource{}, b8ConstraintError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE b8_replay_fault_schedule_states
	  SET revision=revision+1,updated_at=$2 WHERE replay_id=$1 AND revision=$3`,
		command.replayID, now, revision); err != nil {
		return generated.ReplayFaultResource{}, err
	}
	if _, err = completeA11Command(ctx, tx, commandID, auditID, command.principal,
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

func lockB8FaultRevision(
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
	if _, err := tx.Exec(ctx, `INSERT INTO b8_replay_fault_schedule_states(replay_id,revision,updated_at)
	  VALUES($1,0,$2) ON CONFLICT (replay_id) DO NOTHING`, replayID, now); err != nil {
		return 0, err
	}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM b8_replay_fault_schedule_states
	  WHERE replay_id=$1 FOR UPDATE`, replayID).Scan(&revision)
	return revision, err
}

func lookupB8FaultCommand(
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
	  repeatable,schedule_revision,created_at FROM b8_replay_fault_schedules
	  WHERE command_id=$1`, commandID).Scan(&item.Id, &item.ReplayId, &item.Fault,
		&item.Ordinal, &item.DelayNanos, &item.Repeatable, &revision, &item.CreatedAt)
	if err != nil {
		return generated.ReplayFaultResource{}, false, err
	}
	item.Revision = strconv.FormatInt(revision, 10)
	item.SimulationOnly = true
	return item, true, nil
}

func commitB8Fault(ctx context.Context, tx pgx.Tx, prior error) error {
	if prior != nil {
		return prior
	}
	return tx.Commit(ctx)
}

// ExportReport persists a deterministic simulation-only research export.
func (store *A11ConsoleStore) ExportReport(
	ctx context.Context, principal authentication.Principal, reportID, key string,
	body generated.ReportExportRequest,
) (generated.ReportExportResource, error) {
	if !body.Format.Valid() {
		return generated.ReportExportResource{}, console.ErrInvalidRequest
	}
	_, hash, err := a11CommandPayload(map[string]any{"report_id": reportID, "body": body})
	if err != nil {
		return generated.ReportExportResource{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupB8ExportCommand(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.ReportExportResource{}, lookupErr
	} else if found {
		return existing, commitB8Fault(ctx, tx, nil)
	}
	resource, err := store.exportB8ReportTx(ctx, tx, principal, reportID, key, hash, body)
	return resource, commitB8Fault(ctx, tx, err)
}

func (store *A11ConsoleStore) exportB8ReportTx(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	reportID, key, hash string, body generated.ReportExportRequest,
) (generated.ReportExportResource, error) {
	report, err := readB8ChampionByID(ctx, tx, reportID)
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	content, contentType, err := renderB8Report(report, string(body.Format))
	if err != nil {
		return generated.ReportExportResource{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	exportID, _ := b8Identifier("b8-export")
	if err = insertA11Command(ctx, tx, commandID, principal, key, hash, "report.export",
		"research_report", reportID, "Export immutable simulation-only research report",
		now, auditID, commandID); err != nil {
		return generated.ReportExportResource{}, err
	}
	payloadHash := a11Hash([]byte(content))
	if _, err = tx.Exec(ctx, b8InsertExportSQL, exportID, reportID, commandID,
		body.Format, contentType, content, payloadHash, principal.UserID, principal.SessionID,
		now); err != nil {
		return generated.ReportExportResource{}, b8ConstraintError(err)
	}
	if _, err = completeA11Command(ctx, tx, commandID, auditID, principal, "report.export",
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

func readB8ChampionByID(
	ctx context.Context, tx pgx.Tx, reportID string,
) (generated.ChampionChallengerReport, error) {
	row := tx.QueryRow(ctx, `SELECT report.id,report.champion_strategy_version_id,
	  report.challenger_strategy_version_id,report.champion_suite_id,
	  report.challenger_suite_id,challenger.confidence_label,
	  challenger.viability_disposition,report.disposition,report.disclaimer_policy,
	  report.manifest_hash,report.created_at
	FROM b7_champion_challenger_reports report
	JOIN b7_validation_suites challenger ON challenger.id=report.challenger_suite_id
	WHERE report.id=$1`, reportID)
	item, err := scanB8Champion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ChampionChallengerReport{}, console.ErrNotFound
	}
	return item, err
}

func renderB8Report(report generated.ChampionChallengerReport, format string) (string, string, error) {
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

func lookupB8ExportCommand(
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
	  simulation_only,created_at FROM b8_report_exports WHERE command_id=$1`, commandID).Scan(
		&item.Id, &item.ReportId, &item.Format, &item.ContentType, &item.Content,
		&item.PayloadHash, &item.SimulationOnly, &item.CreatedAt)
	if err != nil {
		return generated.ReportExportResource{}, false, err
	}
	item.Revision = "1"
	return item, true, nil
}

func b8Identifier(prefix string) (string, error) {
	value, err := a11Identifier(prefix)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(value, "-", 3)
	return parts[0] + "-" + parts[1] + "-" + parts[2][:24], nil
}

func b8ConstraintError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23514") {
		return console.ErrConflict
	}
	return err
}
