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

func TestB4PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB4TestDatabase(t, "AXIOM_B4_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 16)
	assertB4SchemaAndPersistence(t, ctx, pool)
}

func TestB4PostgresB3ToB4UpgradeQualification(t *testing.T) {
	ctx, pool := openB4TestDatabase(t, "AXIOM_B4_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 15)
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
		t.Fatalf("B3-to-B4 migration changed=%t error=%v", changed, err)
	}
	assertB4SchemaAndPersistence(t, ctx, pool)
}

func openB4TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b4_test") {
		t.Fatal("B4 integration requires a dedicated database ending _b4_test")
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

func applyB4MigrationPrefix(
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

func assertB4SchemaAndPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	configurationHash := seedB4References(t, ctx, pool, now)
	repository, err := NewB4Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	seedB4Decision(t, ctx, pool, 4, now)
	tampered := b4CandidateWrite(4, strings.Repeat("0", 64), now)
	if err = repository.RecordCandidate(ctx, tampered); err == nil {
		t.Fatal("candidate with mismatched registered configuration hash persisted")
	}
	for index := 1; index <= 3; index++ {
		seedB4Decision(t, ctx, pool, index, now)
		write := b4CandidateWrite(index, configurationHash, now)
		if err = repository.RecordCandidate(ctx, write); err != nil {
			t.Fatalf("candidate %d: %v", index, err)
		}
	}
	assertB4CandidateEvidence(t, ctx, pool, repository)
	assertB4AtomicClaims(t, ctx, pool, repository, now.Add(time.Minute))
	assertB4OutcomeAndJournal(t, ctx, pool, repository, now.Add(2*time.Minute))
	assertB4RoleMatrix(t, ctx, pool)
}

func seedB4References(
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
	hash := strings.Repeat("a", 64)
	executeB4SeedStatements(t, ctx, pool, b4MarketSeedStatements(configurationHash, canonical, now))
	executeB4SeedStatements(t, ctx, pool, b4StrategySeedStatements(hash, now))
	seedB4Ownership(t, ctx, pool, now)
	return configurationHash
}

type b4SeedStatement struct {
	sql  string
	args []any
}

func b4MarketSeedStatements(
	configurationHash string,
	canonical []byte,
	now time.Time,
) []b4SeedStatement {
	return []b4SeedStatement{
		{`INSERT INTO configuration_versions(
id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('configuration-b4',1,$1,$2,'b4-qualification',$3)`,
			[]any{configurationHash, canonical, now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES
('BTCUSDT','BTC','USDT','spot'),('ETHUSDT','ETH','USDT','spot'),('ETHBTC','ETH','BTC','spot')`, nil},
		{`INSERT INTO instrument_metadata_versions(
id,exchange_id,instrument_id,version,price_tick,quantity_step,
minimum_quantity,minimum_notional,effective_at,recorded_at
) VALUES
('metadata-btcusdt-b4','binance','BTCUSDT',1,0.01,0.0001,0.0001,0.001,$1,$1),
('metadata-ethusdt-b4','binance','ETHUSDT',1,0.01,0.0001,0.0001,0.001,$1,$1),
('metadata-ethbtc-b4','binance','ETHBTC',1,0.0001,0.0001,0.0001,0.001,$1,$1)`, []any{now}},
	}
}

func b4StrategySeedStatements(hash string, now time.Time) []b4SeedStatement {
	return []b4SeedStatement{
		{`INSERT INTO strategy_definitions(id,name,family)
VALUES ('triangular-b4','Triangular Arbitrage V1B','triangular')`, nil},
		{`INSERT INTO strategy_versions(
id,strategy_id,version,implementation_hash,promotion_status,created_at
) VALUES ('triangular-v1b-1','triangular-b4',1,$1,'research',$2)`, []any{hash, now}},
		{`INSERT INTO model_versions(
id,model_type,version,model_hash,canonical_payload,created_at
) VALUES
('depth-b4','depth',1,$1,'{}',$2),
('claim-b4','claim',1,$1,'{}',$2),
('fee-b4','fee',1,$1,'{}',$2),
('latency-b4','latency',1,$1,'{}',$2),
('recovery-b4','recovery',1,$1,'{}',$2)`, []any{hash, now}},
		{`INSERT INTO runs(
id,mode,configuration_id,strategy_version_id,root_seed_hash,reproducibility_hash,state,created_at
) VALUES ('run-b4','backtest','configuration-b4','triangular-v1b-1',$1,$1,'created',$2)`, []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-b4','Triangular B4','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-b4','portfolio-b4','run-b4','triangular-binance',$1)", []any{now}},
		{`INSERT INTO virtual_balances VALUES
('account-b4','USDT',500,0,1,$1),('account-b4','BTC',0,0,1,$1),('account-b4','ETH',0,0,1,$1)`, []any{now}},
	}
}

func executeB4SeedStatements(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	statements []b4SeedStatement,
) {
	t.Helper()
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("B4 seed %d failed: %v", index+1, err)
		}
	}
}

func seedB4Ownership(
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
'journal-initialization-b4','portfolio_initialization','run-b4','portfolio-b4',
'configuration-b4','initialize-b4','initialize-b4',$1,1
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES
('journal-initialization-b4',1,'available_asset','triangular','USDT','debit',500),
('journal-initialization-b4',2,'external_equity','triangular','USDT','credit',500)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portfolio_ownership(
account_id,portfolio_id,exchange_id,strategy_version_id,strategy_key,
initialization_transaction_id,numeraire_asset,ownership_hash,created_at
) VALUES (
'account-b4','portfolio-b4','binance','triangular-v1b-1','triangular',
'journal-initialization-b4','USDT',$1,$2
)`, strings.Repeat("b", 64), now); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedB4Decision(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	index int,
	now time.Time,
) {
	t.Helper()
	id := b4DecisionID(index)
	_, err := pool.Exec(ctx, `INSERT INTO decisions(
id,run_id,configuration_id,strategy_version_id,outcome,reason_code,
causation_id,decided_at,ingest_ordinal,decision_market_scope
) VALUES (
$1,'run-b4','configuration-b4','triangular-v1b-1','approved',
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

func b4CandidateWrite(
	index int,
	configurationHash string,
	now time.Time,
) B4CandidateWrite {
	id := b4DecisionID(index)
	hashCharacter := string(rune('c' + index))
	return B4CandidateWrite{
		Candidate: generated.InsertTriangularCandidateParams{
			DecisionID: id, StrategyVersionID: "triangular-v1b-1",
			ConfigurationID:             "configuration-b4",
			PortfolioOwnershipAccountID: "account-b4", ExchangeID: "binance",
			Cycle: "USDT-BTC-ETH-USDT", StartQuantity: "10",
			ExpectedFinalQuantity: "10.5", WorstFinalQuantity: "10.4",
			ExpectedNet: "0.5", WorstNet: "0.4", ExpectedEdge: "0.05",
			WorstEdge: "0.04", AdditionalSafetyMargin: "0.0015",
			FirstDetectedOffsetNanos: 100, DecisionOffsetNanos: 110,
			ExpiresOffsetNanos: 250_000_100, ConfigurationHash: configurationHash,
			ModelVersionID:            "depth-b4",
			InstrumentMetadataSetHash: strings.Repeat("d", 64),
			RiskEvaluationID:          "risk-" + id, ClaimModelVersionID: "claim-b4",
			FeeModelVersionID: "fee-b4", LatencyModelVersionID: "latency-b4",
			RecoveryModelVersionID: "recovery-b4",
			CorrelationID:          "correlation-" + id, CausationID: "cause-" + id,
			CanonicalHash: strings.Repeat(hashCharacter, 64), RecordedAt: pgTimestamp(now),
		},
		Legs: b4CandidateLegWrites(id),
	}
}

func b4CandidateLegWrites(decisionID string) []generated.InsertTriangularCandidateLegParams {
	return []generated.InsertTriangularCandidateLegParams{
		{
			DecisionID: decisionID, LegIndex: 0, InstrumentID: "BTCUSDT",
			InstrumentMetadataID: "metadata-btcusdt-b4", SourceAsset: "USDT",
			TargetAsset: "BTC", Side: "buy", InputQuantity: "10",
			TradeQuantity: "0.1", GrossOutput: "0.1", NetOutput: "0.1",
			SourceDust: "0", FeeAsset: "USDT", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "10", Vwap: "100",
			SpreadDepthCost: "0", BookVersion: 1, ConnectionGeneration: 1,
		},
		{
			DecisionID: decisionID, LegIndex: 1, InstrumentID: "ETHBTC",
			InstrumentMetadataID: "metadata-ethbtc-b4", SourceAsset: "BTC",
			TargetAsset: "ETH", Side: "buy", InputQuantity: "0.1",
			TradeQuantity: "0.18", GrossOutput: "0.18", NetOutput: "0.18",
			SourceDust: "0", FeeAsset: "BTC", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "0.1", Vwap: "0.555555555555555555",
			SpreadDepthCost: "0", BookVersion: 1, ConnectionGeneration: 1,
		},
		{
			DecisionID: decisionID, LegIndex: 2, InstrumentID: "ETHUSDT",
			InstrumentMetadataID: "metadata-ethusdt-b4", SourceAsset: "ETH",
			TargetAsset: "USDT", Side: "sell", InputQuantity: "0.18",
			TradeQuantity: "0.18", GrossOutput: "10.5", NetOutput: "10.5",
			SourceDust: "0", FeeAsset: "USDT", FeeQuantity: "0",
			FeeQuoteEquivalent: "0", Notional: "10.5",
			Vwap: "58.333333333333333333", SpreadDepthCost: "0",
			BookVersion: 1, ConnectionGeneration: 1,
		},
	}
}

func assertB4CandidateEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
) {
	t.Helper()
	candidate, legs, err := repository.LoadCandidate(ctx, "decision-b4-1")
	if err != nil || candidate.Cycle != "USDT-BTC-ETH-USDT" || len(legs) != 3 {
		t.Fatalf("B4 restart/load mismatch: %#v %#v %v", candidate, legs, err)
	}
	if _, err = pool.Exec(ctx,
		"UPDATE triangular_candidates SET cycle='USDT-ETH-BTC-USDT' WHERE decision_id='decision-b4-1'",
	); err == nil {
		t.Fatal("immutable B4 candidate mutated")
	}
	if _, err = pool.Exec(ctx,
		"DELETE FROM triangular_candidate_legs WHERE decision_id='decision-b4-1' AND leg_index=0",
	); err == nil {
		t.Fatal("immutable B4 leg deleted")
	}
}

func assertB4AtomicClaims(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
	now time.Time,
) {
	t.Helper()
	registerB4ClaimFixtures(t, ctx, repository, now)
	assertB4FailedClaimRollsBack(t, ctx, pool, repository, now)
	activeGroup := claimB4UnderContention(t, ctx, pool, repository, now)
	settleB4Claim(t, ctx, repository, activeGroup, now.Add(time.Second))
	assertB4QuarantineSurvivesRestart(t, ctx, pool, repository, now.Add(2*time.Second))
}

func registerB4ClaimFixtures(
	t *testing.T,
	ctx context.Context,
	repository *B4Repository,
	now time.Time,
) {
	t.Helper()
	for _, write := range []generated.RegisterB4ClaimResourceParams{
		{
			PID: "resource-b4-balance", PAccountID: "account-b4",
			PExchangeID: "binance", PResourceKind: "balance",
			PResourceKey: "usdt", PAvailable: "10", PRecordedAt: pgTimestamp(now),
		},
		{
			PID: "resource-b4-liquidity", PAccountID: "account-b4",
			PExchangeID: "binance", PResourceKind: "liquidity",
			PResourceKey: "btcusdt/buy/v1", PAvailable: "1", PRecordedAt: pgTimestamp(now),
		},
		{
			PID: "resource-b4-quarantine", PAccountID: "account-b4",
			PExchangeID: "binance", PResourceKind: "recovery",
			PResourceKey: "usdt", PAvailable: "2", PRecordedAt: pgTimestamp(now),
		},
	} {
		if err := repository.RegisterClaimResource(ctx, write); err != nil {
			t.Fatal(err)
		}
	}
}

func assertB4FailedClaimRollsBack(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
	now time.Time,
) {
	t.Helper()
	rejected := b4ClaimWrite("group-b4-rejected", "decision-b4-1",
		[]string{"resource-b4-balance", "resource-b4-liquidity"},
		[]string{"5", "2"}, now)
	if err := repository.Claim(ctx, rejected); err == nil {
		t.Fatal("insufficient multi-resource claim committed")
	}
	var held, groups string
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(held_quantity),0)::text
FROM b4_claim_resources`).Scan(&held); err != nil || held != "0.000000000000000000" {
		t.Fatalf("partial hold leaked: %s %v", held, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)::text FROM b4_claim_groups`).Scan(&groups); err != nil || groups != "0" {
		t.Fatalf("failed group persisted: %s %v", groups, err)
	}
}

func claimB4UnderContention(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
	now time.Time,
) string {
	t.Helper()
	claims := []generated.ClaimB4ResourcesParams{
		b4ClaimWrite("group-b4-1", "decision-b4-1",
			[]string{"resource-b4-balance"}, []string{"10"}, now),
		b4ClaimWrite("group-b4-2", "decision-b4-2",
			[]string{"resource-b4-balance"}, []string{"10"}, now),
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, claim := range claims {
		wait.Add(1)
		go func(write generated.ClaimB4ResourcesParams) {
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
		"SELECT id FROM b4_claim_groups WHERE state='active' ORDER BY id LIMIT 1",
	).Scan(&activeGroup); err != nil {
		t.Fatal(err)
	}
	return activeGroup
}

func settleB4Claim(
	t *testing.T,
	ctx context.Context,
	repository *B4Repository,
	activeGroup string,
	now time.Time,
) {
	t.Helper()
	if err := repository.Settle(ctx, generated.SettleB4ClaimGroupParams{
		GroupID: activeGroup, ExpectedRevision: 1, FencingToken: 7,
		ResourceIds: []string{"resource-b4-balance"}, Consumed: []pgtype.Numeric{b4Numeric("10")},
		Final: true, RecordedAt: pgTimestamp(now),
	}); err != nil {
		t.Fatal(err)
	}
}

func assertB4QuarantineSurvivesRestart(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
	now time.Time,
) {
	t.Helper()
	if err := repository.Claim(ctx, b4ClaimWrite(
		"group-b4-quarantine", "decision-b4-3",
		[]string{"resource-b4-quarantine"}, []string{"2"}, now,
	)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(ctx, generated.CloseB4ClaimGroupParams{
		PGroupID: "group-b4-quarantine", PExpectedRevision: 1, PFencingToken: 7,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now.Add(time.Second)),
	}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT groups.state || ':' || resources.held_quantity::text
FROM b4_claim_groups groups
JOIN b4_claim_items items ON items.group_id=groups.id
JOIN b4_claim_resources resources ON resources.id=items.resource_id
WHERE groups.id='group-b4-quarantine'`).Scan(&state); err != nil ||
		state != "quarantined:2.000000000000000000" {
		t.Fatalf("quarantined hold was not restart-safe: %s %v", state, err)
	}
}

func b4ClaimWrite(
	groupID, decisionID string,
	resourceIDs []string,
	quantities []string,
	now time.Time,
) generated.ClaimB4ResourcesParams {
	numeric := make([]pgtype.Numeric, len(quantities))
	for index, quantity := range quantities {
		numeric[index] = b4Numeric(quantity)
	}
	return generated.ClaimB4ResourcesParams{
		GroupID: groupID, DecisionID: decisionID, AccountID: "account-b4",
		FencingToken: 7, CorrelationID: "correlation-" + groupID,
		CausationID: "cause-" + groupID, ResourceIds: resourceIDs,
		Quantities: numeric, RecordedAt: pgTimestamp(now),
	}
}

func b4Numeric(value string) pgtype.Numeric {
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		panic(err)
	}
	return numeric
}

func assertB4OutcomeAndJournal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B4Repository,
	now time.Time,
) {
	t.Helper()
	seedB4OutcomeJournal(t, ctx, pool, now)
	if err := repository.RecordOutcome(ctx, b4OutcomeWrite(now)); err != nil {
		t.Fatal(err)
	}
	assertB4OutcomeImmutable(t, ctx, pool)
}

func seedB4OutcomeJournal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO execution_plans(
id,decision_id,state,recovery_state,revision,created_at,updated_at
) VALUES ('plan-b4','decision-b4-1','completed','not_required',1,$1,$1)`, now); err != nil {
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
'journal-b4-trade','b4_trade_economics','run-b4','portfolio-b4',
'configuration-b4','cause-outcome-b4','correlation-outcome-b4',$1,2
)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES
('journal-b4-trade',1,'trade_cost_proceeds','triangular','USDT','debit',0.5),
('journal-b4-trade',2,'realized_pnl','triangular','USDT','credit',0.5)`,
	); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func b4OutcomeWrite(now time.Time) B4OutcomeWrite {
	return B4OutcomeWrite{
		Simulation: generated.InsertTriangularSimulationOutcomeParams{
			DecisionID: "decision-b4-1", PlanID: "plan-b4",
			Outcome: "full_success", ActualFinalUsdt: "10.4",
			LatencyModelVersionID: "latency-b4", RecoveryLoss: "0",
			CanonicalHash: strings.Repeat("f", 64),
			CorrelationID: "correlation-outcome-b4",
			CausationID:   "cause-outcome-b4", RecordedAt: pgTimestamp(now),
		},
		Lifetime: generated.InsertTriangularOpportunityLifetimeParams{
			DecisionID: "decision-b4-1", FirstDetectionNanos: 100,
			LastProfitableNanos: 200, PeakEdge: "0.05", EdgeAtArrival: "0.04",
			TotalLifetimeNanos: 100, SurvivedP50: true, SurvivedP95: true,
			MetricWindow: 1000, CorrelationID: "correlation-outcome-b4",
			CausationID: "cause-outcome-b4", RecordedAt: pgTimestamp(now),
		},
		Journals: []generated.InsertTriangularJournalLinkParams{
			{
				DecisionID: "decision-b4-1", TransactionID: "journal-b4-trade",
				Category: "trade_economics",
			},
		},
	}
}

func assertB4OutcomeImmutable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		"DELETE FROM triangular_simulation_outcomes WHERE decision_id='decision-b4-1'",
	); err == nil {
		t.Fatal("immutable B4 simulation outcome deleted")
	}
	if _, err := pool.Exec(ctx,
		"UPDATE triangular_opportunity_lifetimes SET peak_edge=1 WHERE decision_id='decision-b4-1'",
	); err == nil {
		t.Fatal("immutable B4 lifetime mutated")
	}
}

func assertB4RoleMatrix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_B4_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_B4_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_B4_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	signature := "claim_b4_resources(text,text,text,bigint,text,text,text[],numeric[],timestamp with time zone)"
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
		t.Fatalf("B4 function role matrix runtime=%t readonly=%t", runtimeExecute, readonlyExecute)
	}
}

func b4DecisionID(index int) string {
	return "decision-b4-" + string(rune('0'+index))
}
