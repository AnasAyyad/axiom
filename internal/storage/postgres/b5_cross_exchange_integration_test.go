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

func TestB5PostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openB5TestDatabase(t, "AXIOM_B5_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 17)
	assertB5SchemaAndPersistence(t, ctx, pool)
}

func TestB5PostgresB4ToB5UpgradeQualification(t *testing.T) {
	ctx, pool := openB5TestDatabase(t, "AXIOM_B5_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 16)
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
		t.Fatalf("B4-to-B5 migration changed=%t error=%v", changed, err)
	}
	assertB5SchemaAndPersistence(t, ctx, pool)
}

func openB5TestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b5_test") {
		t.Fatal("B5 integration requires a dedicated database ending _b5_test")
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

func assertB5SchemaAndPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	configurationHash, coherentViewID := seedB5References(t, ctx, pool, now)
	repository, err := NewB5Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	seedB5Decision(t, ctx, pool, "decision-b5-good", coherentViewID, now)
	write := b5DatabaseCandidateWrite("decision-b5-good", configurationHash, coherentViewID, now)
	if err = repository.RecordCandidate(ctx, write); err != nil {
		t.Fatal(err)
	}
	seedB5Decision(t, ctx, pool, "decision-b5-tamper", coherentViewID, now)
	tampered := b5DatabaseCandidateWrite(
		"decision-b5-tamper", strings.Repeat("0", 64), coherentViewID, now,
	)
	if err = repository.RecordCandidate(ctx, tampered); err == nil {
		t.Fatal("mismatched registered configuration hash persisted")
	}
	assertB5CandidateEvidence(t, ctx, pool, repository)
	assertB5AtomicClaims(t, ctx, pool, repository, now.Add(time.Minute))
	assertB5OutcomeAndAccounting(t, ctx, pool, repository, now.Add(2*time.Minute))
	assertB5RoleMatrix(t, ctx, pool)
}

func seedB5References(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	now time.Time,
) (string, string) {
	t.Helper()
	canonical, err := json.Marshal(config.DefaultV1BConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	configurationHash := a10PayloadHash(canonical)
	viewID := strings.Repeat("e", 64)
	statements := b5ReferenceSeedStatements(configurationHash, canonical, viewID, now)
	for index, statement := range statements {
		if _, err = pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("B5 reference seed %d failed: %v", index+1, err)
		}
	}
	seedB5Ownership(t, ctx, pool, now)
	seedB5CoherentView(t, ctx, pool, viewID, now)
	return configurationHash, viewID
}

func b5ReferenceSeedStatements(
	configurationHash string,
	canonical []byte,
	viewID string,
	now time.Time,
) []b4SeedStatement {
	hash := strings.Repeat("a", 64)
	statements := b5StrategyReferenceSeedStatements(configurationHash, canonical, hash, now)
	return append(statements, b5PortfolioReferenceSeedStatements(hash, viewID, now)...)
}

func b5StrategyReferenceSeedStatements(
	configurationHash string,
	canonical []byte,
	hash string,
	now time.Time,
) []b4SeedStatement {
	return []b4SeedStatement{
		{`INSERT INTO configuration_versions(
id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('configuration-b5',1,$1,$2,'b5-qualification',$3)`,
			[]any{configurationHash, canonical, now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{`INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES
('BTCUSDT','BTC','USDT','spot'),('ETHUSDT','ETH','USDT','spot')`, nil},
		{`INSERT INTO instrument_metadata_versions(
id,exchange_id,instrument_id,version,price_tick,quantity_step,
minimum_quantity,minimum_notional,effective_at,recorded_at
) VALUES
('metadata-binance-b5','binance','BTCUSDT',1,0.01,0.000001,0.000001,0.01,$1,$1),
('metadata-bybit-b5','bybit','BTCUSDT',1,0.01,0.000001,0.000001,0.01,$1,$1)`,
			[]any{now}},
		{`INSERT INTO strategy_definitions(id,name,family)
VALUES ('cross-exchange-b5','Cross Exchange Arbitrage V1B','cross_exchange')`, nil},
		{`INSERT INTO strategy_versions(
id,strategy_id,version,implementation_hash,promotion_status,created_at
) VALUES ('cross-exchange-v1b-1','cross-exchange-b5',1,$1,'research',$2)`,
			[]any{hash, now}},
		{`INSERT INTO model_versions(
id,model_type,version,model_hash,canonical_payload,created_at
) VALUES
('depth-b5','depth',1,$1,'{}',$2),
('claim-b5','claim',1,$1,'{}',$2),
('fee-b5','fee',1,$1,'{}',$2),
('latency-b5','latency',1,$1,'{}',$2),
('recovery-b5','recovery',1,$1,'{}',$2),
('shadow-b5','inventory_shadow',1,$1,'{}',$2),
('concentration-b5','concentration',1,$1,'{}',$2)`, []any{hash, now}},
	}
}

func b5PortfolioReferenceSeedStatements(
	hash, viewID string,
	now time.Time,
) []b4SeedStatement {
	return []b4SeedStatement{
		{`INSERT INTO runs(
id,mode,configuration_id,strategy_version_id,root_seed_hash,
reproducibility_hash,state,created_at
) VALUES (
'run-b5','backtest','configuration-b5','cross-exchange-v1b-1',$1,$1,'created',$2
)`, []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-b5','Cross Exchange B5','USDT',$1)", []any{now}},
		{`INSERT INTO virtual_accounts VALUES
('buy-account-b5','portfolio-b5','run-b5','crossarb-binance',$1),
('sell-account-b5','portfolio-b5','run-b5','crossarb-bybit',$1)`, []any{now}},
		{`INSERT INTO virtual_balances VALUES
('buy-account-b5','USDT',100,0,1,$1),('buy-account-b5','BTC',20,0,1,$1),
('sell-account-b5','USDT',100,0,1,$1),('sell-account-b5','BTC',80,0,1,$1)`,
			[]any{now}},
		{`SELECT $1::sha256_hex`, []any{viewID}},
	}
}

func seedB5Ownership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
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
		{"buy-account-b5", "binance", strings.Repeat("b", 64)},
		{"sell-account-b5", "bybit", strings.Repeat("c", 64)},
	} {
		transactionID := "journal-initialization-b5-" + account.exchange
		if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES ($1,'portfolio_initialization','run-b5','portfolio-b5',
'configuration-b5',$2,$2,$3,$4)`, transactionID, "initialize-"+account.exchange, now, index+1); err != nil {
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
) VALUES ($1,'portfolio-b5',$2,'cross-exchange-v1b-1','cross_exchange',
$3,'USDT',$4,$5)`, account.id, account.exchange, transactionID, account.hash, now); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedB5CoherentView(
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

func seedB5Decision(
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
) VALUES ($1,'run-b5','configuration-b5','cross-exchange-v1b-1','approved',
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

func b5DatabaseCandidateWrite(
	decisionID, configurationHash, viewID string,
	now time.Time,
) B5CandidateWrite {
	write := b5CandidateWriteFixture()
	write.Candidate.DecisionID = decisionID
	write.Candidate.ConfigurationID = "configuration-b5"
	write.Candidate.StrategyVersionID = "cross-exchange-v1b-1"
	write.Candidate.ConfigurationHash = configurationHash
	write.Candidate.CoherentViewID = viewID
	write.Candidate.CanonicalHash = a10PayloadHash([]byte(decisionID))
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
			"metadata-binance-b5", "metadata-bybit-b5",
		}[index]
	}
	for index := range write.Inventories {
		write.Inventories[index].DecisionID = decisionID
	}
	write.Inventories[0].BandState = "paused_depleted"
	write.Inventories[0].NaturalReversePreferred = false
	return write
}

func assertB5CandidateEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B5Repository,
) {
	t.Helper()
	candidate, members, legs, inventory, err := repository.LoadCandidate(ctx, "decision-b5-good")
	if err != nil || candidate.Direction != "buy_binance_sell_bybit" ||
		len(members) != 2 || len(legs) != 2 || len(inventory) != 2 {
		t.Fatalf("B5 restart/load mismatch: %#v %#v %#v %#v %v",
			candidate, members, legs, inventory, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE cross_exchange_candidates
SET direction='buy_bybit_sell_binance' WHERE decision_id='decision-b5-good'`); err == nil {
		t.Fatal("immutable B5 candidate mutated")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM cross_exchange_candidate_members
WHERE decision_id='decision-b5-good' AND member_ordinal=0`); err == nil {
		t.Fatal("immutable B5 member deleted")
	}
}

func assertB5AtomicClaims(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B5Repository,
	now time.Time,
) {
	t.Helper()
	ids, quantities := registerB5ClaimResources(t, ctx, repository, now)
	write := generated.ClaimB5ResourcesParams{
		GroupID: "b5-claim-good", DecisionID: "decision-b5-good", FencingToken: 7,
		CorrelationID: "claim-b5", CausationID: "claim-b5", ResourceIds: ids,
		Quantities: quantities, RecordedAt: pgTimestamp(now),
	}
	activeGroup := assertB5ClaimContention(t, ctx, pool, repository, write)
	assertB5ClaimQuarantine(t, ctx, pool, repository, activeGroup, now)
}

func registerB5ClaimResources(
	t *testing.T,
	ctx context.Context,
	repository *B5Repository,
	now time.Time,
) ([]string, []pgtype.Numeric) {
	t.Helper()
	kinds := []string{"balance", "balance", "fee_buffer", "fee_buffer", "liquidity", "liquidity", "recovery"}
	ids := make([]string, len(kinds))
	quantities := make([]pgtype.Numeric, len(kinds))
	for index, kind := range kinds {
		ids[index] = "b5-resource-" + string(rune('a'+index))
		quantities[index] = b4Numeric("1")
		if err := repository.RegisterClaimResource(ctx, generated.RegisterB5ClaimResourceParams{
			PID: ids[index], PAccountID: "buy-account-b5",
			PExchangeID: "portfolio", PResourceKind: kind,
			PResourceKey: kind + "-" + string(rune('a'+index)),
			PAvailable:   "1", PRecordedAt: pgTimestamp(now),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return ids, quantities
}

func assertB5ClaimContention(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B5Repository,
	write generated.ClaimB5ResourcesParams,
) string {
	t.Helper()
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			candidate := write
			candidate.GroupID = "b5-claim-" + string(rune('a'+index))
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
		t.Fatalf("B5 concurrent claim successes=%d", successes)
	}
	var activeGroup string
	if err := pool.QueryRow(ctx,
		"SELECT id FROM b5_claim_groups WHERE state='active'",
	).Scan(&activeGroup); err != nil {
		t.Fatal(err)
	}
	return activeGroup
}

func assertB5ClaimQuarantine(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B5Repository,
	activeGroup string,
	now time.Time,
) {
	t.Helper()
	if err := repository.Close(ctx, generated.CloseB5ClaimGroupParams{
		PGroupID: activeGroup, PExpectedRevision: 1, PFencingToken: 8,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now),
	}); err == nil {
		t.Fatal("stale B5 fence accepted")
	}
	if err := repository.Close(ctx, generated.CloseB5ClaimGroupParams{
		PGroupID: activeGroup, PExpectedRevision: 1, PFencingToken: 7,
		PNextState: "quarantined", PRecordedAt: pgTimestamp(now),
	}); err != nil {
		t.Fatal(err)
	}
	var heldExactly bool
	if err := pool.QueryRow(ctx,
		"SELECT sum(held_quantity) = 7 FROM b5_claim_resources",
	).Scan(&heldExactly); err != nil || !heldExactly {
		t.Fatalf("quarantined resources remained held=%t error=%v", heldExactly, err)
	}
}

func assertB5OutcomeAndAccounting(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *B5Repository,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO execution_plans(
id,decision_id,state,recovery_state,revision,created_at,updated_at
) VALUES ('plan-b5-good','decision-b5-good','completed','none',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	write := b5OutcomeWriteFixture()
	write.Simulation.DecisionID = "decision-b5-good"
	write.Simulation.PlanID = "plan-b5-good"
	write.Simulation.RecordedAt = pgTimestamp(now)
	write.Simulation.CanonicalHash = strings.Repeat("9", 64)
	for index := range write.Legs {
		write.Legs[index].DecisionID = "decision-b5-good"
	}
	write.Rebalancing.DecisionID = "decision-b5-good"
	write.Rebalancing.RecordedAt = pgTimestamp(now)
	seedB5JournalTransactions(t, ctx, pool, &write, now)
	if err := repository.RecordOutcome(ctx, write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE cross_exchange_simulation_outcomes
SET outcome='both_missed' WHERE decision_id='decision-b5-good'`); err == nil {
		t.Fatal("immutable B5 simulation mutated")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM cross_exchange_journal_links
WHERE decision_id='decision-b5-good' AND category='fees'`); err == nil {
		t.Fatal("immutable B5 journal link deleted")
	}
}

func seedB5JournalTransactions(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	write *B5OutcomeWrite,
	now time.Time,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for index := range write.Journals {
		id := "journal-b5-outcome-" + string(rune('a'+index))
		write.Journals[index].DecisionID = "decision-b5-good"
		write.Journals[index].TransactionID = id
		if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(
id,transaction_type,run_id,portfolio_id,configuration_id,causation_id,
correlation_id,recorded_at,ingest_ordinal
) VALUES ($1,'b5_attribution','run-b5','portfolio-b5','configuration-b5',
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

func assertB5RoleMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_B5_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_B5_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_B5_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	signature := "claim_b5_resources(text,text,bigint,text,text,text[],numeric[],timestamp with time zone)"
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
		t.Fatalf("B5 function role matrix runtime=%t readonly=%t", runtimeExecute, readonlyExecute)
	}
}
