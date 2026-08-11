package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/alerting"
	"axiom/internal/api/console"
	apigenerated "axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOperationalEvidencePostgresOperationalEvidenceQualification(t *testing.T) {
	ctx, pool := openOperationalEvidenceTestDatabase(t, "AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("operational evidence clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertOperationalEvidenceRelations(t, ctx, pool)
	fixture := newOwnerControlIntegrationFixture(t, ctx, pool)
	assertOperationalEvidenceReportsAndSchedules(t, ctx, pool, fixture)
	assertOperationalEvidenceIncidentEvidenceAndHold(t, ctx, pool, fixture)
	assertOperationalEvidenceAlertDeliveryAndEscalation(t, ctx, pool, fixture)
	verification, err := fixture.store.OperationalEvidenceAuditVerification(ctx)
	if err != nil || verification.Verdict != apigenerated.Valid || verification.CheckedEvents < 1 {
		t.Fatalf("operational evidence audit verification=%+v error=%v", verification, err)
	}
}

func TestOperationalEvidencePostgresOwnerControlToOperationalEvidenceUpgradeQualification(t *testing.T) {
	ctx, pool := openOperationalEvidenceTestDatabase(t, "AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 25)
	insertOwnerControlAuditSource(t, ctx, pool, "operational_evidence-upgrade-audit")
	at := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(
id,severity,state,reason_code,opened_at,resolved_at
) VALUES('operational_evidence-upgrade-incident','warning','open','upgrade_review',$1,NULL)`, at); err != nil {
		t.Fatal(err)
	}
	migrations, err := Migrations()
	if err != nil || len(migrations) < 26 {
		t.Fatalf("operational evidence migration catalog=%d error=%v", len(migrations), err)
	}
	wantApplied := len(migrations) - 25
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != wantApplied {
		t.Fatalf("owner-control-to-current migration applied=%d want=%d error=%v",
			applied, wantApplied, applyErr)
	}
	assertOperationalEvidenceRelations(t, ctx, pool)
	var revision, timeline int
	var updated time.Time
	if err = pool.QueryRow(ctx, `SELECT incident.revision,incident.updated_at,
(SELECT count(*) FROM owner_console_incident_events event WHERE event.incident_id=incident.id)
FROM incidents incident WHERE incident.id='operational_evidence-upgrade-incident'`).Scan(
		&revision, &updated, &timeline); err != nil || revision != 1 || timeline != 1 || !updated.Equal(at) {
		t.Fatalf("operational evidence incident backfill revision=%d updated=%s timeline=%d error=%v",
			revision, updated, timeline, err)
	}
	clock, _ := domain.NewReplayClock(at.Add(time.Hour))
	store, err := NewOwnerConsoleStore(pool, []byte(strings.Repeat("u", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := store.OperationalEvidenceAuditVerification(ctx)
	if err != nil || verification.Verdict != apigenerated.Valid || verification.CheckedEvents < 1 {
		t.Fatalf("operational evidence upgraded audit verification=%+v error=%v", verification, err)
	}
	var webhookEnabled bool
	if err = pool.QueryRow(ctx, `SELECT enabled FROM owner_console_alert_routes WHERE id='webhook'`).Scan(
		&webhookEnabled); err != nil || webhookEnabled {
		t.Fatalf("operational evidence upgrade webhook enabled=%t error=%v", webhookEnabled, err)
	}
}

func openOperationalEvidenceTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_operational_evidence_test") {
		t.Fatal("operational evidence integration requires a dedicated database ending _operational_evidence_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return ctx, pool
}

func assertOperationalEvidenceRelations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, relation := range []string{
		"owner_console_incident_events", "owner_console_incident_replay_inputs", "owner_console_incident_resolution_evidence",
		"owner_console_report_schedules", "owner_console_reports", "owner_console_alert_routes",
		"owner_console_alert_delivery_attempts", "owner_console_alert_escalations", "owner_console_audit_chain",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("operational evidence relation %s exists=%t error=%v", relation, exists, err)
		}
	}
}

func assertOperationalEvidenceReportsAndSchedules(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	assertOperationalEvidenceOnDemandReport(t, ctx, pool, fixture)
	assertOperationalEvidenceReportFailure(t, ctx, pool, fixture)
	assertOperationalEvidenceScheduledReport(t, ctx, pool, fixture)
}

func assertOperationalEvidenceOnDemandReport(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	command := console.OwnerControlCommand{Kind: "report", TargetID: "report-operational_evidence-risk",
		Action: "create", State: "QUEUED", IdempotencyKey: "operational_evidence-report-create-0001",
		Reason: "create deterministic risk evidence report", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "risk"}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, command); err != nil {
		t.Fatal(err)
	}
	workerClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC))
	worker, err := NewOperationalEvidenceReportWorker(pool, "operational_evidence-report-worker", workerClock)
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := worker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("operational evidence on-demand worker worked=%t error=%v", worked, runErr)
	}
	report, err := fixture.store.OperationalEvidenceReport(ctx, command.TargetID)
	if err != nil || report.State != apigenerated.ReportResourceStateSUCCEEDED ||
		report.Revision != "3" || report.ContentHash == nil || report.GeneratedAt == nil {
		t.Fatalf("operational evidence report=%+v error=%v", report, err)
	}
	export, err := fixture.store.CreateOwnerControlExport(ctx, fixture.principal, "operational_evidence-report-export-0002",
		apigenerated.ExportRequest{ExpectedRevision: report.Revision,
			Format: apigenerated.ExportRequestFormatJson, Reason: "export completed operational evidence report evidence",
			ResourceId: report.Id, ResourceType: apigenerated.ExportRequestResourceTypeReport})
	if err != nil || export.Content == nil || !strings.Contains(*export.Content, "real_trading_enabled") {
		t.Fatalf("operational evidence report export=%+v error=%v", export, err)
	}
	for _, forbidden := range []string{"authorization_token", "request_signature", "private_payload"} {
		if strings.Contains(strings.ToLower(*export.Content), forbidden) {
			t.Fatalf("operational evidence report export leaked %q", forbidden)
		}
	}
}

func assertOperationalEvidenceReportFailure(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	failure := console.OwnerControlCommand{Kind: "report", TargetID: "report-operational_evidence-failure",
		Action: "create", State: "QUEUED", IdempotencyKey: "operational_evidence-report-create-failure-0002",
		Reason: "verify durable sanitized report generation failure", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "risk"}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, failure); err != nil {
		t.Fatal(err)
	}
	workerClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC))
	failureWorker, _ := NewOperationalEvidenceReportWorker(pool, "operational_evidence-report-failure-worker", workerClock)
	failureWorker.build = func(context.Context, pgx.Tx, string, string, time.Time) ([]byte, error) {
		return nil, errors.New("injected report content failure")
	}
	if worked, runErr := failureWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("operational evidence report failure worked=%t error=%v", worked, runErr)
	}
	failed, err := fixture.store.OperationalEvidenceReport(ctx, failure.TargetID)
	if err != nil || failed.State != apigenerated.ReportResourceStateFAILED ||
		failed.Revision != "3" || failed.FailureCode == nil ||
		*failed.FailureCode != "report_generation_failed" {
		t.Fatalf("operational evidence failed report=%+v error=%v", failed, err)
	}
	var failedJobState, failedJobCode string
	if err = pool.QueryRow(ctx, `SELECT job.state,job.failure_code FROM jobs job
JOIN owner_console_reports report ON report.job_id=job.id WHERE report.id=$1`, failure.TargetID).Scan(
		&failedJobState, &failedJobCode); err != nil || failedJobState != "FAILED" ||
		failedJobCode != "report_generation_failed" {
		t.Fatalf("operational evidence failed report job=%s/%s error=%v", failedJobState, failedJobCode, err)
	}
}

func assertOperationalEvidenceScheduledReport(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	schedule := console.OwnerControlCommand{Kind: "report_schedule", TargetID: "schedule-operational_evidence-hourly",
		Action: "create", State: "active", IdempotencyKey: "operational_evidence-schedule-create-0003",
		Reason: "schedule deterministic hourly operational report", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "platform_readiness", "frequency": "hourly", "minute_utc": 1}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, schedule); err != nil {
		t.Fatal(err)
	}
	dueClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 2, 0, 0, time.UTC))
	dueWorker, _ := NewOperationalEvidenceReportWorker(pool, "operational_evidence-schedule-worker", dueClock)
	if worked, runErr := dueWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("operational evidence schedule queue worked=%t error=%v", worked, runErr)
	}
	if worked, runErr := dueWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("operational evidence scheduled report worked=%t error=%v", worked, runErr)
	}
	var generated, audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM owner_console_reports
WHERE schedule_id=$1 AND state='SUCCEEDED'`, schedule.TargetID).Scan(&generated); err != nil || generated != 1 {
		t.Fatalf("operational evidence scheduled reports=%d error=%v", generated, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
WHERE event_type LIKE 'report_%'`).Scan(&audits); err != nil || audits < 6 {
		t.Fatalf("operational evidence report audits=%d error=%v", audits, err)
	}
}

func assertOperationalEvidenceIncidentEvidenceAndHold(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	incidentID := "incident-operational_evidence-evidence"
	createOperationalEvidenceIncidentFixture(t, ctx, fixture, incidentID)
	linkOperationalEvidenceIncidentReplay(t, ctx, pool, fixture, incidentID)
	activityID, alertID := linkOperationalEvidenceIncidentRelations(t, ctx, pool, fixture, incidentID)
	detail := resolveOperationalEvidenceIncidentFixture(t, ctx, fixture, incidentID)
	if detail.ReplayWindow.DatasetId != "dataset-operational_evidence-replay" ||
		detail.ReplayWindow.FirstOrdinal != "41" || detail.ReplayWindow.LastOrdinal != "43" ||
		len(detail.RelatedActivityIds) != 1 || detail.RelatedActivityIds[0] != activityID ||
		len(detail.RelatedAlertIds) != 1 || detail.RelatedAlertIds[0] != alertID {
		t.Fatalf("operational evidence incident relations=%+v", detail)
	}
	assertOperationalEvidenceIncidentBundleHold(t, ctx, fixture, incidentID, detail)
}

func createOperationalEvidenceIncidentFixture(
	t *testing.T, ctx context.Context, fixture ownerControlIntegrationFixture, incidentID string,
) {
	t.Helper()
	create := console.OwnerControlCommand{Kind: "incident_create", TargetID: incidentID,
		Action: "create", State: "open", IdempotencyKey: "operational_evidence-incident-create-0001",
		Reason: "open incident for deterministic evidence review", ExpectedRevision: 1,
		Payload: map[string]any{"severity": "error", "reason_code": "operational_evidence_operational_test", "owner_user_id": fixture.userID}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, create); err != nil {
		t.Fatal(err)
	}
	remediation := console.OwnerControlCommand{Kind: "incident_update", TargetID: incidentID,
		Action: "add_remediation", IdempotencyKey: "operational_evidence-incident-remediate-0002",
		Reason: "record verified remediation for incident", ExpectedRevision: 1,
		Payload: map[string]any{"note": "Restarted the isolated read worker and verified durable recovery."}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, remediation); err != nil {
		t.Fatal(err)
	}
}

func linkOperationalEvidenceIncidentReplay(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture, incidentID string,
) {
	t.Helper()
	missingReplay := console.OwnerControlCommand{Kind: "incident_update", TargetID: incidentID,
		Action: "link_replay", IdempotencyKey: "operational_evidence-incident-replay-missing-0003",
		Reason: "reject replay evidence without a qualified input dataset", ExpectedRevision: 2,
		Payload: map[string]any{"dataset_id": "dataset-operational_evidence-missing", "first_ordinal": "41",
			"last_ordinal": "43", "source_identity": strings.Repeat("e", 64)}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, missingReplay); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("operational evidence missing replay dataset error=%v", err)
	}
	seedOperationalEvidenceReplayInput(t, ctx, pool)
	replay := missingReplay
	replay.IdempotencyKey = "operational_evidence-incident-replay-valid-0004"
	replay.Reason = "link the complete qualified incident replay input window"
	replay.Payload["dataset_id"] = "dataset-operational_evidence-replay"
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, replay); err != nil {
		t.Fatal(err)
	}
}

func linkOperationalEvidenceIncidentRelations(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture, incidentID string,
) (string, string) {
	t.Helper()
	var activityID string
	if err := pool.QueryRow(ctx, `SELECT id FROM owner_console_activity_projection
WHERE source_type='jobs' ORDER BY activity_revision LIMIT 1`).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	activity := console.OwnerControlCommand{Kind: "incident_update", TargetID: incidentID,
		Action: "link_activity", IdempotencyKey: "operational_evidence-incident-activity-0005",
		Reason: "link correlated durable report activity evidence", ExpectedRevision: 3,
		Payload: map[string]any{"reference_id": activityID}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, activity); err != nil {
		t.Fatal(err)
	}
	alertStore, err := NewAlertStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	incidentAlert, err := alertStore.Upsert(ctx, alerting.Alert{ID: "alert-operational_evidence-incident",
		DeduplicationKey: strings.Repeat("d", 64), Severity: alerting.SeverityWarning,
		Reason: alerting.ReasonAlertDelivery, Component: "incident-worker",
		CorrelationID: incidentID, CreatedAt: time.Date(2030, 8, 3, 11, 58, 0, 0, time.UTC),
		LastSeenAt: time.Date(2030, 8, 3, 11, 58, 0, 0, time.UTC), Occurrences: 1, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	linkAlert := console.OwnerControlCommand{Kind: "incident_update", TargetID: incidentID,
		Action: "link_alert", IdempotencyKey: "operational_evidence-incident-alert-0006",
		Reason: "link the correlated sanitized operational alert", ExpectedRevision: 4,
		Payload: map[string]any{"reference_id": incidentAlert.ID}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, linkAlert); err != nil {
		t.Fatal(err)
	}
	return activityID, incidentAlert.ID
}

func resolveOperationalEvidenceIncidentFixture(
	t *testing.T, ctx context.Context, fixture ownerControlIntegrationFixture, incidentID string,
) apigenerated.IncidentDetail {
	t.Helper()
	acknowledge := console.OwnerControlCommand{Kind: "incident", TargetID: incidentID,
		Action: "transition", State: "acknowledged", IdempotencyKey: "operational_evidence-incident-ack-0007",
		Reason: "accept ownership after reviewing operational impact", ExpectedRevision: 5, Payload: map[string]any{}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, acknowledge); err != nil {
		t.Fatal(err)
	}
	resolve := console.OwnerControlCommand{Kind: "incident", TargetID: incidentID,
		Action: "transition", State: "resolved", IdempotencyKey: "resolve-resolve-resolve",
		Reason: "resolve after remediation and recovery verification", ExpectedRevision: 6,
		Payload: map[string]any{"resolution_evidence": "Recovery checks passed and no further mismatches were observed."}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, resolve); err != nil {
		t.Fatal(err)
	}
	detail, err := fixture.store.Incident(ctx, incidentID, true)
	if err != nil || detail.State != apigenerated.IncidentDetailStateResolved ||
		detail.Revision != "7" || len(detail.Timeline) != 7 || detail.ResolutionEvidence == nil {
		t.Fatalf("operational evidence incident=%+v error=%v", detail, err)
	}
	return detail
}

func assertOperationalEvidenceIncidentBundleHold(
	t *testing.T, ctx context.Context, fixture ownerControlIntegrationFixture,
	incidentID string, detail apigenerated.IncidentDetail,
) {
	t.Helper()
	artifact, err := fixture.store.CreateOwnerControlExport(ctx, fixture.principal, "operational_evidence-incident-export-0009",
		apigenerated.ExportRequest{ExpectedRevision: detail.Revision,
			Format: apigenerated.ExportRequestFormatJson, Reason: "create redacted incident evidence bundle",
			ResourceId: incidentID, ResourceType: apigenerated.ExportRequestResourceTypeIncident})
	if err != nil || artifact.Content == nil || !strings.Contains(*artifact.Content, "timeline_head_hash") {
		t.Fatalf("operational evidence incident artifact=%+v error=%v", artifact, err)
	}
	revision, _ := strconv.ParseInt(artifact.Revision, 10, 64)
	holdReason := "retain incident evidence for documented operational review"
	invalid := operationalEvidenceHoldCommand(artifact.Id, "incident-missing", "operational_evidence-hold-invalid-0010",
		holdReason, revision, fixture.userID)
	if _, err = fixture.store.ExecuteOwnerControl(ctx, fixture.principal, invalid); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("operational evidence invalid hold reference error=%v", err)
	}
	valid := operationalEvidenceHoldCommand(artifact.Id, incidentID, "operational_evidence-hold-valid-0011",
		holdReason, revision, fixture.userID)
	if _, err = fixture.store.ExecuteOwnerControl(ctx, fixture.principal, valid); err != nil {
		t.Fatal(err)
	}
	detail, err = fixture.store.Incident(ctx, incidentID, false)
	if err != nil || len(detail.EvidenceHolds) != 1 {
		t.Fatalf("operational evidence incident holds=%+v error=%v", detail.EvidenceHolds, err)
	}
	deleteCommand := console.OwnerControlCommand{Kind: "export", TargetID: artifact.Id,
		Action: "delete", State: "deleted", IdempotencyKey: "operational_evidence-delete-held-0012",
		Reason: "attempt retention cleanup of held artifact", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err = fixture.store.ExecuteOwnerControl(ctx, fixture.principal, deleteCommand); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("operational evidence held deletion error=%v", err)
	}
}

func seedOperationalEvidenceReplayInput(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	at := time.Date(2030, 8, 3, 11, 57, 0, 0, time.UTC)
	hash := strings.Repeat("8", 64)
	statements := []struct {
		statement string
		args      []any
	}{
		{`INSERT INTO assets(symbol) VALUES('BTC'),('USDT') ON CONFLICT DO NOTHING`, nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product)
VALUES('instrument-BTC-USDT','BTC','USDT','spot') ON CONFLICT DO NOTHING`, nil},
		{`INSERT INTO market_data_segments(
id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
parser_version,normalization_version,compression,path,checksum,ordered_content_hash,
record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at
) VALUES('segment-operational_evidence-replay','recorder-operational_evidence-replay','binance','instrument-BTC-USDT',
'decision_input','axiom.decision-input.v1','decision-input-v1','decision-input-v1',
'zstd','operational_evidence/replay-input.zst',$1,$1,3,41,43,$2,$3,'ready',$3)`, []any{hash, at, at.Add(time.Second)}},
		{`INSERT INTO dataset_manifests(
id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,dataset_kind
) VALUES('dataset-operational_evidence-replay',$1,'axiom.decision-input.v1',$2,$3,'building',$3,'decision_inputs')`, []any{hash, at, at.Add(time.Second)}},
		{`INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES('dataset-operational_evidence-replay','segment-operational_evidence-replay',0)`, nil},
		{`UPDATE dataset_manifests SET state='ready' WHERE id='dataset-operational_evidence-replay'`, nil},
		{`UPDATE dataset_manifests SET state='qualified' WHERE id='dataset-operational_evidence-replay'`, nil},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.statement, statement.args...); err != nil {
			t.Fatalf("operational evidence replay input seed %d failed: %v", index+1, err)
		}
	}
}

func operationalEvidenceHoldCommand(
	artifactID, reference, key, reason string, revision int64, userID string,
) console.OwnerControlCommand {
	return console.OwnerControlCommand{Kind: "artifact_hold", TargetID: artifactID,
		Action: "incident", State: "held", IdempotencyKey: key, Reason: reason,
		ExpectedRevision: revision, Payload: map[string]any{"hold_type": "incident", "reference_id": reference},
		Authorization: &authentication.ConsumedAuthorization{UserID: userID,
			Purpose:    authentication.PurposeArtifactHold,
			ReasonHash: authentication.AuthorizationBindingHash(reason), TargetRevision: &revision}}
}

func assertOperationalEvidenceAlertDeliveryAndEscalation(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ownerControlIntegrationFixture,
) {
	t.Helper()
	at := time.Date(2030, 8, 3, 11, 59, 0, 0, time.UTC)
	alertStore, err := NewAlertStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = alertStore.SetWebhookRouteEnabled(ctx, true, at); err != nil {
		t.Fatal(err)
	}
	if err = alertStore.SetWebhookRouteEnabled(ctx, true, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	stored, err := alertStore.Upsert(ctx, alerting.Alert{ID: "alert-operational_evidence-delivery",
		DeduplicationKey: strings.Repeat("a", 64), Severity: alerting.SeverityWarning,
		Reason: alerting.ReasonAlertDelivery, Component: "report-worker", CorrelationID: "operational_evidence-alert-correlation",
		CreatedAt: at, LastSeenAt: at, Occurrences: 1, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	deliveryID, err := alertStore.PrepareDelivery(ctx, stored, "webhook", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = alertStore.CompleteDelivery(ctx, deliveryID, false, "sink_unavailable",
		at.Add(100*time.Millisecond), at.Add(525*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	escalate := console.OwnerControlCommand{Kind: "alert", TargetID: stored.ID,
		Action: "escalate", IdempotencyKey: "operational_evidence-alert-escalate-0001",
		Reason: "escalate after external delivery failure review", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err = fixture.store.ExecuteOwnerControl(ctx, fixture.principal, escalate); err != nil {
		t.Fatal(err)
	}
	detail, err := fixture.store.OperationalEvidenceAlert(ctx, stored.ID)
	if err != nil || len(detail.Deliveries) != 1 || len(detail.Escalations) != 1 ||
		detail.Deliveries[0].LatencyMs == nil || *detail.Deliveries[0].LatencyMs != 425 {
		t.Fatalf("operational evidence alert detail=%+v error=%v", detail, err)
	}
	assertOperationalEvidenceAlertRouteTestAndImmutability(t, ctx, pool, fixture, detail.Deliveries[0].Id)
}

func assertOperationalEvidenceAlertRouteTestAndImmutability(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	fixture ownerControlIntegrationFixture, deliveryAttemptID string,
) {
	t.Helper()
	testRoute := console.OwnerControlCommand{Kind: "alert_test", TargetID: "in-app",
		Action: "test", State: "pending", IdempotencyKey: "operational_evidence-alert-test-0002",
		Reason: "verify sanitized in-app delivery route", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err := fixture.store.ExecuteOwnerControl(ctx, fixture.principal, testRoute); err != nil {
		t.Fatal(err)
	}
	routes, err := fixture.store.OperationalEvidenceAlertRoutes(ctx)
	if err != nil || len(routes.Items) != 2 || routes.Items[0].LastTestState == nil ||
		!routes.Items[1].Enabled || routes.Items[1].Revision != "2" {
		t.Fatalf("operational evidence alert routes=%+v error=%v", routes, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE owner_console_alert_delivery_attempts SET latency_ms=0 WHERE id=$1`,
		deliveryAttemptID); err == nil {
		t.Fatal("operational evidence immutable alert delivery attempt accepted mutation")
	}
}
