package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationLockID int64 = 8_946_612_403_104_001

// Migration is one immutable forward-only schema change.
type Migration struct {
	Version  string
	Name     string
	SQL      string
	Checksum string
}

// Migrations returns the embedded reviewed changes in strict filename order.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migration_catalog_unavailable")
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	result := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, ok := migrationVersion(entry.Name())
		if !ok || (len(result) > 0 && result[len(result)-1].Version >= version) {
			return nil, fmt.Errorf("migration_order_invalid")
		}
		contents, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil || len(contents) == 0 || strings.Contains(strings.ToLower(string(contents)), "drop database") {
			return nil, fmt.Errorf("migration_content_invalid")
		}
		digest := sha256.Sum256(contents)
		result = append(result, Migration{
			Version: version, Name: entry.Name(), SQL: string(contents), Checksum: hex.EncodeToString(digest[:]),
		})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("migration_catalog_empty")
	}
	return result, nil
}

// ApplyMigrations serializes and transactionally applies every pending change.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	if pool == nil {
		return 0, fmt.Errorf("migration_pool_missing")
	}
	migrations, err := Migrations()
	if err != nil {
		return 0, err
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("migration_connection_unavailable")
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return 0, fmt.Errorf("migration_lock_unavailable")
	}
	defer func() { _, _ = connection.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID) }()
	if err = ensureMigrationTable(ctx, connection); err != nil {
		return 0, err
	}
	applied := 0
	for _, migration := range migrations {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil {
			return applied, applyErr
		}
		if changed {
			applied++
		}
	}
	return applied, nil
}

func ensureMigrationTable(ctx context.Context, connection *pgxpool.Conn) error {
	_, err := connection.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version text PRIMARY KEY, name text NOT NULL UNIQUE, checksum text NOT NULL CHECK (length(checksum) = 64),
applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`)
	if err != nil {
		return fmt.Errorf("migration_table_unavailable")
	}
	return nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, migration Migration) (bool, error) {
	var checksum string
	err := connection.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", migration.Version).Scan(&checksum)
	if err == nil {
		if checksum != migration.Checksum {
			return false, fmt.Errorf("migration_checksum_mismatch")
		}
		return false, nil
	}
	if err != pgx.ErrNoRows {
		return false, fmt.Errorf("migration_state_unavailable")
	}
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("migration_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, migration.SQL); err != nil {
		return false, fmt.Errorf("migration_apply_failed: %s", migration.Name)
	}
	if _, err = tx.Exec(ctx,
		"INSERT INTO schema_migrations(version, name, checksum) VALUES ($1, $2, $3)",
		migration.Version, migration.Name, migration.Checksum); err != nil {
		return false, fmt.Errorf("migration_record_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("migration_commit_failed")
	}
	return true, nil
}

func migrationVersion(name string) (string, bool) {
	separator := strings.IndexByte(name, '_')
	if separator != 6 || len(name) <= separator+5 {
		return "", false
	}
	for _, character := range name[:separator] {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return name[:separator], true
}
