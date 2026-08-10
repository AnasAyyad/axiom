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

func TestInventoryRebalancingPostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openInventoryRebalancingTestDatabase(t, "AXIOM_INVENTORY_REBALANCING_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 18)
	assertInventoryRebalancingSchemaAndPersistence(t, ctx, pool)
}

func TestInventoryRebalancingPostgresCrossExchangeArbitrageToInventoryRebalancingUpgradeQualification(t *testing.T) {
	ctx, pool := openInventoryRebalancingTestDatabase(t, "AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 17)
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
		t.Fatalf("cross-exchange-to-rebalancing migration changed=%t error=%v", changed, err)
	}
	assertInventoryRebalancingSchemaAndPersistence(t, ctx, pool)
}

func openInventoryRebalancingTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_inventory_rebalancing_test") {
		t.Fatal("inventory rebalancing integration requires a dedicated database ending _inventory_rebalancing_test")
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

func assertInventoryRebalancingSchemaAndPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	configurationHash := seedInventoryRebalancingReferences(t, ctx, pool, now)
	repository, err := NewInventoryRebalancingRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	write := inventoryRebalancingFactSetWrite("inventory_rebalancing-facts-good", configurationHash, now, true)
	if err = repository.RecordFactSet(ctx, write); err != nil {
		t.Fatal(err)
	}
	mismatched := inventoryRebalancingFactSetWrite("inventory_rebalancing-facts-mismatch", strings.Repeat("0", 64), now, true)
	if err = repository.RecordFactSet(ctx, mismatched); err == nil {
		t.Fatal("mismatched registered configuration hash persisted")
	}
	recommendation := inventoryRebalancingRecommendationWrite(write, configurationHash, now)
	if err = repository.RecordRecommendation(ctx, recommendation); err != nil {
		t.Fatal(err)
	}
	assertInventoryRebalancingReloadAndImmutability(t, ctx, pool, repository)

	unapproved := inventoryRebalancingFactSetWrite("inventory_rebalancing-facts-unapproved", configurationHash, now, false)
	if err = repository.RecordFactSet(ctx, unapproved); err != nil {
		t.Fatal(err)
	}
	rejected := inventoryRebalancingRecommendationWrite(unapproved, configurationHash, now)
	rejected.Recommendation.ID = "inventory_rebalancing-" + strings.Repeat("d", 24)
	rejected.Recommendation.RequestID = "request-inventory_rebalancing-unapproved"
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
	assertInventoryRebalancingRoleMatrix(t, ctx, pool)
}

func seedInventoryRebalancingReferences(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) string {
	t.Helper()
	canonical, err := json.Marshal(config.DefaultMultiStrategyConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := researchRegistryPayloadHash(canonical)
	statements := []triangularArbitrageSeedStatement{
		{sql: `INSERT INTO configuration_versions (
id, version, configuration_hash, canonical_payload, actor, recorded_at
) VALUES ('configuration-inventory_rebalancing',1,$1,$2,'inventory_rebalancing-integration',$3)`,
			args: []any{configurationHash, canonical, now}},
		{sql: `INSERT INTO assets(symbol) VALUES ('BTC'),('USDT')`},
	}
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("inventory rebalancing reference seed %d failed: %v", index+1, err)
		}
	}
	return configurationHash
}

func inventoryRebalancingFactSetWrite(
	id, configurationHash string,
	now time.Time,
	approved bool,
) InventoryRebalancingFactSetWrite {
	setHash := researchRegistryPayloadHash([]byte(id))
	provenanceHash := researchRegistryPayloadHash([]byte(id + "-transfer"))
	network := "BTC"
	actorPointer, referencePointer, approvedAt := inventoryRebalancingApprovalFields(approved, now)
	return InventoryRebalancingFactSetWrite{
		FactSet: generated.InsertRebalancingFactSetParams{
			ID: id, ConfigurationID: "configuration-inventory_rebalancing", ConfigurationHash: configurationHash,
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
			Source:          "reviewed-fixture", Observer: "inventory_rebalancing-integration",
			ObservedAt: pgTimestamp(now.Add(-time.Hour)),
			ExpiresAt:  pgTimestamp(now.Add(time.Hour)),
			Confidence: "0.95", Approved: approved, ApprovalActor: actorPointer,
			ApprovalReference: referencePointer, ApprovedAt: approvedAt,
			ProvenanceHash: provenanceHash,
		}},
	}
}

func inventoryRebalancingApprovalFields(
	approved bool,
	now time.Time,
) (*string, *string, pgtype.Timestamptz) {
	actor := "risk-reviewer"
	reference := "AX-V1B-B06-TEST"
	approvedAt := pgTimestamp(now.Add(-30 * time.Minute))
	if !approved {
		return nil, nil, pgtype.Timestamptz{}
	}
	return &actor, &reference, approvedAt
}

func inventoryRebalancingRecommendationWrite(
	factSet InventoryRebalancingFactSetWrite,
	configurationHash string,
	now time.Time,
) InventoryRebalancingRecommendationWrite {
	id := "inventory_rebalancing-" + strings.Repeat("c", 24)
	fact := factSet.Facts[0]
	return InventoryRebalancingRecommendationWrite{
		Recommendation: generated.InsertRebalancingRecommendationParams{
			ID: id, RequestID: "request-inventory_rebalancing-good", ConfigurationID: "configuration-inventory_rebalancing",
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

func assertInventoryRebalancingReloadAndImmutability(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *InventoryRebalancingRepository,
) {
	t.Helper()
	factSet, facts, err := repository.LoadFactSet(ctx, "inventory_rebalancing-facts-good")
	if err != nil || factSet.FactSchemaVersion != "rebalancing-fact.v1" ||
		len(facts) != 1 || facts[0].FactKind != "transfer" {
		t.Fatalf("inventory rebalancing fact-set reload = %#v %#v %v", factSet, facts, err)
	}
	recommendation, steps, checklist, err := repository.LoadRecommendation(
		ctx, "inventory_rebalancing-"+strings.Repeat("c", 24),
	)
	if err != nil || recommendation.Method != "reviewed_graph_route" ||
		!recommendation.AdvisoryOnly || len(steps) != 1 || len(checklist) != 4 {
		t.Fatalf("inventory rebalancing recommendation reload = %#v %#v %#v %v",
			recommendation, steps, checklist, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE rebalancing_route_facts
SET available=false WHERE fact_set_id='inventory_rebalancing-facts-good'`); err == nil {
		t.Fatal("immutable inventory rebalancing fact mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM rebalancing_recommendations
WHERE id=$1`, recommendation.ID); err == nil {
		t.Fatal("immutable inventory rebalancing recommendation deleted")
	}
}

func assertInventoryRebalancingRoleMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_INVENTORY_REBALANCING_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_INVENTORY_REBALANCING_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_INVENTORY_REBALANCING_READONLY_ROLE", "axiom_readonly")
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
			"inventory rebalancing role matrix runtime_insert=%t recorder_select=%t readonly_select=%t readonly_insert=%t",
			runtimeInsert, recorderSelect, readOnlySelect, readOnlyInsert,
		)
	}
}
