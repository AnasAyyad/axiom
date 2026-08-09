package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDurableStoragePostgresRestoredRoleQualification(t *testing.T) {
	dsn := os.Getenv("AXIOM_DURABLE_STORAGE_RESTORE_DSN")
	if dsn == "" {
		t.Skip("AXIOM_DURABLE_STORAGE_RESTORE_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_durable_storage_test") {
		t.Fatal("durable storage restore qualification requires a dedicated database ending _durable_storage_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertRestoredFacts(t, ctx, pool)
	runtimeRole := testRole("AXIOM_DURABLE_STORAGE_RUNTIME_ROLE", "axiom_runtime")
	recorderRole := testRole("AXIOM_DURABLE_STORAGE_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_DURABLE_STORAGE_READONLY_ROLE", "axiom_readonly")
	if err = ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		t.Fatalf("restored role grants failed: %v", err)
	}
	assertRoleMatrix(t, ctx, pool, runtimeRole, recorderRole, readOnlyRole)
}

func assertRestoredFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var migrations, balances, journals, datasets int
	err := pool.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM schema_migrations),
      (SELECT count(*) FROM virtual_balances),
      (SELECT count(*) FROM journal_transactions WHERE sealed),
      (SELECT count(*) FROM dataset_manifests)`).Scan(&migrations, &balances, &journals, &datasets)
	if err != nil || migrations != 3 || balances == 0 || journals == 0 || datasets == 0 {
		t.Fatalf("restored facts = migrations:%d balances:%d journals:%d datasets:%d, %v",
			migrations, balances, journals, datasets, err)
	}
}
