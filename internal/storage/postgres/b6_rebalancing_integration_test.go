package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB6PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB6TestDatabase(t, "AXIOM_B6_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 18)
	assertB6SchemaAndPersistence(t, ctx, pool)
}

func TestB6PostgresB5ToB6UpgradeQualification(t *testing.T) {
	ctx, pool := openB6TestDatabase(t, "AXIOM_B6_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 17)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 18 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[17])
	if err != nil || !changed {
		t.Fatalf("B5-to-B6 migration changed=%t error=%v", changed, err)
	}
	assertB6SchemaAndPersistence(t, ctx, pool)
}

func openB6TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b6_test") {
		t.Fatal("B6 integration requires a dedicated database ending _b6_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

func assertB6SchemaAndPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	configurationHash := seedB6References(t, ctx, pool, now)
	repository, err := NewB6Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	write := b6FactSetWrite("b6-facts-good", configurationHash, now, true)
	if err = repository.RecordFactSet(ctx, write); err != nil {
		t.Fatal(err)
	}
	mismatched := b6FactSetWrite("b6-facts-mismatch", strings.Repeat("0", 64), now, true)
	if err = repository.RecordFactSet(ctx, mismatched); err == nil {
		t.Fatal("mismatched registered configuration hash persisted")
	}
	recommendation := b6RecommendationWrite(write, configurationHash, now)
	if err = repository.RecordRecommendation(ctx, recommendation); err != nil {
		t.Fatal(err)
	}
	assertB6ReloadAndImmutability(t, ctx, pool, repository)

	unapproved := b6FactSetWrite("b6-facts-unapproved", configurationHash, now, false)
	if err = repository.RecordFactSet(ctx, unapproved); err != nil {
		t.Fatal(err)
	}
	rejected := b6RecommendationWrite(unapproved, configurationHash, now)
	rejected.Recommendation.ID = "b6-" + strings.Repeat("d", 24)
	rejected.Recommendation.RequestID = "request-b6-unapproved"
	rejected.Recommendation.FactSetID = unapproved.FactSet.ID
	rejected.Recommendation.FactSetHash = unapproved.FactSet.CanonicalHash
	for index := range rejected.Steps {
		rejected.Steps[index].RecommendationID = rejected.Recommendation.ID
		rejected.Steps[index].FactSetID = unapproved.FactSet.ID
		rejected.Steps[index].FactID = unapproved.Facts[index].FactID
		rejected.Steps[index].ProvenanceHash = unapproved.Facts[index].ProvenanceHash
	}
	for index := range rejected.Checklist {
		rejected.Checklist[index].RecommendationID = rejected.Recommendation.ID
	}
	if err = repository.RecordRecommendation(ctx, rejected); err == nil {
		t.Fatal("unapproved selected fact persisted")
	}
	assertB6RoleMatrix(t, ctx, pool)
}

func seedB6References(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) string {
	t.Helper()
	canonical, err := json.Marshal(config.DefaultV1BConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := a10PayloadHash(canonical)
	statements := []b4SeedStatement{
		{sql: `INSERT INTO configuration_versions (
id, version, configuration_hash, canonical_payload, actor, recorded_at
) VALUES ('configuration-b6',1,$1,$2,'b6-integration',$3)`,
			args: []any{configurationHash, canonical, now}},
		{sql: `INSERT INTO assets(symbol) VALUES ('BTC'),('USDT')`},
	}
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("B6 reference seed %d failed: %v", index+1, err)
		}
	}
	return configurationHash
}

func b6FactSetWrite(
	id, configurationHash string,
	now time.Time,
	approved bool,
) B6FactSetWrite {
	setHash := a10PayloadHash([]byte(id))
	provenanceHash := a10PayloadHash([]byte(id + "-transfer"))
	network := "BTC"
	actor := "risk-reviewer"
	reference := "AX-V1B-B06-TEST"
	approvedAt := pgTimestamp(now.Add(-30 * time.Minute))
	if !approved {
		actor = ""
		reference = ""
		approvedAt = pgtype.Timestamptz{}
	}
	var actorPointer, referencePointer *string
	if approved {
		actorPointer = &actor
		referencePointer = &reference
	}
	return B6FactSetWrite{
		FactSet: generated.InsertRebalancingFactSetParams{
			ID: id, ConfigurationID: "configuration-b6", ConfigurationHash: configurationHash,
			FactSchemaVersion: "rebalancing-fact.v1",
			CostModelVersion:  "rebalancing-cost.v1",
			CanonicalHash:     setHash, RecordedAt: pgTimestamp(now),
		},
		Facts: []generated.InsertRebalancingRouteFactParams{{
			FactSetID: id, FactID: id + "-transfer", LogicalKey: "transfer|binance@BTC|bybit@BTC|BTC",
			FactVersion: 1, FactKind: "transfer",
			FromExchangeID: "binance", FromAssetSymbol: "BTC",
			ToExchangeID: "bybit", ToAssetSymbol: "BTC",
			Network: &network, SourceChain: &network, DestinationChain: &network,
			MinimumQuantity: "0.0001", Available: true, WithdrawalAvailable: true,
			DepositAvailable: true, Compatible: true, Ambiguous: false,
			FeeCost: "1", SpreadCost: "2", DepthCost: "3", DelayCost: "4",
			NetworkFeeCost: "5", CompatibilityCost: "6", VolatilityRiskCost: "2",
			OperationalRiskCost: "2", MinimumDurationNanos: int64(time.Second),
			MaximumDurationNanos: int64(2 * time.Second), RiskScore: "0.1",
			Warnings:        []string{"manual_external_action_required"},
			ManualChecklist: []string{"confirm_exact_network_chain_compatibility"},
			Source:          "reviewed-fixture", Observer: "b6-integration",
			ObservedAt: pgTimestamp(now.Add(-time.Hour)),
			ExpiresAt:  pgTimestamp(now.Add(time.Hour)),
			Confidence: "0.95", Approved: approved, ApprovalActor: actorPointer,
			ApprovalReference: referencePointer, ApprovedAt: approvedAt,
			ProvenanceHash: provenanceHash,
		}},
	}
}

func b6RecommendationWrite(
	factSet B6FactSetWrite,
	configurationHash string,
	now time.Time,
) B6RecommendationWrite {
	id := "b6-" + strings.Repeat("c", 24)
	fact := factSet.Facts[0]
	return B6RecommendationWrite{
		Recommendation: generated.InsertRebalancingRecommendationParams{
			ID: id, RequestID: "request-b6-good", ConfigurationID: "configuration-b6",
			ConfigurationHash: configurationHash, FactSetID: factSet.FactSet.ID,
			FactSetHash: factSet.FactSet.CanonicalHash, Method: "reviewed_graph_route",
			SourceExchangeID: "binance", SourceAssetSymbol: "BTC",
			DestinationExchangeID: "bybit", DestinationAssetSymbol: "BTC",
			Quantity: "1", FeeCost: "1", SpreadCost: "2", DepthCost: "3",
			DelayCost: "4", NetworkFeeCost: "5", CompatibilityCost: "6",
			VolatilityRiskCost: "2", OperationalRiskCost: "2", TotalCost: "25",
			MinimumDurationNanos: int64(time.Second),
			MaximumDurationNanos: int64(2 * time.Second), RiskScore: "0.1",
			Warnings: []string{"manual_external_action_required"}, AdvisoryOnly: true,
			CanonicalHash: strings.Repeat("c", 64), RecordedAt: pgTimestamp(now),
		},
		Steps: []generated.InsertRebalancingRecommendationStepParams{{
			RecommendationID: id, StepIndex: 0, Role: "route",
			FactSetID: factSet.FactSet.ID, FactID: fact.FactID,
			FactVersion: fact.FactVersion, ProvenanceHash: fact.ProvenanceHash,
		}},
		Checklist: []generated.InsertRebalancingChecklistStepParams{
			{RecommendationID: id, StepIndex: 0, Instruction: "verify_facts_are_current_and_approved"},
			{RecommendationID: id, StepIndex: 1, Instruction: "confirm_inventory_and_reservations_have_not_changed"},
			{RecommendationID: id, StepIndex: 2, Instruction: "review_all_cost_duration_and_risk_components"},
			{RecommendationID: id, StepIndex: 3, Instruction: "record_operator_decision_and_reconcile_after_manual_action"},
		},
	}
}

func assertB6ReloadAndImmutability(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B6Repository,
) {
	t.Helper()
	factSet, facts, err := repository.LoadFactSet(ctx, "b6-facts-good")
	if err != nil || factSet.FactSchemaVersion != "rebalancing-fact.v1" ||
		len(facts) != 1 || facts[0].FactKind != "transfer" {
		t.Fatalf("B6 fact-set reload = %#v %#v %v", factSet, facts, err)
	}
	recommendation, steps, checklist, err := repository.LoadRecommendation(
		ctx, "b6-"+strings.Repeat("c", 24),
	)
	if err != nil || recommendation.Method != "reviewed_graph_route" ||
		!recommendation.AdvisoryOnly || len(steps) != 1 || len(checklist) != 4 {
		t.Fatalf("B6 recommendation reload = %#v %#v %#v %v",
			recommendation, steps, checklist, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE rebalancing_route_facts
SET available=false WHERE fact_set_id='b6-facts-good'`); err == nil {
		t.Fatal("immutable B6 fact mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM rebalancing_recommendations
WHERE id=$1`, recommendation.ID); err == nil {
		t.Fatal("immutable B6 recommendation deleted")
	}
}

func assertB6RoleMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_B6_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_B6_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_B6_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	var runtimeInsert, recorderSelect, readOnlySelect, readOnlyInsert bool
	if err := pool.QueryRow(ctx,
		"SELECT has_table_privilege($1,'rebalancing_recommendations','INSERT')",
		runtimeRole,
	).Scan(&runtimeInsert); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT has_table_privilege($1,'rebalancing_recommendations','SELECT')",
		recorderRole,
	).Scan(&recorderSelect); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT
has_table_privilege($1,'rebalancing_recommendations','SELECT'),
has_table_privilege($1,'rebalancing_recommendations','INSERT')`,
		readOnlyRole,
	).Scan(&readOnlySelect, &readOnlyInsert); err != nil {
		t.Fatal(err)
	}
	if !runtimeInsert || recorderSelect || !readOnlySelect || readOnlyInsert {
		t.Fatalf(
			"B6 role matrix runtime_insert=%t recorder_select=%t readonly_select=%t readonly_insert=%t",
			runtimeInsert, recorderSelect, readOnlySelect, readOnlyInsert,
		)
	}
}
