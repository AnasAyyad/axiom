package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA9PostgresPortfolioRiskRecoveryQualification(t *testing.T) {
	dsn := os.Getenv("AXIOM_A9_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A9_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a9_test") {
		t.Fatal("A9 integration requires a dedicated database ending _a9_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("A9 migrations = %d %v", applied, applyErr)
	}
	assertA9RolePermissions(t, ctx, pool)
	seedA9References(t, ctx, pool)
	repository, err := NewA9Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	assertA9Initialization(t, ctx, pool, repository)
	winner := assertA9Contention(t, ctx, pool, repository)
	assertA9AtomicClose(t, ctx, pool, repository, winner)
	assertA9AtomicPartialFills(t, ctx, pool, repository)
	assertA9RiskReconciliationEvidence(t, ctx, pool)
	assertA9RecoveryEvidence(t, ctx, pool)
}

func assertA9AtomicPartialFills(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A9Repository) {
	t.Helper()
	seedA9ExecutionOrder(t, ctx, pool)
	allocation := a9AllocationFixture(99)
	allocation.LiquidityRevision = 3
	if err := repository.Reserve(ctx, allocation); err != nil {
		t.Fatal(err)
	}
	first := a9FillFixture(false)
	stale := first
	stale.Liquidity.Revision++
	if err := repository.SettleFill(ctx, stale); err == nil {
		t.Fatal("stale partial-fill CAS committed")
	}
	assertA9FillCounts(t, ctx, pool, 0)
	if err := repository.SettleFill(ctx, first); err != nil {
		t.Fatal(err)
	}
	assertA9PartialProjection(t, ctx, pool)
	second := a9FillFixture(true)
	if err := repository.SettleFill(ctx, second); err != nil {
		t.Fatal(err)
	}
	assertA9FillCounts(t, ctx, pool, 2)
	var fundsState, fundsRemaining, liquidityState, liquidityRemaining, usdtAvailable, usdtReserved, btcAvailable string
	_ = pool.QueryRow(ctx, "SELECT state,remaining_quantity::text FROM reservations WHERE id='funds-99'").Scan(&fundsState, &fundsRemaining)
	_ = pool.QueryRow(ctx, "SELECT state,remaining_quantity::text FROM liquidity_reservations WHERE id='liquidity-99'").Scan(&liquidityState, &liquidityRemaining)
	_ = pool.QueryRow(ctx, "SELECT available::text,reserved::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='USDT'").Scan(&usdtAvailable, &usdtReserved)
	_ = pool.QueryRow(ctx, "SELECT available::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='BTC'").Scan(&btcAvailable)
	if fundsState != "consumed" || fundsRemaining != "0.000000000000000000" ||
		liquidityState != "consumed" || liquidityRemaining != "0.000000000000000000" ||
		usdtAvailable != "399.900000000000000000" || usdtReserved != "0.000000000000000000" ||
		btcAvailable != "1.000000000000000000" {
		t.Fatalf("final A9 fill = %s/%s %s/%s %s/%s/%s", fundsState, fundsRemaining,
			liquidityState, liquidityRemaining, usdtAvailable, usdtReserved, btcAvailable)
	}
}

func seedA9ExecutionOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 16, 10, 6, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, "INSERT INTO execution_plans (id,decision_id,state,recovery_state,revision,created_at,updated_at) VALUES ('plan-a','decision-a','active',$1,1,$2,$2)", hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO orders (id,plan_id,account_id,client_order_id,account_epoch,instrument_id,side,quantity,state,revision,created_at,updated_at) VALUES ('order-a','plan-a','account-a',$1,1,'instrument-a','buy',1,'created',1,$2,$2)", hash, now); err != nil {
		t.Fatal(err)
	}
	for index, state := range []string{"validating", "reserved", "approved", "submitting"} {
		if _, err := pool.Exec(ctx, "UPDATE orders SET state=$1,revision=$2,last_event_ordinal=$3,updated_at=$4 WHERE id='order-a'",
			state, index+2, index+1, now.Add(time.Duration(index+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}

type a9FillConfig struct {
	final                                                             bool
	now                                                               pgtype.Timestamptz
	ordinal, candidateRevision, claimRevision, domainRevision         int64
	usdtRevision, btcRevision, positionRevision, projectionRevision   int64
	debit, fillQuantity, usdtReserved, btcAvailable, positionQuantity string
}

func newA9FillConfig(final bool) a9FillConfig {
	nowValue := time.Date(2026, 7, 16, 10, 7, 0, 0, time.UTC)
	config := a9FillConfig{final: final, now: pgTimestamp(nowValue), ordinal: 5, debit: "40.04", fillQuantity: "0.4",
		candidateRevision: 1, claimRevision: 1, domainRevision: 4, usdtRevision: 4, btcRevision: 1,
		usdtReserved: "60.06", btcAvailable: "0.4", positionQuantity: "0.4"}
	if final {
		config.now, config.ordinal = pgTimestamp(nowValue.Add(time.Minute)), 6
		config.debit, config.fillQuantity = "60.06", "0.6"
		config.candidateRevision, config.claimRevision, config.domainRevision = 2, 2, 5
		config.usdtRevision, config.btcRevision, config.positionRevision, config.projectionRevision = 5, 2, 1, 1
		config.usdtReserved, config.btcAvailable, config.positionQuantity = "0", "1", "1"
	}
	return config
}

func a9FillFixture(final bool) A9FillSettlementWrite {
	config := newA9FillConfig(final)
	execution := a9ExecutionFixture(config)
	return A9FillSettlementWrite{Execution: execution,
		Candidate: generated.SettleAllocationCandidateFillParams{ID: "candidate-99", Revision: config.candidateRevision,
			FinalFill: final, UpdatedAt: config.now},
		Funds: generated.SettleReservationFillParams{ID: "funds-99", Revision: config.claimRevision, FencingToken: 7,
			DebitQuantity: config.debit, FinalFill: final, UpdatedAt: config.now},
		Liquidity: generated.SettleLiquidityReservationFillParams{ID: "liquidity-99", Revision: config.claimRevision,
			FencingToken: 7, FillQuantity: config.fillQuantity, FinalFill: final, UpdatedAt: config.now},
		Domain: generated.UpdateLiquidityDomainProjectionParams{ID: "combined-a", AvailableQuantity: "0",
			ExpectedRevision: config.domainRevision, UpdatedAt: config.now}}
}

func a9ExecutionFixture(config a9FillConfig) A8AtomicWrite {
	execution := a8AtomicFixture()
	execution.OrderEvent.OccurredAt, execution.Order.UpdatedAt = config.now, config.now
	execution.Journal.RecordedAt, execution.Checkpoint.CreatedAt, execution.Outbox.CreatedAt = config.now, config.now, config.now
	execution.OrderEvent.IngestOrdinal, execution.Fills[0].IngestOrdinal = &config.ordinal, &config.ordinal
	execution.OrderEvent.CumulativeQuantity, execution.Order.CumulativeQuantity = config.positionQuantity, config.positionQuantity
	execution.Order.LastEventOrdinal, execution.Journal.IngestOrdinal = config.ordinal, config.ordinal
	execution.Fills[0].Quantity, execution.Fills[0].OccurredAt = config.fillQuantity, config.now
	execution.Checkpoint.InputOrdinal, execution.Checkpoint.CursorLogicalTime = config.ordinal, &config.ordinal
	execution.Entries[0].Quantity, execution.Entries[1].Quantity = config.fillQuantity, config.fillQuantity
	execution.Entries[3].AccountClass, execution.Entries[5].AccountClass = "reserved_asset", "reserved_asset"
	execution.Balances = []generated.UpdateVirtualBalanceProjectionParams{
		{AccountID: "account-a", AssetSymbol: "USDT", Available: "399.9", Reserved: config.usdtReserved, UpdatedAt: config.now, ExpectedRevision: config.usdtRevision},
		{AccountID: "account-a", AssetSymbol: "BTC", Available: config.btcAvailable, Reserved: "0", UpdatedAt: config.now, ExpectedRevision: config.btcRevision},
	}
	execution.Positions = []generated.UpsertPositionProjectionParams{{AccountID: "account-a", InstrumentID: "instrument-a",
		Quantity: config.positionQuantity, WeightedAverageCost: "100.1", RealizedPnl: "0", UpdatedAt: config.now, ExpectedRevision: config.positionRevision}}
	for index := range execution.Projections {
		execution.Projections[index].UpdatedAt = config.now
		execution.Projections[index].ExpectedRevision = config.projectionRevision
	}
	if !config.final {
		execution.OrderEvent.CumulativeQuantity = "0.4"
		execution.Order.CumulativeFee = "0.04"
		execution.Fills[0].FeeQuantity = "0.04"
		execution.Entries[0].Quantity, execution.Entries[1].Quantity = "0.4", "0.4"
		execution.Entries[2].Quantity, execution.Entries[3].Quantity = "40", "40"
		execution.Entries[4].Quantity, execution.Entries[5].Quantity = "0.04", "0.04"
	} else {
		finalizeA9ExecutionFixture(&execution)
	}
	return execution
}

func finalizeA9ExecutionFixture(execution *A8AtomicWrite) {
	prior, status := "partially_filled", "FILLED"
	execution.OrderEvent.ID = "event-b"
	execution.OrderEvent.ExchangeEventIdentity = "simulated-event-b"
	execution.OrderEvent.PriorState = &prior
	execution.OrderEvent.NewState = "filled"
	execution.OrderEvent.Revision = 7
	execution.OrderEvent.ExchangeStatus = &status
	execution.Order.State = "filled"
	execution.Order.ExchangeStatus = status
	execution.Order.CumulativeFee = "0.1"
	execution.Order.Revision = 6
	execution.Fills[0].ID = "fill-b"
	execution.Fills[0].ExchangeFillID = "sim-fill-b"
	execution.Fills[0].FeeQuantity = "0.06"
	execution.Journal.ID = "journal-b"
	execution.Journal.FillID = &execution.Fills[0].ID
	for index := range execution.Entries {
		execution.Entries[index].TransactionID = execution.Journal.ID
	}
	execution.Entries[0].Quantity, execution.Entries[1].Quantity = "0.6", "0.6"
	execution.Entries[2].Quantity, execution.Entries[3].Quantity = "60", "60"
	execution.Entries[4].Quantity, execution.Entries[5].Quantity = "0.06", "0.06"
	execution.Postings[0].FillID = execution.Fills[0].ID
	execution.Postings[0].TransactionID = execution.Journal.ID
	for index := range execution.Projections {
		execution.Projections[index].SourceJournalID = execution.Journal.ID
	}
	execution.Checkpoint.ID = "checkpoint-b"
	execution.Checkpoint.Revision = 2
	execution.Outbox.ID = "outbox-b"
}

func assertA9FillCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected int) {
	t.Helper()
	for _, table := range []string{"order_events", "fills", "fill_journal_postings", "run_checkpoints", "outbox_events"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("A9 atomic fill count %s = %d %v", table, count, err)
		}
	}
	var journals int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM journal_transactions WHERE transaction_type='simulated_fill'").Scan(&journals)
	if journals != expected {
		t.Fatalf("A9 fill journals = %d", journals)
	}
}

func assertA9PartialProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var fundsState, fundsRemaining, liquidityState, liquidityRemaining, usdtReserved, btcAvailable, candidateState string
	_ = pool.QueryRow(ctx, "SELECT state,remaining_quantity::text FROM reservations WHERE id='funds-99'").Scan(&fundsState, &fundsRemaining)
	_ = pool.QueryRow(ctx, "SELECT state,remaining_quantity::text FROM liquidity_reservations WHERE id='liquidity-99'").Scan(&liquidityState, &liquidityRemaining)
	_ = pool.QueryRow(ctx, "SELECT reserved::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='USDT'").Scan(&usdtReserved)
	_ = pool.QueryRow(ctx, "SELECT available::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='BTC'").Scan(&btcAvailable)
	_ = pool.QueryRow(ctx, "SELECT state FROM allocation_candidates WHERE id='candidate-99'").Scan(&candidateState)
	if fundsState != "active" || fundsRemaining != "60.060000000000000000" || liquidityState != "active" ||
		liquidityRemaining != "0.600000000000000000" || usdtReserved != "60.060000000000000000" ||
		btcAvailable != "0.400000000000000000" || candidateState != "reserved" {
		t.Fatalf("partial A9 projection = %s/%s %s/%s %s/%s/%s", fundsState, fundsRemaining,
			liquidityState, liquidityRemaining, usdtReserved, btcAvailable, candidateState)
	}
}

func assertA9RolePermissions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	runtimeRole := testRole("AXIOM_A9_RUNTIME_ROLE", "axiom_runtime")
	recorderRole := testRole("AXIOM_A9_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_A9_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		role, table, privilege string
		want                   bool
	}{{runtimeRole, "risk_policies", "INSERT", true}, {runtimeRole, "risk_policies", "UPDATE", false},
		{runtimeRole, "liquidity_reservations", "UPDATE", true},
		{recorderRole, "portfolio_ownership", "SELECT", false},
		{readOnlyRole, "startup_recovery_evidence", "SELECT", true},
		{readOnlyRole, "startup_recovery_evidence", "INSERT", false}}
	for _, check := range checks {
		var allowed bool
		if err := pool.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)",
			check.role, check.table, check.privilege).Scan(&allowed); err != nil || allowed != check.want {
			t.Fatalf("A9 privilege %#v = %t %v", check, allowed, err)
		}
	}
}

func seedA9References(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO configuration_versions VALUES ('configuration-a',1,$1,'{}','test',$2)", []any{hash, now}},
		{"INSERT INTO assets(symbol) VALUES ('USDT'),('BTC'),('ETH')", nil},
		{"INSERT INTO exchanges VALUES ('exchange-a','binance','production_public')", nil},
		{"INSERT INTO instruments VALUES ('instrument-a','BTC','USDT','spot')", nil},
		{"INSERT INTO strategy_definitions VALUES ('strategy-a','trend','trend')", nil},
		{"INSERT INTO strategy_versions VALUES ('strategy-version-a','strategy-a',1,$1,'research',NULL,$2)", []any{hash, now}},
		{"INSERT INTO runs VALUES ('run-a','shadow','configuration-a','strategy-version-a',NULL,$1,$1,'created',$2,NULL,NULL)", []any{hash, now}},
		{"INSERT INTO portfolios VALUES ('portfolio-a','Trend V1A','USDT',$1)", []any{now}},
		{"INSERT INTO virtual_accounts VALUES ('account-a','portfolio-a','run-a','trend-binance',$1)", []any{now}},
		{"INSERT INTO virtual_balances VALUES ('account-a','USDT',500,0,1,$1),('account-a','BTC',0,0,1,$1),('account-a','ETH',0,0,1,$1)", []any{now}},
		{"INSERT INTO model_versions VALUES ('fee-v1','fee',1,$1,'{}',NULL,$2)", []any{hash, now}},
		{"INSERT INTO model_versions VALUES ('latency-v1','latency',1,$1,'{}',NULL,$2)", []any{hash, now}},
		{"INSERT INTO model_versions VALUES ('fill-v1','fill',1,$1,'{}',NULL,$2)", []any{hash, now}},
		{"INSERT INTO model_namespaces VALUES ('namespace-a',$1,'production-public','combined-a','fee-v1','latency-v1','fill-v1',$1,'{}',$2)", []any{hash, now}},
		{"INSERT INTO liquidity_domains VALUES ('combined-a','namespace-a',1,1,$1)", []any{now}},
		{"INSERT INTO opportunities VALUES ('opportunity-a','run-a','strategy-version-a','instrument-a','configuration-a',$2,1,$1)", []any{hash, now}},
		{"INSERT INTO decisions VALUES ('decision-a','opportunity-a','run-a','configuration-a','strategy-version-a','approved','approved','cause',$1,1)", []any{now}},
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("A9 seed %d failed: %v", index+1, err)
		}
	}
}

func assertA9Initialization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A9Repository) {
	t.Helper()
	now := pgTimestamp(time.Date(2026, 7, 16, 10, 1, 0, 0, time.UTC))
	hash := strings.Repeat("b", 64)
	write := A9InitializationWrite{
		Journal: generated.InsertJournalTransactionParams{ID: "journal-initialization", TransactionType: "portfolio_initialization",
			RunID: "run-a", PortfolioID: "portfolio-a", ConfigurationID: "configuration-a", CausationID: "initialization",
			CorrelationID: "initialization", RecordedAt: now, IngestOrdinal: 1},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: "journal-initialization", LineNumber: 1, AccountClass: "available_asset", AccountOwner: "trend", AssetSymbol: "USDT", Direction: "debit", Quantity: "500"},
			{TransactionID: "journal-initialization", LineNumber: 2, AccountClass: "external_equity", AccountOwner: "trend", AssetSymbol: "USDT", Direction: "credit", Quantity: "500"},
		},
		Ownership: generated.InsertPortfolioOwnershipParams{AccountID: "account-a", PortfolioID: "portfolio-a",
			ExchangeID: "exchange-a", StrategyVersionID: "strategy-version-a", StrategyKey: "trend",
			InitializationTransactionID: "journal-initialization", NumeraireAsset: "USDT", OwnershipHash: hash, CreatedAt: now},
		Snapshot: generated.InsertA9AccountSnapshotParams{ID: "snapshot-initialization", AccountID: "account-a", Revision: 1,
			SnapshotHash: hash, CanonicalPayload: []byte("{}"), RecordedAt: now, OwnershipHash: hash,
			BalancesHash: hash, PositionsHash: hash, ReservationsHash: hash, RiskStateHash: hash},
	}
	if err := repository.InitializeV1ATrend(ctx, write); err != nil {
		t.Fatal(err)
	}
	if err := repository.InitializeV1ATrend(ctx, write); err == nil {
		t.Fatal("duplicate initialization evidence committed")
	}
	for table, expected := range map[string]int{"portfolio_ownership": 1, "account_snapshots": 1,
		"journal_transactions": 1, "ledger_entries": 2} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("initialization table %s = %d %v", table, count, err)
		}
	}
}

type a9Winner struct{ index int }

func assertA9Contention(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A9Repository) a9Winner {
	t.Helper()
	var successes atomic.Int32
	winner := atomic.Int32{}
	winner.Store(-1)
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if repository.Reserve(ctx, a9AllocationFixture(index)) == nil {
				successes.Add(1)
				winner.CompareAndSwap(-1, int32(index))
			}
		}(index)
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("A9 successful reservations = %d", successes.Load())
	}
	var available, reserved, liquidity string
	if err := pool.QueryRow(ctx, "SELECT available::text,reserved::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='USDT'").Scan(&available, &reserved); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT available_quantity::text FROM liquidity_domains WHERE id='combined-a'").Scan(&liquidity); err != nil {
		t.Fatal(err)
	}
	if available != "399.900000000000000000" || reserved != "100.100000000000000000" ||
		liquidity != "0.000000000000000000" {
		t.Fatalf("A9 exclusive projections = %s/%s/%s", available, reserved, liquidity)
	}
	return a9Winner{index: int(winner.Load())}
}

func a9AllocationFixture(index int) A9AllocationWrite {
	suffix := a9Decimal(index)
	now := pgTimestamp(time.Date(2026, 7, 16, 10, 2, 0, 0, time.UTC))
	candidateID, fundsID, liquidityID := "candidate-"+suffix, "funds-"+suffix, "liquidity-"+suffix
	return A9AllocationWrite{
		Candidate: generated.InsertAllocationCandidateParams{ID: candidateID, AccountID: "account-a", InstrumentID: "instrument-a",
			Side: "buy", Quantity: "1", Notional: "100.1", AggregateScore: "1", BaseEligibilityVersion: 1,
			QuoteEligibilityVersion: 1, State: "reserved", ReasonCode: "approved", Revision: 1, CreatedAt: now, UpdatedAt: now},
		Scores: []generated.InsertAllocationScoreComponentParams{{CandidateID: candidateID,
			ComponentName: "worst_case", ComponentValue: "1", Ordinal: 0}},
		Funds: generated.InsertReservationParams{ID: fundsID, AccountID: "account-a", AssetSymbol: "USDT",
			Quantity: "100.1", FencingToken: 7, CreatedAt: now},
		FundsQuantity: "100.1", LiquidityDomainID: "combined-a", LiquidityRevision: 1, LiquidityQuantity: "1",
		Liquidity: generated.InsertLiquidityReservationParams{ID: liquidityID, CandidateID: candidateID,
			DomainID: "combined-a", Quantity: "1", FencingToken: 7, CreatedAt: now},
		Link: generated.LinkAllocationReservationsParams{CandidateID: candidateID,
			ReservationID: fundsID, LiquidityReservationID: liquidityID},
		Journal: generated.InsertJournalTransactionParams{ID: "journal-reserve-" + suffix,
			TransactionType: "reservation_created", RunID: "run-a", PortfolioID: "portfolio-a",
			ConfigurationID: "configuration-a", CausationID: candidateID, CorrelationID: candidateID,
			RecordedAt: now, IngestOrdinal: int64(100 + index)},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: "journal-reserve-" + suffix, LineNumber: 1, AccountClass: "reserved_asset",
				AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "debit", Quantity: "100.1"},
			{TransactionID: "journal-reserve-" + suffix, LineNumber: 2, AccountClass: "available_asset",
				AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "credit", Quantity: "100.1"},
		},
	}
}

func assertA9AtomicClose(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *A9Repository, winner a9Winner) {
	t.Helper()
	suffix := a9Decimal(winner.index)
	now := pgTimestamp(time.Date(2026, 7, 16, 10, 3, 0, 0, time.UTC))
	write := A9AllocationClose{
		Candidate: generated.CloseAllocationCandidateParams{ID: "candidate-" + suffix, State: "released",
			ReasonCode: "canceled", UpdatedAt: now, Revision: 1},
		Funds: generated.CloseReservationParams{ID: "funds-" + suffix, State: "released", UpdatedAt: now,
			Revision: 1, FencingToken: 7},
		FundsAccountID: "account-a", FundsAsset: "USDT", FundsQuantity: "100.1",
		Liquidity: generated.CloseLiquidityReservationParams{ID: "liquidity-" + suffix, State: "released",
			UpdatedAt: now, Revision: 2, FencingToken: 7},
		LiquidityDomainID: "combined-a", LiquidityQuantity: "1",
		Journal: generated.InsertJournalTransactionParams{ID: "journal-release-" + suffix,
			TransactionType: "reservation_released", RunID: "run-a", PortfolioID: "portfolio-a",
			ConfigurationID: "configuration-a", CausationID: "candidate-" + suffix,
			CorrelationID: "candidate-" + suffix, RecordedAt: now, IngestOrdinal: 200},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: "journal-release-" + suffix, LineNumber: 1, AccountClass: "available_asset",
				AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "debit", Quantity: "100.1"},
			{TransactionID: "journal-release-" + suffix, LineNumber: 2, AccountClass: "reserved_asset",
				AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "credit", Quantity: "100.1"},
		},
	}
	if err := repository.Close(ctx, write); err == nil {
		t.Fatal("stale liquidity CAS closed allocation")
	}
	var fundsState, liquidityState string
	if err := pool.QueryRow(ctx, "SELECT state FROM reservations WHERE id=$1", write.Funds.ID).Scan(&fundsState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT state FROM liquidity_reservations WHERE id=$1", write.Liquidity.ID).Scan(&liquidityState); err != nil {
		t.Fatal(err)
	}
	if fundsState != "active" || liquidityState != "active" {
		t.Fatal("failed close partially changed lifecycle")
	}
	write.Liquidity.Revision = 1
	if err := repository.Close(ctx, write); err != nil {
		t.Fatal(err)
	}
	var available, reserved, liquidity string
	_ = pool.QueryRow(ctx, "SELECT available::text,reserved::text FROM virtual_balances WHERE account_id='account-a' AND asset_symbol='USDT'").Scan(&available, &reserved)
	_ = pool.QueryRow(ctx, "SELECT available_quantity::text FROM liquidity_domains WHERE id='combined-a'").Scan(&liquidity)
	if available != "500.000000000000000000" || reserved != "0.000000000000000000" ||
		liquidity != "1.000000000000000000" {
		t.Fatalf("A9 released projections = %s/%s/%s", available, reserved, liquidity)
	}
}

func assertA9RiskReconciliationEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	queries := generated.New(pool)
	now := pgTimestamp(time.Date(2026, 7, 16, 10, 4, 0, 0, time.UTC))
	hash := strings.Repeat("c", 64)
	assertA9RiskEvidence(t, ctx, pool, queries, now, hash)
	assertA9ReconciliationEvidence(t, ctx, pool, queries, now, hash)
	for _, table := range []string{"risk_policies", "risk_policy_limits", "risk_evaluations", "risk_evaluation_policies",
		"risk_state_events", "circuit_breaker_events", "reconciliation_cases", "reconciliation_differences",
		"reconciliation_suspense", "quarantined_scopes"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("A9 evidence table %s = %d %v", table, count, err)
		}
	}
}

func assertA9RiskEvidence(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, queries *generated.Queries,
	now pgtype.Timestamptz, hash string,
) {
	t.Helper()
	_, err := queries.InsertRiskPolicy(ctx, generated.InsertRiskPolicyParams{ID: "risk-global", Version: 1,
		ScopeKind: "global", ScopeID: "engine", State: "PAUSED", PolicyHash: hash,
		CanonicalPayload: []byte("{}"), EffectiveAt: now, RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = queries.InsertRiskPolicyLimits(ctx, generated.InsertRiskPolicyLimitsParams{PolicyID: "risk-global", PolicyVersion: 1,
		AccountDrawdown: "0.05", UtcDayLoss: "0.01", Rolling24HourLoss: "0.01", StrategyLoss: "0.03",
		AssetExposure: "0.30", CombinedExposure: "0.50", ExchangeExposure: "0.60", MinimumReserve: "0.15",
		MaximumReservedCapital: "0.85", MaximumSpread: "0.01", MaximumSlippage: "0.005",
		MaximumOpenOrders: 8, MaximumBookAgeMicroseconds: 250000, MaximumQueueLagMicroseconds: 250000,
		MaximumClockDriftMicroseconds: 100000, MinimumQualityScore: 90})
	if err != nil {
		t.Fatal(err)
	}
	_, err = queries.InsertA9RiskEvaluation(ctx, generated.InsertA9RiskEvaluationParams{ID: "risk-evaluation-a",
		DecisionID: "decision-a", PolicyVersion: "risk-global:1", Outcome: "approved", ReasonCode: "approved",
		EvaluatedAt: now, Action: "approve", EffectiveState: "NORMAL", ObservationHash: hash, CanonicalPayload: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = queries.InsertRiskEvaluationPolicy(ctx, generated.InsertRiskEvaluationPolicyParams{
		EvaluationID: "risk-evaluation-a", PolicyID: "risk-global", PolicyVersion: 1, Precedence: 0})
	_, _ = queries.InsertRiskStateEvent(ctx, generated.InsertRiskStateEventParams{ID: "risk-state-a", PriorState: "PAUSED",
		NextState: "NORMAL", ReasonCode: "manual_recovery", Actor: "owner", EvidenceHash: hash, OccurredAt: now})
	_, _ = queries.InsertCircuitBreakerEvent(ctx, generated.InsertCircuitBreakerEventParams{ID: "breaker-a",
		BreakerKind: "reconciliation_mismatch", ScopeKind: "portfolio", ScopeID: "portfolio-a", Action: "quarantine",
		ResultingState: "LOCKED", EvidenceHash: hash, OccurredAt: now})
	if _, err = pool.Exec(ctx, "INSERT INTO incidents VALUES ('incident-a','critical','open','critical_reconciliation_mismatch',$1,NULL)", now.Time); err != nil {
		t.Fatal(err)
	}
	_ = pool
}

func assertA9ReconciliationEvidence(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, queries *generated.Queries,
	now pgtype.Timestamptz, hash string,
) {
	t.Helper()
	scope := "portfolio:portfolio-a"
	incident := "incident-a"
	_, err := queries.InsertA9ReconciliationCase(ctx, generated.InsertA9ReconciliationCaseParams{ID: "case-a",
		AccountID: "account-a", Classification: "inconsistent_fact", State: "quarantined", IncidentID: &incident,
		OpenedAt: now, Scope: &scope, ExpectedStateHash: hash, ActualStateHash: strings.Repeat("d", 64), CaseHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	asset := "USDT"
	_, _ = queries.InsertReconciliationDifference(ctx, generated.InsertReconciliationDifferenceParams{CaseID: "case-a", Ordinal: 0,
		Category: "balances", Classification: "inconsistent_fact", ExpectedHash: hash, ActualHash: strings.Repeat("d", 64),
		AssetSymbol: &asset, Quantity: "1", Critical: true, CanonicalPayload: []byte("{}")})
	_, _ = queries.InsertReconciliationSuspense(ctx, generated.InsertReconciliationSuspenseParams{
		CaseID: "case-a", AssetSymbol: "USDT", Quantity: "1", Reason: "balance_mismatch"})
	_, _ = queries.QuarantineScope(ctx, generated.QuarantineScopeParams{Scope: scope,
		ReasonCode: "critical_reconciliation_mismatch", CaseID: "case-a", QuarantinedAt: now})
	_ = pool
}

func assertA9RecoveryEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 16, 10, 5, 0, 0, time.UTC)
	hash := strings.Repeat("e", 64)
	queries := generated.New(pool)
	if _, err := queries.InsertStartupRecoveryAttempt(ctx, generated.InsertStartupRecoveryAttemptParams{
		ID: "recovery-a", RunID: "run-a", BuildHash: hash, ConfigurationHash: hash, StartedAt: pgTimestamp(now)}); err != nil {
		t.Fatal(err)
	}
	clock := now
	store, err := NewA9RecoveryEvidenceStore(ctx, pool, "recovery-a", func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := runtimecore.RecoverySequence()
	if err = store.Append(sequence[1], hash); err == nil {
		t.Fatal("out-of-order recovery evidence committed")
	}
	for _, stage := range sequence {
		if err = store.Append(stage, hash); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.Complete(); err != nil {
		t.Fatal(err)
	}
	var state string
	var count int
	_ = pool.QueryRow(ctx, "SELECT state FROM startup_recovery_attempts WHERE id='recovery-a'").Scan(&state)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM startup_recovery_evidence WHERE attempt_id='recovery-a'").Scan(&count)
	if state != "ready_paused" || count != len(sequence) {
		t.Fatalf("A9 recovery = %s/%d", state, count)
	}
}

func pgTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func a9Decimal(value int) string {
	if value == 0 {
		return "zero"
	}
	const digits = "0123456789"
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}
