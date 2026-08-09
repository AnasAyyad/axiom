package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTriangularArbitragePostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openTriangularArbitrageTestDatabase(t, "AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 16)
	assertTriangularArbitrageSchemaAndPersistence(t, ctx, pool)
}

func TestTriangularArbitragePostgresMeanReversionToTriangularArbitrageUpgradeQualification(t *testing.T) {
	ctx, pool := openTriangularArbitrageTestDatabase(t, "AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 15)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 16 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[15])
	if err != nil || !changed {
		t.Fatalf("mean-reversion-to-triangular migration changed=%t error=%v", changed, err)
	}
	assertTriangularArbitrageSchemaAndPersistence(t, ctx, pool)
}

func openTriangularArbitrageTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_triangular_arbitrage_test") {
		t.Fatal("triangular arbitrage integration requires a dedicated database ending _triangular_arbitrage_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

func applyTriangularArbitrageMigrationPrefix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	count int,
) {
	t.Helper()
	migrations, err := Migrations()
	if err != nil || len(migrations) < count {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if err = ensureMigrationTable(ctx, connection); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:count] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			t.Fatalf("migration %s changed=%t error=%v", migration.Name, changed, applyErr)
		}
	}
}

func assertTriangularArbitrageSchemaAndPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	configurationHash := seedTriangularArbitrageReferences(t, ctx, pool, now)
	repository, err := NewTriangularArbitrageRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	seedTriangularArbitrageDecision(t, ctx, pool, 4, now)
	tampered := triangularArbitrageCandidateWrite(4, strings.Repeat("0", 64), now)
	if err = repository.RecordCandidate(ctx, tampered); err == nil {
		t.Fatal("candidate with mismatched registered configuration hash persisted")
	}
	for index := 1; index <= 3; index++ {
		seedTriangularArbitrageDecision(t, ctx, pool, index, now)
		write := triangularArbitrageCandidateWrite(index, configurationHash, now)
		if err = repository.RecordCandidate(ctx, write); err != nil {
			t.Fatalf("candidate %d: %v", index, err)
		}
	}
	assertTriangularArbitrageCandidateEvidence(t, ctx, pool, repository)
	assertTriangularArbitrageAtomicClaims(t, ctx, pool, repository, now.Add(time.Minute))
	assertTriangularArbitrageOutcomeAndJournal(t, ctx, pool, repository, now.Add(2*time.Minute))
	assertTriangularArbitrageRoleMatrix(t, ctx, pool)
}

func seedTriangularArbitrageReferences(
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
	hash := strings.Repeat("a", 64)
	executeTriangularArbitrageSeedStatements(t, ctx, pool, triangularArbitrageMarketSeedStatements(configurationHash, canonical, now))
	executeTriangularArbitrageSeedStatements(t, ctx, pool, triangularArbitrageStrategySeedStatements(hash, now))
	seedTriangularArbitrageOwnership(t, ctx, pool, now)
	return configurationHash
}

type triangularArbitrageSeedStatement struct {
	sql  string
	args []any
}

func triangularArbitrageMarketSeedStatements(
	configurationHash string,
	canonical []byte,
	now time.Time,
) []triangularArbitrageSeedStatement {
	return []triangularArbitrageSeedStatement{
		{`INSERT INTO configuration_versions(
id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('configuration-triangular_arbitrage',1,$1,$2,'triangular_arbitrage-qualification',$3)`,
			[]any{configurationHash, canonical, now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES
('BTCUSDT','BTC','USDT','spot'),('ETHUSDT','ETH','USDT','spot'),('ETHBTC','ETH','BTC','spot')`, nil},
		{`INSERT INTO instrument_metadata_versions(
id,exchange_id,instrument_id,version,price_tick,quantity_step,
minimum_quantity,minimum_notional,effective_at,recorded_at
) VALUES
('metadata-btcusdt-triangular_arbitrage','binance','BTCUSDT',1,0.01,0.0001,0.0001,0.001,$1,$1),
('metadata-ethusdt-triangular_arbitrage','binance','ETHUSDT',1,0.01,0.0001,0.0001,0.001,$1,$1),
('metadata-ethbtc-triangular_arbitrage','binance','ETHBTC',1,0.0001,0.0001,0.0001,0.001,$1,$1)`, []any{now}},
	}
}

func triangularArbitrageStrategySeedStatements(hash string, now time.Time) []triangularArbitrageSeedStatement {
	return []triangularArbitrageSeedStatement{
		{`INSERT INTO strategy_definitions(id,name,family)
VALUES ('triangular-triangular_arbitrage','Triangular Arbitrage multi-strategy research','triangular')`, nil},
		{`INSERT INTO strategy_versions(
id,strategy_id,version,implementation_hash,promotion_status,created_at
) VALUES ('triangular-arbitrage-1-0-0','triangular-triangular_arbitrage',1,$1,'research',$2)`, []any{hash, now}},
		{`INSERT INTO model_versions(
id,model_type,version,model_hash,canonical_payload,created_at
) VALUES
('depth-triangular_arbitrage','depth',1,$1,'{}',$2),
('claim-triangular_arbitrage','claim',1,$1,'{}',$2),
('fee-triangular_arbitrage','fee',1,$1,'{}',$2),
('latency-triangular_arbitrage','latency',1,$1,'{}',$2),
('recovery-triangular_arbitrage','recovery',1,$1,'{}',$2)`, []any{hash, now}},
		{`INSERT INTO runs(
id,mode,configuration_id,strategy_version_id,root_seed_hash,reproducibility_hash,state,created_at
) VALUES ('run-triangular_arbitrage','backtest','configuration-triangular_arbitrage','triangular-arbitrage-1-0-0',$1,$1,'created',$2)`, []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-triangular_arbitrage','Triangular triangular arbitrage','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-triangular_arbitrage','portfolio-triangular_arbitrage','run-triangular_arbitrage','triangular-binance',$1)", []any{now}},
		{`INSERT INTO virtual_balances VALUES
('account-triangular_arbitrage','USDT',500,0,1,$1),('account-triangular_arbitrage','BTC',0,0,1,$1),('account-triangular_arbitrage','ETH',0,0,1,$1)`, []any{now}},
	}
}

func executeTriangularArbitrageSeedStatements(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	statements []triangularArbitrageSeedStatement,
) {
	t.Helper()
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("triangular arbitrage seed %d failed: %v", index+1, err)
		}
	}
}

func seedTriangularArbitrageOwnership(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES (
'journal-initialization-triangular_arbitrage','portfolio_initialization','run-triangular_arbitrage','portfolio-triangular_arbitrage',
'configuration-triangular_arbitrage','initialize-triangular_arbitrage','initialize-triangular_arbitrage',$1,1
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES
('journal-initialization-triangular_arbitrage',1,'available_asset','triangular','USDT','debit',500),
('journal-initialization-triangular_arbitrage',2,'external_equity','triangular','USDT','credit',500)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portfolio_ownership(
account_id,portfolio_id,exchange_id,strategy_version_id,strategy_key,
initialization_transaction_id,numeraire_asset,ownership_hash,created_at
) VALUES (
'account-triangular_arbitrage','portfolio-triangular_arbitrage','binance','triangular-arbitrage-1-0-0','triangular',
'journal-initialization-triangular_arbitrage','USDT',$1,$2
)`, strings.Repeat("b", 64), now); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedTriangularArbitrageDecision(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	index int,
	now time.Time,
) {
	t.Helper()
	id := triangularArbitrageDecisionID(index)
	_, err := pool.Exec(ctx, `INSERT INTO decisions(
id,run_id,configuration_id,strategy_version_id,outcome,reason_code,
causation_id,decided_at,ingest_ordinal,decision_market_scope
) VALUES (
$1,'run-triangular_arbitrage','configuration-triangular_arbitrage','triangular-arbitrage-1-0-0','approved',
'triangular.entry.accepted',$2,$3,$4,'single_market'
)`, id, "cause-"+id, now, index)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO risk_evaluations(
id,decision_id,policy_version,outcome,reason_code,evaluated_at,action,effective_state
) VALUES ($1,$2,'triangular-risk.v1','approved','approved',$3,'approve','NORMAL')`,
		"risk-"+id, id, now)
	if err != nil {
		t.Fatal(err)
	}
}

func triangularArbitrageCandidateWrite(
	index int,
	configurationHash string,
	now time.Time,
) TriangularArbitrageCandidateWrite {
	id := triangularArbitrageDecisionID(index)
	hashCharacter := string(rune('c' + index))
	return TriangularArbitrageCandidateWrite{
		Candidate: generated.InsertTriangularCandidateParams{
			DecisionID: id, StrategyVersionID: "triangular-arbitrage-1-0-0",
			ConfigurationID:             "configuration-triangular_arbitrage",
			PortfolioOwnershipAccountID: "account-triangular_arbitrage", ExchangeID: "binance",
			Cycle: "USDT-BTC-ETH-USDT", StartQuantity: "10",
			ExpectedFinalQuantity: "10.5", WorstFinalQuantity: "10.4",
			ExpectedNet: "0.5", WorstNet: "0.4", ExpectedEdge: "0.05",
			WorstEdge: "0.04", AdditionalSafetyMargin: "0.0015",
			FirstDetectedOffsetNanos: 100, DecisionOffsetNanos: 110,
			ExpiresOffsetNanos: 250_000_100, ConfigurationHash: configurationHash,
			ModelVersionID:            "depth-triangular_arbitrage",
			InstrumentMetadataSetHash: strings.Repeat("d", 64),
			RiskEvaluationID:          "risk-" + id, ClaimModelVersionID: "claim-triangular_arbitrage",
			FeeModelVersionID: "fee-triangular_arbitrage", LatencyModelVersionID: "latency-triangular_arbitrage",
			RecoveryModelVersionID: "recovery-triangular_arbitrage",
			CorrelationID:          "correlation-" + id, CausationID: "cause-" + id,
			CanonicalHash: strings.Repeat(hashCharacter, 64), RecordedAt: pgTimestamp(now),
		},
		Legs: triangularArbitrageCandidateLegWrites(id),
	}
}

func triangularArbitrageCandidateLegWrites(decisionID string) []generated.InsertTriangularCandidateLegParams {
	return []generated.InsertTriangularCandidateLegParams{
		{
			DecisionID: decisionID, LegIndex: 0, InstrumentID: "BTCUSDT",
			InstrumentMetadataID: "metadata-btcusdt-triangular_arbitrage", SourceAsset: "USDT",
			TargetAsset: "BTC", Side: "buy", InputQuantity: "10",
			TradeQuantity: "0.1", GrossOutput: "0.1", NetOutput: "0.1",
			SourceDust: "0", FeeAsset: "USDT", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "10", Vwap: "100",
			SpreadDepthCost: "0", BookVersion: 1, ConnectionGeneration: 1,
		},
		{
			DecisionID: decisionID, LegIndex: 1, InstrumentID: "ETHBTC",
			InstrumentMetadataID: "metadata-ethbtc-triangular_arbitrage", SourceAsset: "BTC",
			TargetAsset: "ETH", Side: "buy", InputQuantity: "0.1",
			TradeQuantity: "0.18", GrossOutput: "0.18", NetOutput: "0.18",
			SourceDust: "0", FeeAsset: "BTC", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "0.1", Vwap: "0.555555555555555555",
			SpreadDepthCost: "0", BookVersion: 1, ConnectionGeneration: 1,
		},
		{
			DecisionID: decisionID, LegIndex: 2, InstrumentID: "ETHUSDT",
			InstrumentMetadataID: "metadata-ethusdt-triangular_arbitrage", SourceAsset: "ETH",
			TargetAsset: "USDT", Side: "sell", InputQuantity: "0.18",
			TradeQuantity: "0.18", GrossOutput: "10.5", NetOutput: "10.5",
			SourceDust: "0", FeeAsset: "USDT", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "10.5",
			Vwap: "58.333333333333333333", SpreadDepthCost: "0",
			BookVersion: 1, ConnectionGeneration: 1,
		},
	}
}

func assertTriangularArbitrageCandidateEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
) {
	t.Helper()
	candidate, legs, err := repository.LoadCandidate(ctx, "decision-triangular_arbitrage-1")
	if err != nil || candidate.Cycle != "USDT-BTC-ETH-USDT" || len(legs) != 3 {
		t.Fatalf("triangular arbitrage restart/load mismatch: %#v %#v %v", candidate, legs, err)
	}
	if _, err = pool.Exec(ctx,
		"UPDATE triangular_candidates SET cycle='USDT-ETH-BTC-USDT' WHERE decision_id='decision-triangular_arbitrage-1'",
	); err == nil {
		t.Fatal("immutable triangular arbitrage candidate mutated")
	}
	if _, err = pool.Exec(ctx,
		"DELETE FROM triangular_candidate_legs WHERE decision_id='decision-triangular_arbitrage-1' AND leg_index=0",
	); err == nil {
		t.Fatal("immutable triangular arbitrage leg deleted")
	}
}

func assertTriangularArbitrageAtomicClaims(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	registerTriangularArbitrageClaimFixtures(t, ctx, repository, now)
	assertTriangularArbitrageFailedClaimRollsBack(t, ctx, pool, repository, now)
	activeGroup := claimTriangularArbitrageUnderContention(t, ctx, pool, repository, now)
	settleTriangularArbitrageClaim(t, ctx, repository, activeGroup, now.Add(time.Second))
	assertTriangularArbitrageQuarantineSurvivesRestart(t, ctx, pool, repository, now.Add(2*time.Second))
}

func registerTriangularArbitrageClaimFixtures(
	t *testing.T,
	ctx context.Context,
	repository *TriangularArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	for _, write := range []generated.RegisterTriangularArbitrageClaimResourceParams{
		{
			PID: "resource-triangular_arbitrage-balance", PAccountID: "account-triangular_arbitrage",
			PExchangeID: "binance", PResourceKind: "balance",
			PResourceKey: "usdt", PAvailable: "10", PRecordedAt: pgTimestamp(now),
		},
		{
			PID: "resource-triangular_arbitrage-liquidity", PAccountID: "account-triangular_arbitrage",
			PExchangeID: "binance", PResourceKind: "liquidity",
			PResourceKey: "btcusdt/buy/v1", PAvailable: "1", PRecordedAt: pgTimestamp(now),
		},
		{
			PID: "resource-triangular_arbitrage-quarantine", PAccountID: "account-triangular_arbitrage",
			PExchangeID: "binance", PResourceKind: "recovery",
			PResourceKey: "usdt", PAvailable: "2", PRecordedAt: pgTimestamp(now),
		},
	} {
		if err := repository.RegisterClaimResource(ctx, write); err != nil {
			t.Fatal(err)
		}
	}
}

func assertTriangularArbitrageFailedClaimRollsBack(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	rejected := triangularArbitrageClaimWrite("group-triangular_arbitrage-rejected", "decision-triangular_arbitrage-1",
		[]string{"resource-triangular_arbitrage-balance", "resource-triangular_arbitrage-liquidity"},
		[]string{"5", "2"}, now)
	if err := repository.Claim(ctx, rejected); err == nil {
		t.Fatal("insufficient multi-resource claim committed")
	}
	var held, groups string
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(held_quantity),0)::text
FROM triangular_arbitrage_claim_resources`).Scan(&held); err != nil || held != "0.000000000000000000" {
		t.Fatalf("partial hold leaked: %s %v", held, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)::text FROM triangular_arbitrage_claim_groups`).Scan(&groups); err != nil || groups != "0" {
		t.Fatalf("failed group persisted: %s %v", groups, err)
	}
}

func claimTriangularArbitrageUnderContention(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
	now time.Time,
) string {
	t.Helper()
	claims := []generated.ClaimTriangularArbitrageResourcesParams{
		triangularArbitrageClaimWrite("group-triangular_arbitrage-1", "decision-triangular_arbitrage-1",
			[]string{"resource-triangular_arbitrage-balance"}, []string{"10"}, now),
		triangularArbitrageClaimWrite("group-triangular_arbitrage-2", "decision-triangular_arbitrage-2",
			[]string{"resource-triangular_arbitrage-balance"}, []string{"10"}, now),
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, claim := range claims {
		wait.Add(1)
		go func(write generated.ClaimTriangularArbitrageResourcesParams) {
			defer wait.Done()
			results <- repository.Claim(ctx, write)
		}(claim)
	}
	wait.Wait()
	close(results)
	successes := 0
	failures := make([]string, 0, 2)
	for result := range results {
		if result == nil {
			successes++
		} else {
			failures = append(failures, result.Error())
		}
	}
	if successes != 1 {
		t.Fatalf("atomic claim contention winners=%d failures=%v", successes, failures)
	}
	var activeGroup string
	if err := pool.QueryRow(ctx,
		"SELECT id FROM triangular_arbitrage_claim_groups WHERE state='active' ORDER BY id LIMIT 1",
	).Scan(&activeGroup); err != nil {
		t.Fatal(err)
	}
	return activeGroup
}

func settleTriangularArbitrageClaim(
	t *testing.T,
	ctx context.Context,
	repository *TriangularArbitrageRepository,
	activeGroup string,
	now time.Time,
) {
	t.Helper()
	if err := repository.Settle(ctx, generated.SettleTriangularArbitrageClaimGroupParams{
		GroupID: activeGroup, ExpectedRevision: 1, FencingToken: 7,
		ResourceIds: []string{"resource-triangular_arbitrage-balance"}, Consumed: []pgtype.Numeric{triangularArbitrageNumeric("10")},
		Final: true, RecordedAt: pgTimestamp(now),
	}); err != nil {
		t.Fatal(err)
	}
}

func assertTriangularArbitrageQuarantineSurvivesRestart(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	if err := repository.Claim(ctx, triangularArbitrageClaimWrite(
		"group-triangular_arbitrage-quarantine", "decision-triangular_arbitrage-3",
		[]string{"resource-triangular_arbitrage-quarantine"}, []string{"2"}, now,
	)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(ctx, generated.CloseTriangularArbitrageClaimGroupParams{
		PGroupID: "group-triangular_arbitrage-quarantine", PExpectedRevision: 1, PFencingToken: 7,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now.Add(time.Second)),
	}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT groups.state || ':' || resources.held_quantity::text
FROM triangular_arbitrage_claim_groups groups
JOIN triangular_arbitrage_claim_items items ON items.group_id=groups.id
JOIN triangular_arbitrage_claim_resources resources ON resources.id=items.resource_id
WHERE groups.id='group-triangular_arbitrage-quarantine'`).Scan(&state); err != nil ||
		state != "quarantined:2.000000000000000000" {
		t.Fatalf("quarantined hold was not restart-safe: %s %v", state, err)
	}
}

func triangularArbitrageClaimWrite(
	groupID, decisionID string,
	resourceIDs []string,
	quantities []string,
	now time.Time,
) generated.ClaimTriangularArbitrageResourcesParams {
	numeric := make([]pgtype.Numeric, len(quantities))
	for index, quantity := range quantities {
		numeric[index] = triangularArbitrageNumeric(quantity)
	}
	return generated.ClaimTriangularArbitrageResourcesParams{
		GroupID: groupID, DecisionID: decisionID, AccountID: "account-triangular_arbitrage",
		FencingToken: 7, CorrelationID: "correlation-" + groupID,
		CausationID: "cause-" + groupID, ResourceIds: resourceIDs,
		Quantities: numeric, RecordedAt: pgTimestamp(now),
	}
}

func triangularArbitrageNumeric(value string) pgtype.Numeric {
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		panic(err)
	}
	return numeric
}

func assertTriangularArbitrageOutcomeAndJournal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *TriangularArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	seedTriangularArbitrageOutcomeJournal(t, ctx, pool, now)
	if err := repository.RecordOutcome(ctx, triangularArbitrageOutcomeWrite(now)); err != nil {
		t.Fatal(err)
	}
	assertTriangularArbitrageOutcomeImmutable(t, ctx, pool)
}

func seedTriangularArbitrageOutcomeJournal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO execution_plans(
id,decision_id,state,recovery_state,revision,created_at,updated_at
) VALUES ('plan-triangular_arbitrage','decision-triangular_arbitrage-1','completed','not_required',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES (
'journal-triangular_arbitrage-trade','triangular_arbitrage_trade_economics','run-triangular_arbitrage','portfolio-triangular_arbitrage',
'configuration-triangular_arbitrage','cause-outcome-triangular_arbitrage','correlation-outcome-triangular_arbitrage',$1,2
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES
('journal-triangular_arbitrage-trade',1,'trade_cost_proceeds','triangular','USDT','debit',0.5),
('journal-triangular_arbitrage-trade',2,'realized_pnl','triangular','USDT','credit',0.5)`,
	); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func triangularArbitrageOutcomeWrite(now time.Time) TriangularArbitrageOutcomeWrite {
	return TriangularArbitrageOutcomeWrite{
		Simulation: generated.InsertTriangularSimulationOutcomeParams{
			DecisionID: "decision-triangular_arbitrage-1", PlanID: "plan-triangular_arbitrage",
			Outcome: "full_success", ActualFinalUsdt: "10.4",
			LatencyModelVersionID: "latency-triangular_arbitrage", RecoveryLoss: "0",
			CanonicalHash: strings.Repeat("f", 64),
			CorrelationID: "correlation-outcome-triangular_arbitrage",
			CausationID:   "cause-outcome-triangular_arbitrage", RecordedAt: pgTimestamp(now),
		},
		Lifetime: generated.InsertTriangularOpportunityLifetimeParams{
			DecisionID: "decision-triangular_arbitrage-1", FirstDetectionNanos: 100,
			LastProfitableNanos: 200, PeakEdge: "0.05", EdgeAtArrival: "0.04",
			TotalLifetimeNanos: 100, SurvivedP50: true, SurvivedP95: true,
			MetricWindow: 1000, CorrelationID: "correlation-outcome-triangular_arbitrage",
			CausationID: "cause-outcome-triangular_arbitrage", RecordedAt: pgTimestamp(now),
		},
		Journals: []generated.InsertTriangularJournalLinkParams{
			{
				DecisionID: "decision-triangular_arbitrage-1", TransactionID: "journal-triangular_arbitrage-trade",
				Category: "trade_economics",
			},
		},
	}
}

func assertTriangularArbitrageOutcomeImmutable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		"DELETE FROM triangular_simulation_outcomes WHERE decision_id='decision-triangular_arbitrage-1'",
	); err == nil {
		t.Fatal("immutable triangular arbitrage simulation outcome deleted")
	}
	if _, err := pool.Exec(ctx,
		"UPDATE triangular_opportunity_lifetimes SET peak_edge=1 WHERE decision_id='decision-triangular_arbitrage-1'",
	); err == nil {
		t.Fatal("immutable triangular arbitrage lifetime mutated")
	}
}

func assertTriangularArbitrageRoleMatrix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_TRIANGULAR_ARBITRAGE_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_TRIANGULAR_ARBITRAGE_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_TRIANGULAR_ARBITRAGE_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	signature := "claim_triangular_arbitrage_resources(text,text,text,bigint,text,text,text[],numeric[],timestamp with time zone)"
	var runtimeExecute, readonlyExecute bool
	if err := pool.QueryRow(ctx,
		"SELECT has_function_privilege($1,$2,'EXECUTE')", runtimeRole, signature,
	).Scan(&runtimeExecute); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT has_function_privilege($1,$2,'EXECUTE')", readOnlyRole, signature,
	).Scan(&readonlyExecute); err != nil {
		t.Fatal(err)
	}
	if !runtimeExecute || readonlyExecute {
		t.Fatalf("triangular arbitrage function role matrix runtime=%t readonly=%t", runtimeExecute, readonlyExecute)
	}
}

func triangularArbitrageDecisionID(index int) string {
	return "decision-triangular_arbitrage-" + string(rune('0'+index))
}
