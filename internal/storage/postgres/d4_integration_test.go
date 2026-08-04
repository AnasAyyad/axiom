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

func TestV1DD4PostgresOperationalEvidenceQualification(t *testing.T) {
	ctx, pool := openD4TestDatabase(t, "AXIOM_D4_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("D4 clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	assertD4Relations(t, ctx, pool)
	fixture := newD1IntegrationFixture(t, ctx, pool)
	assertD4ReportsAndSchedules(t, ctx, pool, fixture)
	assertD4IncidentEvidenceAndHold(t, ctx, pool, fixture)
	assertD4AlertDeliveryAndEscalation(t, ctx, pool, fixture)
	verification, err := fixture.store.D4AuditVerification(ctx)
	if err != nil || verification.Verdict != apigenerated.Valid || verification.CheckedEvents < 1 {
		t.Fatalf("D4 audit verification=%+v error=%v", verification, err)
	}
}

func TestV1DD4PostgresD1ToD4UpgradeQualification(t *testing.T) {
	ctx, pool := openD4TestDatabase(t, "AXIOM_D4_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 25)
	insertD1AuditSource(t, ctx, pool, "d4-upgrade-audit")
	at := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(
id,severity,state,reason_code,opened_at,resolved_at
) VALUES('d4-upgrade-incident','warning','open','upgrade_review',$1,NULL)`, at); err != nil {
		t.Fatal(err)
	}
	migrations, err := Migrations()
	if err != nil || len(migrations) < 26 {
		t.Fatalf("D4 migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if changed, applyErr := applyMigration(ctx, connection, migrations[25]); applyErr != nil || !changed {
		t.Fatalf("D1-to-D4 migration changed=%t error=%v", changed, applyErr)
	}
	assertD4Relations(t, ctx, pool)
	var revision, timeline int
	var updated time.Time
	if err = pool.QueryRow(ctx, `SELECT incident.revision,incident.updated_at,
(SELECT count(*) FROM v1d_incident_events event WHERE event.incident_id=incident.id)
FROM incidents incident WHERE incident.id='d4-upgrade-incident'`).Scan(
		&revision, &updated, &timeline); err != nil || revision != 1 || timeline != 1 || !updated.Equal(at) {
		t.Fatalf("D4 incident backfill revision=%d updated=%s timeline=%d error=%v",
			revision, updated, timeline, err)
	}
	clock, _ := domain.NewReplayClock(at.Add(time.Hour))
	store, err := NewA11ConsoleStore(pool, []byte(strings.Repeat("u", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := store.D4AuditVerification(ctx)
	if err != nil || verification.Verdict != apigenerated.Valid || verification.CheckedEvents < 1 {
		t.Fatalf("D4 upgraded audit verification=%+v error=%v", verification, err)
	}
	var webhookEnabled bool
	if err = pool.QueryRow(ctx, `SELECT enabled FROM v1d_alert_routes WHERE id='webhook'`).Scan(
		&webhookEnabled); err != nil || webhookEnabled {
		t.Fatalf("D4 upgrade webhook enabled=%t error=%v", webhookEnabled, err)
	}
}

func openD4TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_d4_test") {
		t.Fatal("D4 integration requires a dedicated database ending _d4_test")
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

func assertD4Relations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, relation := range []string{
		"v1d_incident_events", "v1d_incident_replay_inputs", "v1d_incident_resolution_evidence",
		"v1d_report_schedules", "v1d_reports", "v1d_alert_routes",
		"v1d_alert_delivery_attempts", "v1d_alert_escalations", "v1d_audit_chain",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("D4 relation %s exists=%t error=%v", relation, exists, err)
		}
	}
}

func assertD4ReportsAndSchedules(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
) {
	t.Helper()
	assertD4OnDemandReport(t, ctx, pool, fixture)
	assertD4ReportFailure(t, ctx, pool, fixture)
	assertD4ScheduledReport(t, ctx, pool, fixture)
}

func assertD4OnDemandReport(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
) {
	t.Helper()
	command := console.D1Command{Kind: "report", TargetID: "report-d4-risk",
		Action: "create", State: "QUEUED", IdempotencyKey: "d4-report-create-0001",
		Reason: "create deterministic risk evidence report", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "risk"}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, command); err != nil {
		t.Fatal(err)
	}
	workerClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC))
	worker, err := NewD4ReportWorker(pool, "d4-report-worker", workerClock)
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := worker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("D4 on-demand worker worked=%t error=%v", worked, runErr)
	}
	report, err := fixture.store.D4Report(ctx, command.TargetID)
	if err != nil || report.State != apigenerated.ReportResourceStateSUCCEEDED ||
		report.Revision != "3" || report.ContentHash == nil || report.GeneratedAt == nil {
		t.Fatalf("D4 report=%+v error=%v", report, err)
	}
	export, err := fixture.store.CreateD1Export(ctx, fixture.principal, "d4-report-export-0002",
		apigenerated.ExportRequest{ExpectedRevision: report.Revision,
			Format: apigenerated.ExportRequestFormatJson, Reason: "export completed D4 report evidence",
			ResourceId: report.Id, ResourceType: apigenerated.ExportRequestResourceTypeReport})
	if err != nil || export.Content == nil || !strings.Contains(*export.Content, "real_trading_enabled") {
		t.Fatalf("D4 report export=%+v error=%v", export, err)
	}
	for _, forbidden := range []string{"authorization_token", "request_signature", "private_payload"} {
		if strings.Contains(strings.ToLower(*export.Content), forbidden) {
			t.Fatalf("D4 report export leaked %q", forbidden)
		}
	}
}

func assertD4ReportFailure(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
) {
	t.Helper()
	failure := console.D1Command{Kind: "report", TargetID: "report-d4-failure",
		Action: "create", State: "QUEUED", IdempotencyKey: "d4-report-create-failure-0002",
		Reason: "verify durable sanitized report generation failure", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "risk"}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, failure); err != nil {
		t.Fatal(err)
	}
	workerClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 0, 0, 0, time.UTC))
	failureWorker, _ := NewD4ReportWorker(pool, "d4-report-failure-worker", workerClock)
	failureWorker.build = func(context.Context, pgx.Tx, string, string, time.Time) ([]byte, error) {
		return nil, errors.New("injected report content failure")
	}
	if worked, runErr := failureWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("D4 report failure worked=%t error=%v", worked, runErr)
	}
	failed, err := fixture.store.D4Report(ctx, failure.TargetID)
	if err != nil || failed.State != apigenerated.ReportResourceStateFAILED ||
		failed.Revision != "3" || failed.FailureCode == nil ||
		*failed.FailureCode != "report_generation_failed" {
		t.Fatalf("D4 failed report=%+v error=%v", failed, err)
	}
	var failedJobState, failedJobCode string
	if err = pool.QueryRow(ctx, `SELECT job.state,job.failure_code FROM jobs job
JOIN v1d_reports report ON report.job_id=job.id WHERE report.id=$1`, failure.TargetID).Scan(
		&failedJobState, &failedJobCode); err != nil || failedJobState != "FAILED" ||
		failedJobCode != "report_generation_failed" {
		t.Fatalf("D4 failed report job=%s/%s error=%v", failedJobState, failedJobCode, err)
	}
}

func assertD4ScheduledReport(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
) {
	t.Helper()
	schedule := console.D1Command{Kind: "report_schedule", TargetID: "schedule-d4-hourly",
		Action: "create", State: "active", IdempotencyKey: "d4-schedule-create-0003",
		Reason: "schedule deterministic hourly operational report", ExpectedRevision: 1,
		Payload: map[string]any{"report_type": "platform_readiness", "frequency": "hourly", "minute_utc": 1}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, schedule); err != nil {
		t.Fatal(err)
	}
	dueClock, _ := domain.NewReplayClock(time.Date(2030, 8, 3, 12, 2, 0, 0, time.UTC))
	dueWorker, _ := NewD4ReportWorker(pool, "d4-schedule-worker", dueClock)
	if worked, runErr := dueWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("D4 schedule queue worked=%t error=%v", worked, runErr)
	}
	if worked, runErr := dueWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("D4 scheduled report worked=%t error=%v", worked, runErr)
	}
	var generated, audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v1d_reports
WHERE schedule_id=$1 AND state='SUCCEEDED'`, schedule.TargetID).Scan(&generated); err != nil || generated != 1 {
		t.Fatalf("D4 scheduled reports=%d error=%v", generated, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
WHERE event_type LIKE 'report_%'`).Scan(&audits); err != nil || audits < 6 {
		t.Fatalf("D4 report audits=%d error=%v", audits, err)
	}
}

func assertD4IncidentEvidenceAndHold(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
) {
	t.Helper()
	incidentID := "incident-d4-evidence"
	createD4IncidentFixture(t, ctx, fixture, incidentID)
	linkD4IncidentReplay(t, ctx, pool, fixture, incidentID)
	activityID, alertID := linkD4IncidentRelations(t, ctx, pool, fixture, incidentID)
	detail := resolveD4IncidentFixture(t, ctx, fixture, incidentID)
	if detail.ReplayWindow.DatasetId != "dataset-d4-replay" ||
		detail.ReplayWindow.FirstOrdinal != "41" || detail.ReplayWindow.LastOrdinal != "43" ||
		len(detail.RelatedActivityIds) != 1 || detail.RelatedActivityIds[0] != activityID ||
		len(detail.RelatedAlertIds) != 1 || detail.RelatedAlertIds[0] != alertID {
		t.Fatalf("D4 incident relations=%+v", detail)
	}
	assertD4IncidentBundleHold(t, ctx, fixture, incidentID, detail)
}

func createD4IncidentFixture(
	t *testing.T, ctx context.Context, fixture d1IntegrationFixture, incidentID string,
) {
	t.Helper()
	create := console.D1Command{Kind: "incident_create", TargetID: incidentID,
		Action: "create", State: "open", IdempotencyKey: "d4-incident-create-0001",
		Reason: "open incident for deterministic evidence review", ExpectedRevision: 1,
		Payload: map[string]any{"severity": "error", "reason_code": "d4_operational_test", "owner_user_id": fixture.userID}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, create); err != nil {
		t.Fatal(err)
	}
	remediation := console.D1Command{Kind: "incident_update", TargetID: incidentID,
		Action: "add_remediation", IdempotencyKey: "d4-incident-remediate-0002",
		Reason: "record verified remediation for incident", ExpectedRevision: 1,
		Payload: map[string]any{"note": "Restarted the isolated read worker and verified durable recovery."}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, remediation); err != nil {
		t.Fatal(err)
	}
}

func linkD4IncidentReplay(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture, incidentID string,
) {
	t.Helper()
	missingReplay := console.D1Command{Kind: "incident_update", TargetID: incidentID,
		Action: "link_replay", IdempotencyKey: "d4-incident-replay-missing-0003",
		Reason: "reject replay evidence without a qualified input dataset", ExpectedRevision: 2,
		Payload: map[string]any{"dataset_id": "dataset-d4-missing", "first_ordinal": "41",
			"last_ordinal": "43", "source_identity": strings.Repeat("e", 64)}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, missingReplay); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("D4 missing replay dataset error=%v", err)
	}
	seedD4ReplayInput(t, ctx, pool)
	replay := missingReplay
	replay.IdempotencyKey = "d4-incident-replay-valid-0004"
	replay.Reason = "link the complete qualified incident replay input window"
	replay.Payload["dataset_id"] = "dataset-d4-replay"
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, replay); err != nil {
		t.Fatal(err)
	}
}

func linkD4IncidentRelations(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture, incidentID string,
) (string, string) {
	t.Helper()
	var activityID string
	if err := pool.QueryRow(ctx, `SELECT id FROM v1d_activity_projection
WHERE source_type='jobs' ORDER BY activity_revision LIMIT 1`).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	activity := console.D1Command{Kind: "incident_update", TargetID: incidentID,
		Action: "link_activity", IdempotencyKey: "d4-incident-activity-0005",
		Reason: "link correlated durable report activity evidence", ExpectedRevision: 3,
		Payload: map[string]any{"reference_id": activityID}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, activity); err != nil {
		t.Fatal(err)
	}
	alertStore, err := NewAlertStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	incidentAlert, err := alertStore.Upsert(ctx, alerting.Alert{ID: "alert-d4-incident",
		DeduplicationKey: strings.Repeat("d", 64), Severity: alerting.SeverityWarning,
		Reason: alerting.ReasonAlertDelivery, Component: "incident-worker",
		CorrelationID: incidentID, CreatedAt: time.Date(2030, 8, 3, 11, 58, 0, 0, time.UTC),
		LastSeenAt: time.Date(2030, 8, 3, 11, 58, 0, 0, time.UTC), Occurrences: 1, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	linkAlert := console.D1Command{Kind: "incident_update", TargetID: incidentID,
		Action: "link_alert", IdempotencyKey: "d4-incident-alert-0006",
		Reason: "link the correlated sanitized operational alert", ExpectedRevision: 4,
		Payload: map[string]any{"reference_id": incidentAlert.ID}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, linkAlert); err != nil {
		t.Fatal(err)
	}
	return activityID, incidentAlert.ID
}

func resolveD4IncidentFixture(
	t *testing.T, ctx context.Context, fixture d1IntegrationFixture, incidentID string,
) apigenerated.IncidentDetail {
	t.Helper()
	acknowledge := console.D1Command{Kind: "incident", TargetID: incidentID,
		Action: "transition", State: "acknowledged", IdempotencyKey: "d4-incident-ack-0007",
		Reason: "accept ownership after reviewing operational impact", ExpectedRevision: 5, Payload: map[string]any{}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, acknowledge); err != nil {
		t.Fatal(err)
	}
	resolve := console.D1Command{Kind: "incident", TargetID: incidentID,
		Action: "transition", State: "resolved", IdempotencyKey: "resolve-resolve-resolve",
		Reason: "resolve after remediation and recovery verification", ExpectedRevision: 6,
		Payload: map[string]any{"resolution_evidence": "Recovery checks passed and no further mismatches were observed."}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, resolve); err != nil {
		t.Fatal(err)
	}
	detail, err := fixture.store.Incident(ctx, incidentID, true)
	if err != nil || detail.State != apigenerated.IncidentDetailStateResolved ||
		detail.Revision != "7" || len(detail.Timeline) != 7 || detail.ResolutionEvidence == nil {
		t.Fatalf("D4 incident=%+v error=%v", detail, err)
	}
	return detail
}

func assertD4IncidentBundleHold(
	t *testing.T, ctx context.Context, fixture d1IntegrationFixture,
	incidentID string, detail apigenerated.IncidentDetail,
) {
	t.Helper()
	artifact, err := fixture.store.CreateD1Export(ctx, fixture.principal, "d4-incident-export-0009",
		apigenerated.ExportRequest{ExpectedRevision: detail.Revision,
			Format: apigenerated.ExportRequestFormatJson, Reason: "create redacted incident evidence bundle",
			ResourceId: incidentID, ResourceType: apigenerated.ExportRequestResourceTypeIncident})
	if err != nil || artifact.Content == nil || !strings.Contains(*artifact.Content, "timeline_head_hash") {
		t.Fatalf("D4 incident artifact=%+v error=%v", artifact, err)
	}
	revision, _ := strconv.ParseInt(artifact.Revision, 10, 64)
	holdReason := "retain incident evidence for documented operational review"
	invalid := d4HoldCommand(artifact.Id, "incident-missing", "d4-hold-invalid-0010",
		holdReason, revision, fixture.userID)
	if _, err = fixture.store.ExecuteD1(ctx, fixture.principal, invalid); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("D4 invalid hold reference error=%v", err)
	}
	valid := d4HoldCommand(artifact.Id, incidentID, "d4-hold-valid-0011",
		holdReason, revision, fixture.userID)
	if _, err = fixture.store.ExecuteD1(ctx, fixture.principal, valid); err != nil {
		t.Fatal(err)
	}
	detail, err = fixture.store.Incident(ctx, incidentID, false)
	if err != nil || len(detail.EvidenceHolds) != 1 {
		t.Fatalf("D4 incident holds=%+v error=%v", detail.EvidenceHolds, err)
	}
	deleteCommand := console.D1Command{Kind: "export", TargetID: artifact.Id,
		Action: "delete", State: "deleted", IdempotencyKey: "d4-delete-held-0012",
		Reason: "attempt retention cleanup of held artifact", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err = fixture.store.ExecuteD1(ctx, fixture.principal, deleteCommand); !errors.Is(err, console.ErrPrecondition) {
		t.Fatalf("D4 held deletion error=%v", err)
	}
}

func seedD4ReplayInput(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	at := time.Date(2030, 8, 3, 11, 57, 0, 0, time.UTC)
	hash := strings.Repeat("8", 64)
	statements := []struct {
		statement string
		args      []any
	}{
		{`INSERT INTO assets(symbol) VALUES('BTC'),('USDT') ON CONFLICT DO NOTHING`, nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product)
VALUES('instrument-d4-replay','BTC','USDT','spot')`, nil},
		{`INSERT INTO market_data_segments(
id,recorder_session,exchange_id,instrument_id,event_type,schema_version,
parser_version,normalization_version,compression,path,checksum,ordered_content_hash,
record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at
) VALUES('segment-d4-replay','recorder-d4-replay','binance','instrument-d4-replay',
'decision_input','axiom.decision-input.v1','decision-input-v1','decision-input-v1',
'zstd','d4/replay-input.zst',$1,$1,3,41,43,$2,$3,'ready',$3)`, []any{hash, at, at.Add(time.Second)}},
		{`INSERT INTO dataset_manifests(
id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,dataset_kind
) VALUES('dataset-d4-replay',$1,'axiom.decision-input.v1',$2,$3,'building',$3,'decision_inputs')`, []any{hash, at, at.Add(time.Second)}},
		{`INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES('dataset-d4-replay','segment-d4-replay',0)`, nil},
		{`UPDATE dataset_manifests SET state='ready' WHERE id='dataset-d4-replay'`, nil},
		{`UPDATE dataset_manifests SET state='qualified' WHERE id='dataset-d4-replay'`, nil},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.statement, statement.args...); err != nil {
			t.Fatalf("D4 replay input seed %d failed: %v", index+1, err)
		}
	}
}

func d4HoldCommand(
	artifactID, reference, key, reason string, revision int64, userID string,
) console.D1Command {
	return console.D1Command{Kind: "artifact_hold", TargetID: artifactID,
		Action: "incident", State: "held", IdempotencyKey: key, Reason: reason,
		ExpectedRevision: revision, Payload: map[string]any{"hold_type": "incident", "reference_id": reference},
		Authorization: &authentication.ConsumedAuthorization{UserID: userID,
			Purpose:    authentication.PurposeArtifactHold,
			ReasonHash: authentication.AuthorizationBindingHash(reason), TargetRevision: &revision}}
}

func assertD4AlertDeliveryAndEscalation(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture d1IntegrationFixture,
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
	stored, err := alertStore.Upsert(ctx, alerting.Alert{ID: "alert-d4-delivery",
		DeduplicationKey: strings.Repeat("a", 64), Severity: alerting.SeverityWarning,
		Reason: alerting.ReasonAlertDelivery, Component: "report-worker", CorrelationID: "d4-alert-correlation",
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
	escalate := console.D1Command{Kind: "alert", TargetID: stored.ID,
		Action: "escalate", IdempotencyKey: "d4-alert-escalate-0001",
		Reason: "escalate after external delivery failure review", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err = fixture.store.ExecuteD1(ctx, fixture.principal, escalate); err != nil {
		t.Fatal(err)
	}
	detail, err := fixture.store.D4Alert(ctx, stored.ID)
	if err != nil || len(detail.Deliveries) != 1 || len(detail.Escalations) != 1 ||
		detail.Deliveries[0].LatencyMs == nil || *detail.Deliveries[0].LatencyMs != 425 {
		t.Fatalf("D4 alert detail=%+v error=%v", detail, err)
	}
	assertD4AlertRouteTestAndImmutability(t, ctx, pool, fixture, detail.Deliveries[0].Id)
}

func assertD4AlertRouteTestAndImmutability(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	fixture d1IntegrationFixture, deliveryAttemptID string,
) {
	t.Helper()
	testRoute := console.D1Command{Kind: "alert_test", TargetID: "in-app",
		Action: "test", State: "pending", IdempotencyKey: "d4-alert-test-0002",
		Reason: "verify sanitized in-app delivery route", ExpectedRevision: 1, Payload: map[string]any{}}
	if _, err := fixture.store.ExecuteD1(ctx, fixture.principal, testRoute); err != nil {
		t.Fatal(err)
	}
	routes, err := fixture.store.D4AlertRoutes(ctx)
	if err != nil || len(routes.Items) != 2 || routes.Items[0].LastTestState == nil ||
		!routes.Items[1].Enabled || routes.Items[1].Revision != "2" {
		t.Fatalf("D4 alert routes=%+v error=%v", routes, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v1d_alert_delivery_attempts SET latency_ms=0 WHERE id=$1`,
		deliveryAttemptID); err == nil {
		t.Fatal("D4 immutable alert delivery attempt accepted mutation")
	}
}
