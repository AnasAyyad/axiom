package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB2PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB2TestDatabase(t, "AXIOM_B2_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("clean migrations=%d/%d error=%v", applied, len(migrations), applyErr)
	}
	assertB2SchemaAndPersistence(t, ctx, pool)
}

func TestB2PostgresB1ToB2UpgradeQualification(t *testing.T) {
	ctx, pool := openB2TestDatabase(t, "AXIOM_B2_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil || len(migrations) != 14 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureMigrationTable(ctx, connection); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, migration := range migrations[:13] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			connection.Release()
			t.Fatalf("B1 migration %s changed=%t error=%v", migration.Name, changed, applyErr)
		}
	}
	connection.Release()
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != 1 {
		t.Fatalf("B1-to-B2 migration=%d error=%v", applied, applyErr)
	}
	assertB2SchemaAndPersistence(t, ctx, pool)
}

func openB2TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b2_test") {
		t.Fatal("B2 integration requires a dedicated database ending _b2_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

func assertB2SchemaAndPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, "INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instruments(id,base_asset,quote_asset,product)
VALUES ('BTCUSDT','BTC','USDT','spot') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	view := b2CoherentFixture(t, now)
	repository, err := NewCoherentViewRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.Commit(ctx, view, now); err != nil {
		t.Fatalf("commit view: %v", err)
	}
	restored, err := repository.Load(ctx, view.Identity())
	if err != nil || restored.VersionVectorHash() != view.VersionVectorHash() || len(restored.Members()) != 2 {
		t.Fatalf("restored view=%s members=%d error=%v", restored.VersionVectorHash(), len(restored.Members()), err)
	}
	if _, err = pool.Exec(ctx, "UPDATE cross_market_view_headers SET member_count=3 WHERE id=$1", view.Identity()); err == nil {
		t.Fatal("immutable coherent header accepted update")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM cross_market_view_members
WHERE cross_market_view_id=$1 AND member_ordinal=0`, view.Identity()); err == nil {
		t.Fatal("immutable coherent member accepted delete")
	}
	assertB2IncompleteViewRejected(t, ctx, pool, now)
	assertB2PostTriggerOrdinalRejected(t, ctx, pool, now)
	assertB2DecisionViewRequired(t, ctx, pool, view.Identity(), now)
	assertB2TierACompleteness(t, ctx, pool, now)
	assertB2TierACatalogRoundTrip(t, ctx, pool, now)
	assertB2RoleGrants(t, ctx, pool)
}

func assertB2TierACatalogRoundTrip(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	root := t.TempDir()
	ordinals := &runtimecore.IngestOrdinals{}
	profile := marketrecorder.CollectorProfile{Instance: "catalog-collector", Region: "test-region",
		MinimumReaderVersion: "dataset-reader.v2"}
	manifests := make([]marketrecorder.DatasetManifest, 0, 2)
	roots := make(map[string]string, 2)
	catalog, err := NewA11DatasetCatalog(pool)
	if err != nil {
		t.Fatal(err)
	}
	for index, exchange := range []string{"binance", "bybit"} {
		exchangeRoot := root + "/" + exchange
		manifest := registerB2CatalogCandidate(t, ctx, pool, catalog, ordinals, profile,
			exchangeRoot, exchange, index, now)
		manifests = append(manifests, manifest)
		roots[exchange] = exchangeRoot
	}
	tierA, err := marketrecorder.BuildTierAManifest("catalog-tier-a", now, roots, manifests)
	if err != nil {
		t.Fatal(err)
	}
	datasetID, err := catalog.RegisterTierA(ctx, tierA)
	if err != nil {
		t.Fatal(err)
	}
	if retriedID, retryErr := catalog.RegisterTierA(ctx, tierA); retryErr != nil || retriedID != datasetID {
		t.Fatalf("Tier A registration retry=%s error=%v; want %s", retriedID, retryErr, datasetID)
	}
	var state, quality string
	if err = pool.QueryRow(ctx, `SELECT state,quality_tier FROM dataset_manifests WHERE id=$1`, datasetID).
		Scan(&state, &quality); err != nil || state != "qualified" || quality != "A" {
		t.Fatalf("Tier A catalog state=%s quality=%s error=%v", state, quality, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM dataset_tier_a_members WHERE dataset_id=$1`, datasetID); err == nil {
		t.Fatal("immutable Tier A member accepted delete")
	}
}

func registerB2CatalogCandidate(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	catalog *A11DatasetCatalog,
	ordinals *runtimecore.IngestOrdinals,
	profile marketrecorder.CollectorProfile,
	root, exchange string,
	index int,
	now time.Time,
) marketrecorder.DatasetManifest {
	t.Helper()
	session := "b2-catalog-" + exchange
	recording, err := marketrecorder.NewB2(root, "catalog-"+exchange, session, exchange, ordinals,
		catalogSegmentCommitter(ctx, pool, session, exchange), nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	received := now.Add(time.Duration(index) * time.Millisecond)
	link, err := recording.RecordRaw(marketrecorder.RawInput{Exchange: exchange,
		EventType: marketrecorder.EventSnapshot, Instrument: instrument, SessionID: session,
		ConnectionID: "catalog-connection", ConnectionGeneration: 1, MonotonicOffsetNanos: uint64(index + 1),
		RecordedLogicalTime: uint64(index + 1), ReceivedAt: received, Payload: []byte(`{"snapshot":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = recording.RecordCanonical(marketrecorder.CanonicalInput{Link: link,
		EventID: "catalog-event-" + exchange, ParserVersion: "catalog-parser.v1",
		NormalizationVersion: "catalog-normalizer.v1", Canonical: []byte(`{"snapshot":true}`)}); err != nil {
		t.Fatal(err)
	}
	manifest, err := recording.Flush()
	if err != nil {
		t.Fatal(err)
	}
	registeredID, err := catalog.Register(ctx, manifest, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if retriedID, retryErr := catalog.Register(ctx, manifest, strings.Repeat("a", 40)); retryErr != nil || retriedID != registeredID {
		t.Fatalf("candidate registration retry=%s error=%v; want %s", retriedID, retryErr, registeredID)
	}
	return manifest
}

func catalogSegmentCommitter(
	ctx context.Context,
	pool *pgxpool.Pool,
	session, exchange string,
) segments.Committer {
	return func(manifest segments.Manifest) error {
		_, err := pool.Exec(ctx, `INSERT INTO market_data_segments
(id,recorder_session,exchange_id,event_type,schema_version,parser_version,normalization_version,
 compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
VALUES ($1,$2,$3,'mixed_public',$4,$5,$6,'zstd',$7,$8,$9,$10,$11,$12,$13,$14,'ready',$14)`,
			manifest.Spec.Name, session, exchange, manifest.Spec.SchemaVersion, manifest.Spec.ParserVersion,
			manifest.Spec.NormalizationVersion, manifest.Path, manifest.Checksum, manifest.OrderedContentHash,
			int64(manifest.Spec.RecordCount), int64(manifest.Spec.FirstOrdinal), int64(manifest.Spec.LastOrdinal),
			manifest.Spec.StartedAt, manifest.Spec.EndedAt)
		return err
	}
}

func assertB2DecisionViewRequired(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	viewID string,
	now time.Time,
) {
	t.Helper()
	hash := strings.Repeat("8", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO configuration_versions(id,version,configuration_hash,canonical_payload,actor,recorded_at)
VALUES ('b2-configuration',1,$1,'{}','b2-test',$2)`, []any{hash, now}},
		{`INSERT INTO strategy_definitions(id,name,family)
VALUES ('b2-strategy','b2-cross-market','cross_exchange_arbitrage')`, nil},
		{`INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,promotion_status,created_at)
VALUES ('b2-strategy-version','b2-strategy',1,$1,'research',$2)`, []any{hash, now}},
		{`INSERT INTO runs(id,mode,configuration_id,strategy_version_id,root_seed_hash,reproducibility_hash,state,created_at)
VALUES ('b2-run','shadow','b2-configuration','b2-strategy-version',$1,$1,'created',$2)`, []any{hash, now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO decisions
(id,run_id,configuration_id,strategy_version_id,outcome,reason_code,causation_id,decided_at,ingest_ordinal,decision_market_scope)
VALUES ('b2-decision-missing','b2-run','b2-configuration','b2-strategy-version','accepted','fixture','cause',$1,1,'cross_market')`,
		now); err == nil {
		t.Fatal("cross-market decision without coherent view committed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO decisions
(id,run_id,configuration_id,strategy_version_id,outcome,reason_code,causation_id,decided_at,ingest_ordinal,
 decision_market_scope,cross_market_view_id)
VALUES ('b2-decision','b2-run','b2-configuration','b2-strategy-version','accepted','fixture','cause',$1,2,
 'cross_market',$2)`, now, viewID); err != nil {
		t.Fatalf("cross-market decision with coherent view rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE decisions SET reason_code='mutated' WHERE id='b2-decision'"); err == nil {
		t.Fatal("immutable cross-market decision accepted update")
	}
}

func b2CoherentFixture(t *testing.T, now time.Time) runtimecore.CoherentView {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	views := runtimecore.NewMarketViews()
	keys := []runtimecore.MarketKey{{Exchange: "binance", Instrument: instrument}, {Exchange: "bybit", Instrument: instrument}}
	for index, key := range keys {
		if err = views.ActivateGeneration(key, 1); err != nil {
			t.Fatal(err)
		}
		_, err = views.Publish(runtimecore.MarketViewInput{Key: key, BookVersion: 1, ConnectionGeneration: 1,
			ReceiveMonotonicNanos: uint64(100 + index*50), ReceiveUTC: now.Add(time.Duration(index) * 50 * time.Nanosecond),
			IngestOrdinal: uint64(index + 1), ClockUncertainty: 100 * time.Nanosecond,
			StateHash: strings.Repeat(string(rune('a'+index)), 64), CollectorInstance: "collector-1",
			CollectorRegion: "test-region"})
		if err != nil {
			t.Fatal(err)
		}
	}
	joined, err := views.CoherentAsOf(keys, runtimecore.AsOfTrigger{MonotonicNanos: 200,
		IngestOrdinal: 3, UTC: now.Add(200 * time.Nanosecond)}, runtimecore.InitialB2CoherentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return joined
}

func assertB2IncompleteViewRejected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	hash := strings.Repeat("c", 64)
	_, err = tx.Exec(ctx, `INSERT INTO cross_market_view_headers
(id,version_vector_hash,policy_version,maximum_book_age_nanos,maximum_inter_book_skew_nanos,
 maximum_clock_uncertainty_nanos,trigger_monotonic_nanos,trigger_ingest_ordinal,trigger_utc,trigger_utc_unix_nanos,
 member_count,created_at)
VALUES ($1,$1,'axiom.coherent-view-policy.v1',250000000,250000000,100000000,200,3,$2,$3,2,$2)`,
		hash, now, now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("incomplete coherent view committed")
	}
}

func assertB2PostTriggerOrdinalRejected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	hash := strings.Repeat("7", 64)
	if _, err = tx.Exec(ctx, `INSERT INTO cross_market_view_headers
(id,version_vector_hash,policy_version,maximum_book_age_nanos,maximum_inter_book_skew_nanos,
 maximum_clock_uncertainty_nanos,trigger_monotonic_nanos,trigger_ingest_ordinal,trigger_utc,trigger_utc_unix_nanos,
 member_count,created_at)
VALUES ($1,$1,'axiom.coherent-view-policy.v1',250000000,250000000,100000000,200,3,$2,$3,2,$2)`,
		hash, now, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	for ordinal, exchange := range []string{"binance", "bybit"} {
		ingestOrdinal := ordinal + 1
		if ordinal == 1 {
			ingestOrdinal = 4
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cross_market_view_members
(cross_market_view_id,member_ordinal,exchange_id,instrument_id,book_version,connection_generation,
 receive_monotonic_nanos,receive_utc,receive_utc_unix_nanos,ingest_ordinal,clock_offset_nanos,clock_uncertainty_nanos,
 clock_interval_start,clock_interval_end,state_hash,collector_instance,collector_region)
VALUES ($1,$2,$3,'BTCUSDT',1,1,$4,$5,$6,$7,0,0,$5,$5,$8,'collector-1','test-region')`,
			hash, ordinal, exchange, 100+ordinal*50, now, now.UnixNano(), ingestOrdinal,
			strings.Repeat(string(rune('4'+ordinal)), 64)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "cross_market_view_ineligible") {
		t.Fatalf("post-trigger ingest ordinal committed: %v", err)
	}
}

func assertB2TierACompleteness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	insertB2TierAChildren(t, ctx, pool, now)
	manifestHash := strings.Repeat("f", 64)
	_, err := pool.Exec(ctx, `INSERT INTO dataset_manifests
(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,manifest_schema_version,quality_tier)
VALUES ('b2-tier-a',$1,'market-wire.v1',$2,$2,'building',$2,'axiom.multi-exchange-dataset.v1','A')`, manifestHash, now)
	if err != nil {
		t.Fatal(err)
	}
	insertB2TierAEvidence(t, ctx, pool, now)
	if _, err = pool.Exec(ctx, "UPDATE dataset_manifests SET state='ready' WHERE id='b2-tier-a'"); err != nil {
		t.Fatalf("complete Tier A manifest rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES ('b2-tier-a','b2-segment-binance',99)`); err == nil || !strings.Contains(err.Error(), "dataset_evidence_immutable") {
		t.Fatalf("ready Tier A accepted late evidence: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO dataset_manifests
(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,manifest_schema_version,quality_tier)
VALUES ('b2-tier-a-incomplete',$1,'market-wire.v1',$2,$2,'building',$2,'axiom.multi-exchange-dataset.v1','A')`,
		strings.Repeat("9", 64), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE dataset_manifests SET state='ready' WHERE id='b2-tier-a-incomplete'"); err == nil {
		t.Fatal("incomplete Tier A manifest qualified")
	}
}

func insertB2TierAChildren(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	for index, exchange := range []string{"binance", "bybit"} {
		segmentID := "b2-segment-" + exchange
		hash := strings.Repeat(string(rune('d'+index)), 64)
		_, err := pool.Exec(ctx, `INSERT INTO market_data_segments
(id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,
 normalization_version,compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,
 started_at,ended_at,state,finalized_at)
VALUES ($1,'b2-session',$2,'BTCUSDT','depth','market-wire.v1','parser.v1','normalization.v1',
 'zstd',$3,$4,$4,1,$5,$5,$6,$6,'ready',$6)`, segmentID, exchange, "b2/"+exchange+".zst", hash, index+1, now)
		if err != nil {
			t.Fatal(err)
		}
		childID := "b2-child-" + exchange
		_, err = pool.Exec(ctx, `INSERT INTO dataset_manifests
(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,manifest_schema_version)
VALUES ($1,$2,'market-wire.v1',$3,$3,'building',$3,'axiom.dataset.v2')`, childID,
			strings.Repeat(string(rune('1'+index)), 64), now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES ($1,$2,0)`, childID, segmentID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, "UPDATE dataset_manifests SET state='ready' WHERE id=$1", childID); err != nil {
			t.Fatal(err)
		}
	}
}

func insertB2TierAEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	for index, exchange := range []string{"binance", "bybit"} {
		if _, err := pool.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES ('b2-tier-a',$1,$2)`, "b2-segment-"+exchange, index); err != nil {
			t.Fatal(err)
		}
		_, err := pool.Exec(ctx, `INSERT INTO dataset_tier_a_members
(dataset_id,member_ordinal,exchange_id,member_dataset_id,member_manifest_hash,member_revision,replay_hash,record_count)
VALUES ('b2-tier-a',$1,$2,$3,$4,1,$5,1)`, index, exchange, "b2-child-"+exchange,
			strings.Repeat(string(rune('1'+index)), 64), strings.Repeat(string(rune('3'+index)), 64))
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO dataset_exchange_coverage
(dataset_id,exchange_id,collector_instance,collector_region,coverage_start,coverage_end,first_ordinal,last_ordinal,
 generation_history,schema_versions,parser_versions,normalization_versions,compatibility_requirements,
 raw_record_count,canonical_record_count,raw_canonical_linkage_complete,hidden_gap_count,complete)
VALUES ('b2-tier-a',$1,'collector-1','test-region',$2,$2,$3,$3,'[{"generation":1}]',
 ARRAY['market-wire.v1'],ARRAY['parser.v1'],ARRAY['normalization.v1'],'{"minimum_reader":"market-wire.v1"}',
 1,1,true,0,true)`, exchange, now, index+1)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertB2RoleGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	roles := []string{"axiom_b2_runtime", "axiom_b2_recorder", "axiom_b2_readonly"}
	for _, role := range roles {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			if _, err := pool.Exec(ctx, "CREATE ROLE "+role); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := ApplyRoleGrants(ctx, pool, roles[0], roles[1], roles[2]); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		role, table, privilege string
		want                   bool
	}{
		{roles[0], "cross_market_view_headers", "INSERT", true},
		{roles[0], "cross_market_view_headers", "UPDATE", false},
		{roles[1], "dataset_exchange_coverage", "INSERT", true},
		{roles[1], "dataset_exchange_coverage", "UPDATE", false},
		{roles[2], "cross_market_view_members", "SELECT", true},
	}
	for _, check := range checks {
		var allowed bool
		if err := pool.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)", check.role, check.table,
			check.privilege).Scan(&allowed); err != nil || allowed != check.want {
			t.Fatalf("role=%s table=%s privilege=%s allowed=%t error=%v",
				check.role, check.table, check.privilege, allowed, err)
		}
	}
}
