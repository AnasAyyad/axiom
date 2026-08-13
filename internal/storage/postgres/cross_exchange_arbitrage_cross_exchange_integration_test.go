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

func TestCrossExchangeArbitragePostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openCrossExchangeArbitrageTestDatabase(t, "AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 17)
	assertCrossExchangeArbitrageSchemaAndPersistence(t, ctx, pool)
}

func TestCrossExchangeArbitragePostgresTriangularArbitrageToCrossExchangeArbitrageUpgradeQualification(t *testing.T) {
	ctx, pool := openCrossExchangeArbitrageTestDatabase(t, "AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 16)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 17 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	changed, err := applyMigration(ctx, connection, migrations[16])
	if err != nil || !changed {
		t.Fatalf("triangular-to-cross-exchange migration changed=%t error=%v", changed, err)
	}
	assertCrossExchangeArbitrageSchemaAndPersistence(t, ctx, pool)
}

func openCrossExchangeArbitrageTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_cross_exchange_arbitrage_test") {
		t.Fatal("cross-exchange arbitrage integration requires a dedicated database ending _cross_exchange_arbitrage_test")
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

func assertCrossExchangeArbitrageSchemaAndPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	configurationHash, coherentViewID := seedCrossExchangeArbitrageReferences(t, ctx, pool, now)
	repository, err := NewCrossExchangeArbitrageRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	seedCrossExchangeArbitrageDecision(t, ctx, pool, "decision-cross_exchange_arbitrage-good", coherentViewID, now)
	write := crossExchangeArbitrageDatabaseCandidateWrite("decision-cross_exchange_arbitrage-good", configurationHash, coherentViewID, now)
	if err = repository.RecordCandidate(ctx, write); err != nil {
		t.Fatal(err)
	}
	seedCrossExchangeArbitrageDecision(t, ctx, pool, "decision-cross_exchange_arbitrage-tamper", coherentViewID, now)
	tampered := crossExchangeArbitrageDatabaseCandidateWrite(
		"decision-cross_exchange_arbitrage-tamper", strings.Repeat("0", 64), coherentViewID, now,
	)
	if err = repository.RecordCandidate(ctx, tampered); err == nil {
		t.Fatal("mismatched registered configuration hash persisted")
	}
	assertCrossExchangeArbitrageCandidateEvidence(t, ctx, pool, repository)
	assertCrossExchangeArbitrageAtomicClaims(t, ctx, pool, repository, now.Add(time.Minute))
	assertCrossExchangeArbitrageOutcomeAndAccounting(t, ctx, pool, repository, now.Add(2*time.Minute))
	assertCrossExchangeArbitrageRoleMatrix(t, ctx, pool)
}

func seedCrossExchangeArbitrageReferences(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) (string, string) {
	t.Helper()
	canonical, err := json.Marshal(config.DefaultMultiStrategyConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := researchRegistryPayloadHash(canonical)
	viewID := strings.Repeat("e", 64)
	statements := crossExchangeArbitrageReferenceSeedStatements(configurationHash, canonical, viewID, now)
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("cross-exchange arbitrage reference seed %d failed: %v", index+1, err)
		}
	}
	seedCrossExchangeArbitrageOwnership(t, ctx, pool, now)
	seedCrossExchangeArbitrageCoherentView(t, ctx, pool, viewID, now)
	return configurationHash, viewID
}

func crossExchangeArbitrageReferenceSeedStatements(
	configurationHash string,
	canonical []byte,
	viewID string,
	now time.Time,
) []triangularArbitrageSeedStatement {
	hash := strings.Repeat("a", 64)
	statements := crossExchangeArbitrageStrategyReferenceSeedStatements(configurationHash, canonical, hash, now)
	return append(statements, crossExchangeArbitragePortfolioReferenceSeedStatements(hash, viewID, now)...)
}

func crossExchangeArbitrageStrategyReferenceSeedStatements(
	configurationHash string,
	canonical []byte,
	hash string,
	now time.Time,
) []triangularArbitrageSeedStatement {
	return []triangularArbitrageSeedStatement{
		{`INSERT INTO configuration_versions(
id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('configuration-cross_exchange_arbitrage',1,$1,$2,'cross_exchange_arbitrage-qualification',$3)`,
			[]any{configurationHash, canonical, now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES
('BTCUSDT','BTC','USDT','spot'),('ETHUSDT','ETH','USDT','spot')`, nil},
		{`INSERT INTO instrument_metadata_versions(
id,exchange_id,instrument_id,version,price_tick,quantity_step,
minimum_quantity,minimum_notional,effective_at,recorded_at
) VALUES
('metadata-binance-cross_exchange_arbitrage','binance','BTCUSDT',1,0.01,0.000001,0.000001,0.01,$1,$1),
('metadata-bybit-cross_exchange_arbitrage','bybit','BTCUSDT',1,0.01,0.000001,0.000001,0.01,$1,$1)`,
			[]any{now}},
		{`INSERT INTO strategy_definitions(id,name,family)
VALUES ('cross-exchange-cross_exchange_arbitrage','Cross Exchange Arbitrage multi-strategy research','cross_exchange')`, nil},
		{`INSERT INTO strategy_versions(
id,strategy_id,version,implementation_hash,promotion_status,created_at
) VALUES ('cross-exchange-arbitrage-1-0-0','cross-exchange-cross_exchange_arbitrage',1,$1,'research',$2)`,
			[]any{hash, now}},
		{`INSERT INTO model_versions(
id,model_type,version,model_hash,canonical_payload,created_at
) VALUES
('depth-cross_exchange_arbitrage','depth',1,$1,'{}',$2),
('claim-cross_exchange_arbitrage','claim',1,$1,'{}',$2),
('fee-cross_exchange_arbitrage','fee',1,$1,'{}',$2),
('latency-cross_exchange_arbitrage','latency',1,$1,'{}',$2),
('recovery-cross_exchange_arbitrage','recovery',1,$1,'{}',$2),
('shadow-cross_exchange_arbitrage','inventory_shadow',1,$1,'{}',$2),
('concentration-cross_exchange_arbitrage','concentration',1,$1,'{}',$2)`, []any{hash, now}},
	}
}

func crossExchangeArbitragePortfolioReferenceSeedStatements(
	hash, viewID string,
	now time.Time,
) []triangularArbitrageSeedStatement {
	return []triangularArbitrageSeedStatement{
		{`INSERT INTO runs(
id,mode,configuration_id,strategy_version_id,root_seed_hash,
reproducibility_hash,state,created_at
) VALUES (
'run-cross_exchange_arbitrage','backtest','configuration-cross_exchange_arbitrage','cross-exchange-arbitrage-1-0-0',$1,$1,'created',$2
)`, []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-cross_exchange_arbitrage','Cross Exchange cross-exchange arbitrage','USDT',$1)", []any{now}},
		{`INSERT INTO virtual_accounts VALUES
('buy-account-cross_exchange_arbitrage','portfolio-cross_exchange_arbitrage','run-cross_exchange_arbitrage','crossarb-binance',$1),
('sell-account-cross_exchange_arbitrage','portfolio-cross_exchange_arbitrage','run-cross_exchange_arbitrage','crossarb-bybit',$1)`, []any{now}},
		{`INSERT INTO virtual_balances VALUES
('buy-account-cross_exchange_arbitrage','USDT',100,0,1,$1),('buy-account-cross_exchange_arbitrage','BTC',20,0,1,$1),
('sell-account-cross_exchange_arbitrage','USDT',100,0,1,$1),('sell-account-cross_exchange_arbitrage','BTC',80,0,1,$1)`,
			[]any{now}},
		{`SELECT $1::sha256_hex`, []any{viewID}},
	}
}

func seedCrossExchangeArbitrageOwnership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for index, account := range []struct {
		id       string
		exchange string
		hash     string
	}{
		{"buy-account-cross_exchange_arbitrage", "binance", strings.Repeat("b", 64)},
		{"sell-account-cross_exchange_arbitrage", "bybit", strings.Repeat("c", 64)},
	} {
		transactionID := "journal-initialization-cross_exchange_arbitrage-" + account.exchange
		if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES ($1,'portfolio_initialization','run-cross_exchange_arbitrage','portfolio-cross_exchange_arbitrage',
'configuration-cross_exchange_arbitrage',$2,$2,$3,$4)`, transactionID, "initialize-"+account.exchange, now, index+1); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES ($1,1,'available_asset','crossarb','USDT','debit',100),
($1,2,'external_equity','crossarb','USDT','credit',100)`, transactionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO portfolio_ownership(
account_id,portfolio_id,exchange_id,strategy_version_id,strategy_key,
initialization_transaction_id,numeraire_asset,ownership_hash,created_at
) VALUES ($1,'portfolio-cross_exchange_arbitrage',$2,'cross-exchange-arbitrage-1-0-0','cross_exchange',
$3,'USDT',$4,$5)`, account.id, account.exchange, transactionID, account.hash, now); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedCrossExchangeArbitrageCoherentView(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	viewID string,
	now time.Time,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO cross_market_view_headers(
id,version_vector_hash,policy_version,maximum_book_age_nanos,
maximum_inter_book_skew_nanos,maximum_clock_uncertainty_nanos,
trigger_monotonic_nanos,trigger_ingest_ordinal,trigger_utc,
trigger_utc_unix_nanos,member_count,created_at
) VALUES ($1,$1,'axiom.coherent-view-policy.v1',250000000,250000000,100000000,
200,100,$2,$3,2,$2)`, viewID, now, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	for index, exchange := range []string{"binance", "bybit"} {
		receive := now.Add(time.Duration(index) * time.Nanosecond)
		intervalStart := receive.Add(-time.Nanosecond)
		intervalEnd := receive.Add(time.Nanosecond)
		if _, err = tx.Exec(ctx, `INSERT INTO cross_market_view_members(
cross_market_view_id,member_ordinal,exchange_id,instrument_id,book_version,
connection_generation,receive_monotonic_nanos,receive_utc,receive_utc_unix_nanos,
ingest_ordinal,clock_offset_nanos,clock_uncertainty_nanos,clock_interval_start,
clock_interval_end,state_hash,collector_instance,collector_region
) VALUES ($1,$2,$3,'BTCUSDT',1,1,$4,$5,$6,$7,0,1,$8,$9,$10,$11,'test-region')`,
			viewID, index, exchange, 100+index, receive, receive.UnixNano(), index+1,
			intervalStart, intervalEnd,
			strings.Repeat(string(rune('f'-index)), 64), "collector-"+exchange); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedCrossExchangeArbitrageDecision(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id, viewID string,
	now time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO decisions(
id,run_id,configuration_id,strategy_version_id,outcome,reason_code,
causation_id,decided_at,ingest_ordinal,decision_market_scope,cross_market_view_id
) VALUES ($1,'run-cross_exchange_arbitrage','configuration-cross_exchange_arbitrage','cross-exchange-arbitrage-1-0-0','approved',
'cross_exchange.entry.accepted',$2,$3,10,'cross_market',$4)`,
		id, "cause-"+id, now, viewID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO risk_evaluations(
id,decision_id,policy_version,outcome,reason_code,evaluated_at,action,effective_state
) VALUES ($1,$2,'cross-exchange-risk.v1','approved','approved',$3,'approve','NORMAL')`,
		"risk-"+id, id, now)
	if err != nil {
		t.Fatal(err)
	}
}

func crossExchangeArbitrageDatabaseCandidateWrite(
	decisionID, configurationHash, viewID string,
	now time.Time,
) CrossExchangeArbitrageCandidateWrite {
	write := crossExchangeArbitrageCandidateWriteFixture()
	write.Candidate.DecisionID = decisionID
	write.Candidate.ConfigurationID = "configuration-cross_exchange_arbitrage"
	write.Candidate.StrategyVersionID = "cross-exchange-arbitrage-1-0-0"
	write.Candidate.ConfigurationHash = configurationHash
	write.Candidate.CoherentViewID = viewID
	write.Candidate.CanonicalHash = researchRegistryPayloadHash([]byte(decisionID))
	write.Candidate.RiskEvaluationID = "risk-" + decisionID
	write.Candidate.RecordedAt = pgTimestamp(now)
	for index := range write.Members {
		member := &write.Members[index]
		member.DecisionID, member.CoherentViewID = decisionID, viewID
		member.ReceiveUtc = pgTimestamp(now.Add(time.Duration(index) * time.Nanosecond))
		member.ReceiveUtcUnixNanos = member.ReceiveUtc.Time.UnixNano()
		member.ClockIntervalStart = pgTimestamp(member.ReceiveUtc.Time.Add(-time.Nanosecond))
		member.ClockIntervalEnd = pgTimestamp(member.ReceiveUtc.Time.Add(time.Nanosecond))
		member.StateHash = strings.Repeat(string(rune('f'-index)), 64)
	}
	for index := range write.Legs {
		write.Legs[index].DecisionID = decisionID
		write.Legs[index].InstrumentMetadataID = []string{
			"metadata-binance-cross_exchange_arbitrage", "metadata-bybit-cross_exchange_arbitrage",
		}[index]
	}
	for index := range write.Inventories {
		write.Inventories[index].DecisionID = decisionID
	}
	write.Inventories[0].BandState = "paused_depleted"
	write.Inventories[0].NaturalReversePreferred = false
	return write
}

func assertCrossExchangeArbitrageCandidateEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *CrossExchangeArbitrageRepository,
) {
	t.Helper()
	candidate, members, legs, inventory, err := repository.LoadCandidate(ctx, "decision-cross_exchange_arbitrage-good")
	if err != nil || candidate.Direction != "buy_binance_sell_bybit" ||
		len(members) != 2 || len(legs) != 2 || len(inventory) != 2 {
		t.Fatalf("cross-exchange arbitrage restart/load mismatch: %#v %#v %#v %#v %v",
			candidate, members, legs, inventory, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE cross_exchange_candidates
SET direction='buy_bybit_sell_binance' WHERE decision_id='decision-cross_exchange_arbitrage-good'`); err == nil {
		t.Fatal("immutable cross-exchange arbitrage candidate mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM cross_exchange_candidate_members
WHERE decision_id='decision-cross_exchange_arbitrage-good' AND member_ordinal=0`); err == nil {
		t.Fatal("immutable cross-exchange arbitrage member deleted")
	}
}

func assertCrossExchangeArbitrageAtomicClaims(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *CrossExchangeArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	ids, quantities := registerCrossExchangeArbitrageClaimResources(t, ctx, repository, now)
	write := generated.ClaimCrossExchangeArbitrageResourcesParams{
		GroupID: "cross_exchange_arbitrage-claim-good", DecisionID: "decision-cross_exchange_arbitrage-good", FencingToken: 7,
		CorrelationID: "claim-cross_exchange_arbitrage", CausationID: "claim-cross_exchange_arbitrage", ResourceIds: ids,
		Quantities: quantities, RecordedAt: pgTimestamp(now),
	}
	activeGroup := assertCrossExchangeArbitrageClaimContention(t, ctx, pool, repository, write)
	assertCrossExchangeArbitrageClaimQuarantine(t, ctx, pool, repository, activeGroup, now)
}

func registerCrossExchangeArbitrageClaimResources(
	t *testing.T,
	ctx context.Context,
	repository *CrossExchangeArbitrageRepository,
	now time.Time,
) ([]string, []pgtype.Numeric) {
	t.Helper()
	kinds := []string{"balance", "balance", "fee_buffer", "fee_buffer", "liquidity", "liquidity", "recovery"}
	ids := make([]string, len(kinds))
	quantities := make([]pgtype.Numeric, len(kinds))
	for index, kind := range kinds {
		ids[index] = "cross_exchange_arbitrage-resource-" + string(rune('a'+index))
		quantities[index] = triangularArbitrageNumeric("1")
		if err := repository.RegisterClaimResource(ctx, generated.RegisterCrossExchangeArbitrageClaimResourceParams{
			PID: ids[index], PAccountID: "buy-account-cross_exchange_arbitrage",
			PExchangeID: "portfolio", PResourceKind: kind,
			PResourceKey: kind + "-" + string(rune('a'+index)),
			PAvailable:   "1", PRecordedAt: pgTimestamp(now),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return ids, quantities
}

func assertCrossExchangeArbitrageClaimContention(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *CrossExchangeArbitrageRepository,
	write generated.ClaimCrossExchangeArbitrageResourcesParams,
) string {
	t.Helper()
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			candidate := write
			candidate.GroupID = "cross_exchange_arbitrage-claim-" + string(rune('a'+index))
			results <- repository.Claim(ctx, candidate)
		}(index)
	}
	wait.Wait()
	close(results)
	successes := 0
	for claimErr := range results {
		if claimErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("cross-exchange arbitrage concurrent claim successes=%d", successes)
	}
	var activeGroup string
	if err := pool.QueryRow(ctx,
		"SELECT id FROM cross_exchange_arbitrage_claim_groups WHERE state='active'",
	).Scan(&activeGroup); err != nil {
		t.Fatal(err)
	}
	return activeGroup
}

func assertCrossExchangeArbitrageClaimQuarantine(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *CrossExchangeArbitrageRepository,
	activeGroup string,
	now time.Time,
) {
	t.Helper()
	if err := repository.Close(ctx, generated.CloseCrossExchangeArbitrageClaimGroupParams{
		PGroupID: activeGroup, PExpectedRevision: 1, PFencingToken: 8,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now),
	}); err == nil {
		t.Fatal("stale cross-exchange arbitrage fence accepted")
	}
	if err := repository.Close(ctx, generated.CloseCrossExchangeArbitrageClaimGroupParams{
		PGroupID: activeGroup, PExpectedRevision: 1, PFencingToken: 7,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now),
	}); err != nil {
		t.Fatal(err)
	}
	var heldExactly bool
	if err := pool.QueryRow(ctx,
		"SELECT sum(held_quantity) = 7 FROM cross_exchange_arbitrage_claim_resources",
	).Scan(&heldExactly); err != nil || !heldExactly {
		t.Fatalf("quarantined resources remained held=%t error=%v", heldExactly, err)
	}
}

func assertCrossExchangeArbitrageOutcomeAndAccounting(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *CrossExchangeArbitrageRepository,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO execution_plans(
id,decision_id,state,recovery_state,revision,created_at,updated_at
) VALUES ('plan-cross_exchange_arbitrage-good','decision-cross_exchange_arbitrage-good','completed','none',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	write := crossExchangeArbitrageOutcomeWriteFixture()
	write.Simulation.DecisionID = "decision-cross_exchange_arbitrage-good"
	write.Simulation.PlanID = "plan-cross_exchange_arbitrage-good"
	write.Simulation.RecordedAt = pgTimestamp(now)
	write.Simulation.CanonicalHash = strings.Repeat("9", 64)
	for index := range write.Legs {
		write.Legs[index].DecisionID = "decision-cross_exchange_arbitrage-good"
	}
	write.Rebalancing.DecisionID = "decision-cross_exchange_arbitrage-good"
	write.Rebalancing.RecordedAt = pgTimestamp(now)
	seedCrossExchangeArbitrageJournalTransactions(t, ctx, pool, &write, now)
	if err := repository.RecordOutcome(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE cross_exchange_simulation_outcomes
SET outcome='both_missed' WHERE decision_id='decision-cross_exchange_arbitrage-good'`); err == nil {
		t.Fatal("immutable cross-exchange arbitrage simulation mutated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM cross_exchange_journal_links
WHERE decision_id='decision-cross_exchange_arbitrage-good' AND category='fees'`); err == nil {
		t.Fatal("immutable cross-exchange arbitrage journal link deleted")
	}
}

func seedCrossExchangeArbitrageJournalTransactions(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	write *CrossExchangeArbitrageOutcomeWrite,
	now time.Time,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for index := range write.Journals {
		id := "journal-cross_exchange_arbitrage-outcome-" + string(rune('a'+index))
		write.Journals[index].DecisionID = "decision-cross_exchange_arbitrage-good"
		write.Journals[index].TransactionID = id
		if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES ($1,'cross_exchange_arbitrage_attribution','run-cross_exchange_arbitrage','portfolio-cross_exchange_arbitrage','configuration-cross_exchange_arbitrage',
$2,$2,$3,$4)`, id, "cause-"+id, now, 100+index); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(
transaction_id,line_number,account_class,account_owner,asset_symbol,direction,quantity
) VALUES ($1,1,'realized_pnl','crossarb','USDT','debit',0.01),
($1,2,'external_equity','crossarb','USDT','credit',0.01)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func assertCrossExchangeArbitrageRoleMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_CROSS_EXCHANGE_ARBITRAGE_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_CROSS_EXCHANGE_ARBITRAGE_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_CROSS_EXCHANGE_ARBITRAGE_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	signature := "claim_cross_exchange_arbitrage_resources(text,text,bigint,text,text,text[],numeric[],timestamp with time zone)"
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
		t.Fatalf("cross-exchange arbitrage function role matrix runtime=%t readonly=%t", runtimeExecute, readonlyExecute)
	}
}
