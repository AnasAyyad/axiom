package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA8PostgresAtomicOrderFillJournalCheckpoint(t *testing.T) {
	dsn := os.Getenv("AXIOM_A8_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A8_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a8_test") {
		t.Fatal("A8 integration requires a dedicated database ending _a8_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertEmptyTestDatabase(t, ctx, pool)
	if _, err = ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedAccountingReferences(t, ctx, pool)
	seedA8References(t, ctx, pool)
	repository, err := NewA8Repository(pool)
	if err != nil {
		t.Fatal(err)
	}
	write := a8AtomicFixture()
	if err = repository.Commit(ctx, write); err != nil {
		t.Fatal(err)
	}
	assertA8AtomicState(t, ctx, pool, 1)
	write.Outbox.ID = "outbox-second"
	if err = repository.Commit(ctx, write); err == nil {
		t.Fatal("duplicate durable boundary committed")
	}
	assertA8AtomicState(t, ctx, pool, 1)
}

func seedA8References(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	statements := []string{
		"INSERT INTO model_versions VALUES ('fee-v1','fee',1,$1,'{}',NULL,$2)",
		"INSERT INTO model_versions VALUES ('latency-v1','latency',1,$1,'{}',NULL,$2)",
		"INSERT INTO model_versions VALUES ('fill-v1','fill',1,$1,'{}',NULL,$2)",
		"INSERT INTO model_namespaces VALUES ('namespace-a',$1,'production-public','combined-a','fee-v1','latency-v1','fill-v1',$1,'{}',$2)",
		"INSERT INTO opportunities VALUES ('opportunity-a','run-a','strategy-version-a','instrument-a','configuration-a',$2,1,$1)",
		"INSERT INTO decisions VALUES ('decision-a','opportunity-a','run-a','configuration-a','strategy-version-a','approved','approved',$1,$2,1)",
		"INSERT INTO execution_plans (id,decision_id,state,recovery_state,revision,created_at,updated_at) VALUES ('plan-a','decision-a','active',$1,1,$2,$2)",
		"INSERT INTO orders (id,plan_id,account_id,client_order_id,account_epoch,instrument_id,side,quantity,state,revision,created_at,updated_at) VALUES ('order-a','plan-a','account-a',$1,1,'instrument-a','buy',1,'created',1,$2,$2)",
	}
	for index, statement := range statements {
		if _, err := pool.Exec(ctx, statement, hash, now); err != nil {
			t.Fatalf("A8 reference seed %d failed: %v", index+1, err)
		}
	}
	states := []string{"validating", "reserved", "approved", "submitting"}
	for index, state := range states {
		_, err := pool.Exec(ctx, "UPDATE orders SET state=$1,revision=$2,last_event_ordinal=$3,updated_at=$4 WHERE id='order-a'",
			state, index+2, index+1, now.Add(time.Duration(index+1)*time.Second))
		if err != nil {
			t.Fatal("A8 order seed transition failed")
		}
	}
}

func a8AtomicFixture() A8AtomicWrite {
	now := pgtype.Timestamptz{Time: time.Date(2026, 7, 16, 10, 1, 0, 0, time.UTC), Valid: true}
	hash := strings.Repeat("b", 64)
	ordinal := int64(5)
	prior, status, namespace := "submitting", "PARTIALLY_FILLED", "namespace-a"
	orderID, fillID := "order-a", "fill-a"
	return A8AtomicWrite{
		OrderEvent: generated.InsertCanonicalOrderEventParams{ID: "event-a", OrderID: orderID,
			ExchangeEventIdentity: "simulated-event-a", PriorState: &prior, NewState: "partially_filled", Revision: 6,
			CausationID: "cause", OccurredAt: now, IngestOrdinal: &ordinal, EventHash: hash,
			ExchangeStatus: &status, CumulativeQuantity: "0.5", CanonicalPayload: []byte("{}")},
		Order: generated.ReduceCanonicalOrderParams{ID: orderID, State: "partially_filled", ExchangeStatus: status,
			CumulativeQuantity: "0.5", CumulativeFee: "0.05", CumulativeRebate: "0", LastEventOrdinal: ordinal,
			UpdatedAt: now, Revision: 5},
		Fills: []generated.InsertCanonicalFillParams{{ID: fillID, OrderID: orderID, ExchangeID: "exchange-a",
			ExchangeFillID: "sim-fill-a", Quantity: "0.5", Price: "100", FeeQuantity: "0.05", FeeAsset: "USDT",
			OccurredAt: now, RebateQuantity: "0", IngestOrdinal: &ordinal, FillHash: hash}},
		Journal: generated.InsertJournalTransactionParams{ID: "journal-a", TransactionType: "simulated_fill",
			RunID: "run-a", PortfolioID: "portfolio-a", OrderID: &orderID, FillID: &fillID,
			ConfigurationID: "configuration-a", CausationID: "cause", CorrelationID: "correlation", RecordedAt: now, IngestOrdinal: ordinal},
		Entries: []generated.InsertLedgerEntryParams{
			{TransactionID: "journal-a", LineNumber: 1, AccountClass: "strategy_inventory", AccountOwner: "account-a", AssetSymbol: "BTC", Direction: "debit", Quantity: "0.5"},
			{TransactionID: "journal-a", LineNumber: 2, AccountClass: "available_asset", AccountOwner: "account-a", AssetSymbol: "BTC", Direction: "credit", Quantity: "0.5"},
			{TransactionID: "journal-a", LineNumber: 3, AccountClass: "fee_expense", AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "debit", Quantity: "0.05"},
			{TransactionID: "journal-a", LineNumber: 4, AccountClass: "available_asset", AccountOwner: "account-a", AssetSymbol: "USDT", Direction: "credit", Quantity: "0.05"},
		},
		Postings: []generated.InsertFillJournalPostingParams{{FillID: fillID, TransactionID: "journal-a", PostingKind: "fill"}},
		Checkpoint: generated.InsertA8CheckpointParams{ID: "checkpoint-a", RunID: "run-a", Revision: 1,
			InputOrdinal: ordinal, StateHash: hash, Payload: []byte("{}"), CreatedAt: now, CursorLogicalTime: &ordinal,
			OrdersHash: hash, PlansHash: hash, LiquidityHash: hash, JournalHash: hash, ProjectionHash: hash,
			ModelNamespaceID: &namespace, DeterministicStateHash: hash},
		Outbox: generated.InsertOutboxParams{ID: "outbox-a", Topic: "virtual.order.reduced", PayloadHash: hash, CreatedAt: now},
	}
}

func assertA8AtomicState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected int) {
	t.Helper()
	for _, table := range []string{"order_events", "fills", "journal_transactions", "fill_journal_postings", "run_checkpoints", "outbox_events"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != expected {
			t.Fatalf("atomic table count mismatch: %s", table)
		}
	}
	var state, quantity string
	if err := pool.QueryRow(ctx, "SELECT state,cumulative_quantity::text FROM orders WHERE id='order-a'").Scan(&state, &quantity); err != nil ||
		state != "partially_filled" || quantity != "0.500000000000000000" {
		t.Fatal("canonical order projection mismatch")
	}
}
