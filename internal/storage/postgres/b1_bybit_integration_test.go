package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestB1PostgresBybitPublicMigrationQualification(t *testing.T) {
	dsn := os.Getenv("AXIOM_B1_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_B1_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_b1_test") {
		t.Fatal("B1 integration requires a dedicated database ending _b1_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		t.Fatalf("B1 migrations = %d/%d, %v", applied, len(migrations), applyErr)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM exchanges
WHERE id='bybit' AND environment='production_public'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Bybit reference = %d, %v", count, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING;
INSERT INTO instruments(id,base_asset,quote_asset,product) VALUES ('BTCUSDT','BTC','USDT','spot') ON CONFLICT DO NOTHING;
INSERT INTO public_connection_events(id,exchange_id,instrument_id,recorder_session,connection_id,
connection_generation,state,reason,observed_at,ingest_ordinal)
VALUES ('event-1','bybit','BTCUSDT','session-1','connection-1',1,'HEALTHY','fixture',clock_timestamp(),1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE public_connection_events SET reason='mutated' WHERE id='event-1'"); err == nil {
		t.Fatal("immutable public connection evidence accepted update")
	}
}
