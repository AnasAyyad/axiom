package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"

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
	if err != nil || len(migrations) != 55 || migrations[53].Version != "000054" ||
		migrations[54].Version != "000055" {
		t.Fatalf("evaluation campaign migration catalog=%d error=%v", len(migrations), err)
	}
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 54)
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 1 {
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
	assertStandaloneEvaluationAudit(t, ctx, pool, now)
	assertEvaluationCandidateLockAndShadowIsolation(t, ctx, pool, now)
	assertEvaluationPartialReportEvidence(t, ctx, pool, now)
	assertEvaluationRestartClaim(t, ctx, pool, now)
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
