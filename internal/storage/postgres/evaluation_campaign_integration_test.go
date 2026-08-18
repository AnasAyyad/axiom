package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/recorder"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvaluationCampaignPostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openEvaluationCampaignTestDatabase(t, "AXIOM_EVALUATION_CAMPAIGN_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("evaluation campaign clean migration applied=%d want=%d error=%v", applied, len(migrations), applyErr)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 0 {
		t.Fatalf("evaluation campaign idempotent migration applied=%d error=%v", applied, applyErr)
	}
	assertEvaluationCampaignSchemaAndRecovery(t, ctx, pool)
}

func TestEvaluationCampaignPostgresSemanticRuntimeToCampaignUpgradeQualification(t *testing.T) {
	ctx, pool := openEvaluationCampaignTestDatabase(t, "AXIOM_EVALUATION_CAMPAIGN_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) != 57 || migrations[53].Version != "000054" ||
		migrations[54].Version != "000055" ||
		migrations[55].Version != "000056" ||
		migrations[56].Version != "000057" {
		t.Fatalf("evaluation campaign migration catalog=%d error=%v", len(migrations), err)
	}
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 54)
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 3 {
		t.Fatalf("migration-54-to-evaluation-campaign applied=%d error=%v", applied, applyErr)
	}
	assertEvaluationCampaignSchemaAndRecovery(t, ctx, pool)
}

func assertEvaluationCampaignSchemaAndRecovery(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	assertEvaluationMarketReferences(t, ctx, pool)
	ensureEvaluationTestRoles(t, ctx, pool)
	if err := ApplyRoleGrants(ctx, pool, "axiom_app", "axiom_recorder", "axiom_readonly"); err != nil {
		t.Fatal(err)
	}
	assertEvaluationCampaignRoleGrants(t, ctx, pool)
	now := time.Date(2030, 8, 11, 12, 0, 0, 0, time.UTC)
	assertNonCampaignRecorderObservationNoop(t, ctx, pool, now)
	assertEvaluationRecorderGapRecovery(t, ctx, pool, now)
	assertRecordedSegmentCommitSurvivesCampaignUpdate(t, ctx, pool, now.Add(10*time.Minute))
	assertHistoricalDatasetRegistration(t, ctx, pool, now)
	assertStandaloneEvaluationAudit(t, ctx, pool, now)
	assertEvaluationCandidateLockAndShadowIsolation(t, ctx, pool, now)
	assertEvaluationPartialReportEvidence(t, ctx, pool, now)
	assertEvaluationRestartClaim(t, ctx, pool, now)
}

func assertRecordedSegmentCommitSurvivesCampaignUpdate(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	const campaignID = "evaluation-segment-contention"
	const sessionID = "evaluation-segment-contention-session"
	prepareRecordedSegmentContention(t, ctx, pool, campaignID, sessionID, now)
	committer, manifest := commitRecordedSegmentDuringCampaignUpdate(t, ctx, pool, campaignID, sessionID, now)
	assertRecordedSegmentQuarantine(t, ctx, pool, committer, campaignID, sessionID, manifest, now)
	if _, err := pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='BLOCKED',reason_code='TEST_COMPLETE',updated_at=$2
WHERE campaign_id=$1`, campaignID, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE evaluation_campaigns SET state='PARTIAL',current_stage=NULL,
reason_code='TEST_COMPLETE',updated_at=$2 WHERE id=$1`, campaignID, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func prepareRecordedSegmentContention(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	campaignID, sessionID string, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaigns(
id,preset,state,current_stage,created_at,updated_at)
VALUES($1,'balanced_full_v1','RUNNING','RECORDER_QUALIFICATION',$2,$2)`, campaignID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_recorder_requests(campaign_id,desired_session_id,
state,binance_session_id,bybit_session_id,storage_baseline_bytes,requested_at,activated_at,updated_at)
VALUES($1,$2,'ACTIVE',$2,$2||'-bybit',0,$3,$3,$3)`, campaignID, sessionID, now); err != nil {
		t.Fatal(err)
	}
}

func commitRecordedSegmentDuringCampaignUpdate(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	campaignID, sessionID string, now time.Time) (*RecordedSegmentCommitter, segments.Manifest) {
	t.Helper()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err = blocker.Exec(ctx, `UPDATE evaluation_campaigns SET updated_at=$2 WHERE id=$1`,
		campaignID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	committer, err := NewRecordedSegmentCommitter(pool)
	if err != nil {
		t.Fatal(err)
	}
	manifest, finalizedAt := recordedSegmentCommitFixture()
	manifest.Spec.Name = sessionID + "-000001-wire-content"
	manifest.Path = manifest.Spec.Name + ".parquet"
	result := make(chan error, 1)
	commitContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		result <- committer.Commit(commitContext, sessionID, "binance", manifest, finalizedAt)
	}()
	waitForContendedSegmentCommit(t, ctx, blocker, result)
	return committer, manifest
}

func waitForContendedSegmentCommit(t *testing.T, ctx context.Context, blocker pgx.Tx, result <-chan error) {
	t.Helper()
	var commitErr error
	commitReturned := false
	select {
	case commitErr = <-result:
		commitReturned = true
		if commitErr != nil {
			t.Fatalf("segment commit failed during campaign update: %v", commitErr)
		}
	case <-time.After(200 * time.Millisecond):
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if !commitReturned {
		select {
		case commitErr = <-result:
			commitReturned = true
		case <-time.After(2 * time.Second):
			t.Fatal("segment commit remained blocked after campaign update completed")
		}
	}
	if commitErr != nil {
		t.Fatalf("segment commit failed after campaign update completed: %v", commitErr)
	}
}

func assertRecordedSegmentQuarantine(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	committer *RecordedSegmentCommitter, campaignID, sessionID string, manifest segments.Manifest, now time.Time) {
	t.Helper()
	if err := committer.QuarantineRecorderArtifacts(ctx, sessionID, "binance",
		[]string{manifest.Spec.Name}, []segments.Manifest{manifest}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := committer.QuarantineRecorderArtifacts(ctx, sessionID, "binance",
		[]string{manifest.Spec.Name}, []segments.Manifest{manifest}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("quarantine replay failed: %v", err)
	}
	var state string
	var charged, recorded int64
	if err := pool.QueryRow(ctx, `SELECT segment.state,evidence.byte_count,request.recorded_bytes
FROM market_data_segments segment
JOIN evaluation_campaign_recording_segments evidence ON evidence.segment_id=segment.id
JOIN evaluation_recorder_requests request ON request.campaign_id=evidence.campaign_id
WHERE segment.id=$1 AND evidence.campaign_id=$2`, manifest.Spec.Name, campaignID).
		Scan(&state, &charged, &recorded); err != nil || state != "quarantined" ||
		charged != manifest.Size || recorded != manifest.Size {
		t.Fatalf("state=%s charged=%d recorded=%d error=%v", state, charged, recorded, err)
	}
}

func assertEvaluationRecorderGapRecovery(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaigns(
id,preset,state,current_stage,created_at,updated_at)
VALUES('evaluation-gap-recovery','balanced_full_v1','RUNNING','RECORDER_QUALIFICATION',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_recorder_requests(campaign_id,desired_session_id,
state,binance_session_id,bybit_session_id,storage_baseline_bytes,requested_at,activated_at,updated_at)
VALUES('evaluation-gap-recovery','evaluation-gap-session','ACTIVE','evaluation-gap-session',
'evaluation-gap-session-bybit',0,$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	clock, _ := domain.NewReplayClock(now)
	store, err := NewEvaluationRecorderControlStore(pool, clock)
	if err != nil {
		t.Fatal(err)
	}
	first := evaluationRecorderIntegrationObservations(now, 100, 4)
	if err = store.Observe(ctx, "evaluation-gap-session", now, true, first); err != nil {
		t.Fatal(err)
	}
	qualification, err := store.Qualification(ctx, "evaluation-gap-recovery")
	if err != nil || !qualification.LossObserved || qualification.UnresolvedObservations != 1 ||
		qualification.LatestIntervalValid {
		t.Fatalf("initial gap qualification=%#v error=%v", qualification, err)
	}
	assertEvaluationRecorderDatasetPending(t, ctx, pool)
	second := evaluationRecorderIntegrationObservations(now.Add(5*time.Minute), 200, 4)
	if err = store.Observe(ctx, "evaluation-gap-session", now.Add(5*time.Minute), true, second); err != nil {
		t.Fatal(err)
	}
	qualification, err = store.Qualification(ctx, "evaluation-gap-recovery")
	if err != nil || !qualification.LossObserved || qualification.UnresolvedObservations != 0 ||
		!qualification.LatestIntervalValid || qualification.ValidSeconds != 300 {
		t.Fatalf("recovered gap qualification=%#v error=%v", qualification, err)
	}
	assertEvaluationRecorderDatasetRecovered(t, ctx, pool)
	if _, err = pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='COMPLETED',completed_at=$2,
updated_at=$2 WHERE campaign_id=$1`, "evaluation-gap-recovery", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE evaluation_campaigns SET state='PARTIAL',current_stage=NULL,
reason_code='TEST_COMPLETE',updated_at=$2 WHERE id=$1`, "evaluation-gap-recovery", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func assertEvaluationRecorderDatasetPending(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	selectionTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = selectionTx.Rollback(ctx) }()
	_, selectionErr := evaluationRecorderDatasetIDs(ctx, selectionTx, "evaluation-gap-recovery")
	if !errors.Is(selectionErr, errEvaluationInputsPending) {
		t.Fatalf("unrecovered dataset selection error=%v", selectionErr)
	}
}

func assertEvaluationRecorderDatasetRecovered(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	selectionTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = selectionTx.Rollback(ctx) }()
	datasetIDs, selectionErr := evaluationRecorderDatasetIDs(ctx, selectionTx, "evaluation-gap-recovery")
	if selectionErr != nil || len(datasetIDs) != 2 ||
		datasetIDs[0] != "binance-public-recording-evaluation-gap-session" ||
		datasetIDs[1] != "bybit-public-recording-evaluation-gap-session" {
		t.Fatalf("recovered dataset selection ids=%v error=%v", datasetIDs, selectionErr)
	}
}

func evaluationRecorderIntegrationObservations(at time.Time, messages, binanceETHUSDTGaps uint64,
) []EvaluationRecorderInstrumentObservation {
	values := make([]EvaluationRecorderInstrumentObservation, 0, 6)
	for _, exchange := range []string{"binance", "bybit"} {
		for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
			gaps := uint64(0)
			if exchange == "binance" && instrument == "ETHUSDT" {
				gaps = binanceETHUSDTGaps
			}
			values = append(values, EvaluationRecorderInstrumentObservation{ExchangeID: exchange,
				Instrument: instrument, Eligible: true, BookFresh: true, ClockEligible: true,
				LatestEventAt: at, Messages: messages, Gaps: gaps})
		}
	}
	return values
}

func assertHistoricalDatasetRegistration(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	insertEvaluationCampaignFixture(t, ctx, pool, "evaluation-history-catalog", "PARTIAL", now)
	windowStart := time.Date(2023, 8, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_historical_imports(
id,campaign_id,exchange_id,instrument,interval,window_start,window_end,state,checkpoint_time,
session_id,recorder_dataset_id,created_at,updated_at)
VALUES('historical-import-catalog','evaluation-history-catalog','binance','BTC/USDT','15m',$1,$2,
'RUNNING',$1,'evalhist-catalog','evalhistdataset-catalog',$3,$3)`, windowStart, windowEnd, now); err != nil {
		t.Fatal(err)
	}
	clock, _ := domain.NewReplayClock(now.Add(time.Second))
	store, err := NewEvaluationHistoricalSegmentStore(pool, clock)
	if err != nil {
		t.Fatal(err)
	}
	wire := evaluationHistoricalSegmentManifest("evalhist-catalog-wire", "market-wire.v1", "wire", "wire", now)
	canonical := evaluationHistoricalSegmentManifest("evalhist-catalog-canonical", "market-canonical.v1",
		"binance-historical-candle.v1", "axiom-historical-candle-page.v1", now)
	if err = store.CommitHistoricalSegment(ctx, "historical-import-catalog", windowStart, "wire", wire); err != nil {
		t.Fatal(err)
	}
	if err = store.CommitHistoricalSegment(ctx, "historical-import-catalog", windowStart, "canonical", canonical); err != nil {
		t.Fatal(err)
	}
	// A recovered commit must prove the same immutable identity rather than
	// creating a second catalogue row.
	if err = store.CommitHistoricalSegment(ctx, "historical-import-catalog", windowStart, "wire", wire); err != nil {
		t.Fatal(err)
	}
	manifest := recorder.DatasetManifest{SchemaVersion: "axiom.dataset.v1", DatasetID: "evalhistdataset-catalog",
		SessionID: "evalhist-catalog", Exchange: "binance", Revision: 1, CreatedAt: now,
		Segments:       []recorder.SegmentReference{{Kind: "wire", Manifest: wire}, {Kind: "canonical", Manifest: canonical}},
		RawRecordCount: 1, CanonicalCount: 1, Complete: true, Hash: strings.Repeat("a", 64)}
	catalog, err := NewRecordedDatasetCatalog(pool)
	if err != nil {
		t.Fatal(err)
	}
	datasetID, err := catalog.Register(ctx, manifest, strings.Repeat("b", 40))
	if err != nil {
		t.Fatalf("historical dataset registration failed: %v", err)
	}
	if repeated, repeatErr := catalog.Register(ctx, manifest, strings.Repeat("b", 40)); repeatErr != nil || repeated != datasetID {
		t.Fatalf("historical dataset registration replay id=%s error=%v", repeated, repeatErr)
	}
	assertHistoricalDatasetRows(t, ctx, pool, datasetID)
}

func assertHistoricalDatasetRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, datasetID string) {
	t.Helper()
	var registeredSegments int
	var instruments, eventTypes string
	if err := pool.QueryRow(ctx, `SELECT count(*),string_agg(DISTINCT instrument_id,','),
string_agg(event_type,',' ORDER BY event_type) FROM market_data_segments
WHERE recorder_session='evalhist-catalog'`).Scan(&registeredSegments, &instruments, &eventTypes); err != nil ||
		registeredSegments != 2 || instruments != "instrument-BTC-USDT" ||
		eventTypes != "historical_candle_canonical,historical_candle_wire" {
		t.Fatalf("historical market segments count=%d instruments=%s events=%s error=%v",
			registeredSegments, instruments, eventTypes, err)
	}
	var datasetSegments int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dataset_segments WHERE dataset_id=$1`, datasetID).
		Scan(&datasetSegments); err != nil || datasetSegments != 2 {
		t.Fatalf("historical dataset segments=%d error=%v", datasetSegments, err)
	}
}

func evaluationHistoricalSegmentManifest(name, schema, parser, normalizer string,
	now time.Time) segments.Manifest {
	return segments.Manifest{Spec: segments.Spec{Name: name, SchemaVersion: schema, ParserVersion: parser,
		NormalizationVersion: normalizer, OrderedContentHash: strings.Repeat("c", 64), FirstOrdinal: 1,
		LastOrdinal: 1, RecordCount: 1, StartedAt: now, EndedAt: now}, Path: name + ".parquet",
		Checksum: strings.Repeat("d", 64), OrderedContentHash: strings.Repeat("c", 64),
		Size: 128, Format: "parquet", Compression: "zstd"}
}

func assertNonCampaignRecorderObservationNoop(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	clock, _ := domain.NewReplayClock(now)
	store, err := NewEvaluationRecorderControlStore(pool, clock)
	if err != nil {
		t.Fatal(err)
	}
	baseline := []EvaluationRecorderInstrumentObservation{
		{ExchangeID: "binance", Instrument: "BTCUSDT", LatestEventAt: now},
		{ExchangeID: "binance", Instrument: "ETHUSDT", LatestEventAt: now},
	}
	if err = store.Observe(ctx, "non-campaign-recorder", now, true, baseline); err != nil {
		t.Fatalf("non-campaign recorder observation failed: %v", err)
	}
	var observations int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM evaluation_recorder_observations`).Scan(&observations); err != nil ||
		observations != 0 {
		t.Fatalf("non-campaign recorder persisted observations=%d error=%v", observations, err)
	}
}

func assertEvaluationMarketReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var assetCount int
	var markets string
	err := pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM assets WHERE symbol IN ('USDT','BTC','ETH')),
(SELECT string_agg(base_asset||'/'||quote_asset,',' ORDER BY base_asset,quote_asset)
 FROM instruments WHERE product='spot' AND
 ((base_asset='BTC' AND quote_asset='USDT') OR
  (base_asset='ETH' AND quote_asset IN ('BTC','USDT'))))`).Scan(&assetCount, &markets)
	if err != nil || assetCount != 3 || markets != "BTC/USDT,ETH/BTC,ETH/USDT" {
		t.Fatalf("evaluation market references assets=%d markets=%s error=%v", assetCount, markets, err)
	}
}

func ensureEvaluationTestRoles(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, role := range []string{"axiom_app", "axiom_recorder", "axiom_readonly"} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			if _, err := pool.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertStandaloneEvaluationAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_data_audits(
id,state,baseline_at,created_at,updated_at) VALUES('evaluation-standalone-audit','PENDING',$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	auditClock, _ := domain.NewReplayClock(now.Add(time.Second))
	auditWorker, err := NewEvaluationDataAuditCoordinator(pool, "evaluation-audit-worker", t.TempDir(), auditClock)
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := auditWorker.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("standalone data audit worked=%t error=%v", worked, runErr)
	}
	var standaloneAuditState string
	if err = pool.QueryRow(ctx, `SELECT state FROM evaluation_data_audits
WHERE id='evaluation-standalone-audit'`).Scan(&standaloneAuditState); err != nil || standaloneAuditState != "COMPLETED" {
		t.Fatalf("standalone data audit state=%s error=%v", standaloneAuditState, err)
	}
}

func assertEvaluationCandidateLockAndShadowIsolation(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	insertEvaluationCampaignFixture(t, ctx, pool, "evaluation-terminal-a", "BLOCKED", now)
	insertEvaluationCampaignFixture(t, ctx, pool, "evaluation-terminal-b", "COMPLETED", now.Add(time.Second))
	insertEvaluationMemberFixture(t, ctx, pool, "evaluation-terminal-a", "member-a", now)
	insertEvaluationMemberFixture(t, ctx, pool, "evaluation-terminal-b", "member-b", now)
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaign_candidate_locks(
campaign_id,strategy_id,state,reason_code,locked_at)
VALUES('evaluation-terminal-a','trend-following','BLOCKED','VALIDATION_EVIDENCE_INCOMPLETE',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE evaluation_campaign_candidate_locks
SET reason_code='MUTATED' WHERE campaign_id='evaluation-terminal-a' AND strategy_id='trend-following'`); err == nil {
		t.Fatal("immutable candidate lock was updated")
	}
	for _, campaignID := range []string{"evaluation-terminal-a", "evaluation-terminal-b"} {
		if _, err := pool.Exec(ctx, `INSERT INTO evaluation_shadow_sessions(campaign_id,recorder_session_id,
state,start_ordinal,last_processed_ordinal,reason_code,started_at,updated_at)
VALUES($1,$1||'-recorder','BLOCKED',0,0,'TEST_TERMINAL',$2,$2)`, campaignID, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_shadow_member_checkpoints(campaign_id,member_id,
strategy_id,state,reason_code,updated_at) VALUES('evaluation-terminal-a','member-a',
'trend-following','FAILED','STRATEGY_FAILED',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_shadow_member_checkpoints(campaign_id,member_id,
strategy_id,state,reason_code,updated_at) VALUES('evaluation-terminal-a','member-b',
'trend-following','FAILED','STRATEGY_FAILED',$1)`, now); err == nil {
		t.Fatal("shadow member from another campaign crossed the composite ownership boundary")
	}
}

func assertEvaluationPartialReportEvidence(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	hash := make([]byte, 32)
	hash[0] = 1
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaign_events(campaign_id,ordinal,event_type,
stage,reason_code,summary,occurred_at) VALUES('evaluation-terminal-a',0,'campaign_blocked',
'COMBINED_SHADOW','STRATEGY_FAILED','One member failed; preserved partial evidence.',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaign_reports(campaign_id,state,verdict,
reason_code,summary,report_hash,canonical_payload,generated_at) VALUES('evaluation-terminal-a',
'partial','BLOCKED','STRATEGY_FAILED','Preserved partial campaign evidence.',$1,'{"state":"partial"}',$2)`,
		hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE evaluation_campaign_reports SET summary='mutated'
WHERE campaign_id='evaluation-terminal-a'`); err == nil {
		t.Fatal("immutable evaluation report was updated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM evaluation_campaign_reports
WHERE campaign_id='evaluation-terminal-a'`); err == nil {
		t.Fatal("immutable evaluation report was deleted")
	}
	var reportState, memberState, reason string
	if err := pool.QueryRow(ctx, `SELECT report.state,member.state,event.reason_code
FROM evaluation_campaign_reports report
JOIN evaluation_campaign_members member ON member.campaign_id=report.campaign_id AND member.id='member-a'
JOIN evaluation_campaign_events event ON event.campaign_id=report.campaign_id AND event.ordinal=0
WHERE report.campaign_id='evaluation-terminal-a'`).Scan(&reportState, &memberState, &reason); err != nil ||
		reportState != "partial" || memberState != "FAILED" || reason != "STRATEGY_FAILED" {
		t.Fatalf("partial failure evidence report=%s member=%s reason=%s error=%v",
			reportState, memberState, reason, err)
	}
}

func assertEvaluationRestartClaim(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	insertEvaluationCampaignFixture(t, ctx, pool, "evaluation-restart", "PENDING", now.Add(2*time.Second))
	firstClock, _ := domain.NewReplayClock(now.Add(3 * time.Second))
	firstStore, _ := NewEvaluationWorkerStore(pool, "evaluation-worker-before-restart", firstClock)
	first, claimed, err := firstStore.Claim(ctx)
	if err != nil || !claimed || first.ClaimEpoch != 1 {
		t.Fatalf("first evaluation claim=%+v claimed=%t error=%v", first, claimed, err)
	}
	afterRestart, _ := domain.NewReplayClock(now.Add(34 * time.Second))
	restartedStore, _ := NewEvaluationWorkerStore(pool, "evaluation-worker-after-restart", afterRestart)
	second, claimed, err := restartedStore.Claim(ctx)
	if err != nil || !claimed || second.Campaign.ID != first.Campaign.ID || second.ClaimEpoch != 2 {
		t.Fatalf("restart evaluation claim=%+v claimed=%t error=%v", second, claimed, err)
	}
	if err = firstStore.Renew(ctx, first); err == nil {
		t.Fatal("expired pre-restart evaluation lease was renewed")
	}
}

func assertEvaluationCampaignRoleGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var recorderRead, recorderProgressWrite, recorderForbiddenWrite, recorderEvidenceInsert, recorderReportInsert bool
	err := pool.QueryRow(ctx, `SELECT
has_table_privilege('axiom_recorder','evaluation_campaigns','SELECT'),
has_column_privilege('axiom_recorder','evaluation_campaigns','campaign_recorded_bytes','UPDATE'),
has_column_privilege('axiom_recorder','evaluation_campaigns','state','UPDATE'),
has_table_privilege('axiom_recorder','evaluation_recorder_observations','INSERT'),
has_table_privilege('axiom_recorder','evaluation_campaign_reports','INSERT')`).Scan(
		&recorderRead, &recorderProgressWrite, &recorderForbiddenWrite, &recorderEvidenceInsert, &recorderReportInsert)
	if err != nil || !recorderRead || !recorderProgressWrite || recorderForbiddenWrite ||
		!recorderEvidenceInsert || recorderReportInsert {
		t.Fatalf("evaluation recorder grants read=%t progress=%t forbidden=%t evidence=%t report=%t error=%v",
			recorderRead, recorderProgressWrite, recorderForbiddenWrite, recorderEvidenceInsert,
			recorderReportInsert, err)
	}
}

func insertEvaluationCampaignFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	id, state string, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaigns(id,preset,state,reason_code,created_at,updated_at)
VALUES($1,'balanced_full_v1',$2,CASE WHEN $2='BLOCKED' THEN 'TEST_TERMINAL' END,$3,$3)`,
		id, state, now); err != nil {
		t.Fatal(err)
	}
}

func insertEvaluationMemberFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	campaignID, memberID string, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_campaign_members(campaign_id,id,strategy_id,
configuration_key,mode,capital_micros,repeat_ordinal,cost_stress_bps,state,reason_code,created_at,updated_at)
VALUES($1,$2,'trend-following','balanced-trend-1','shadow',2000000000,0,10000,'FAILED',
'STRATEGY_FAILED',$3,$3)`, campaignID, memberID, now); err != nil {
		t.Fatal(err)
	}
}

func openEvaluationCampaignTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_evaluation_campaign_test") {
		t.Fatal("evaluation campaign integration requires a dedicated database ending _evaluation_campaign_test")
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
