package postgres

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreOrderedForwardOnlyAndChecksummed(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 3 {
		t.Fatalf("migration count = %d", len(migrations))
	}
	prior := ""
	for _, migration := range migrations {
		if migration.Version <= prior || len(migration.Checksum) != 64 || migration.SQL == "" {
			t.Fatalf("invalid migration = %#v", migration)
		}
		lower := strings.ToLower(migration.SQL)
		if strings.Contains(lower, "double precision") || strings.Contains(lower, " real ") ||
			strings.Contains(lower, "drop database") {
			t.Fatalf("unsafe migration construct in %s", migration.Name)
		}
		prior = migration.Version
	}
}

func TestB1MigrationSeedsBybitAndImmutablePublicEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	latest := migrations[len(migrations)-1]
	lower := strings.ToLower(latest.SQL)
	for _, required := range []string{"'bybit'", "public_clock_samples", "public_connection_events",
		"public_clock_samples_immutable", "public_connection_events_immutable"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B1 migration missing %q", required)
		}
	}
	if latest.Version != "000012" {
		t.Fatalf("latest migration = %s", latest.Version)
	}
}

func TestMigrationVersionRejectsNonCanonicalNames(t *testing.T) {
	for _, name := range []string{"1_bad.sql", "000001.sql", "00000x_bad.sql", "000001_.sql"} {
		if _, ok := migrationVersion(name); ok {
			t.Fatalf("accepted migration name %q", name)
		}
	}
	if version, ok := migrationVersion("000001_core.sql"); !ok || version != "000001" {
		t.Fatalf("canonical version = %q, %t", version, ok)
	}
}

func TestMigrationsContainA4HistoryAndOwnershipGuards(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, migration := range migrations {
		source.WriteString(strings.ToLower(migration.SQL))
	}
	for _, required := range []string{
		"create table dataset_gaps",
		"protect_strategy_version",
		"protect_model_version",
		"enforce_asset_screening_sequence",
		"protect_market_data_segment",
		"protect_dataset_manifest",
		"immutable_order_identity",
		"immutable_reservation_identity",
		"invalid_run_transition",
		"enforce_job_transition",
		"protect_command_request",
		"protect_outbox_event",
		"enforce_consumer_cursor",
		"enforce_dataset_gap_nonoverlap",
		"enforce_journal_reversal",
		"reject_sealed_journal_line",
		"update journal_transactions set sealed = true",
		"security definer set search_path = pg_catalog, public",
		"journal_single_reversal_idx",
		"unique (exchange_id, order_id, exchange_fill_id)",
	} {
		if !strings.Contains(source.String(), required) {
			t.Fatalf("required migration guard missing: %s", required)
		}
	}
	for _, forbidden := range []string{
		"quantity signed_financial_amount not null,\n  weighted_average_cost",
		"unique (exchange_id, exchange_fill_id)",
	} {
		if strings.Contains(source.String(), forbidden) {
			t.Fatalf("unsafe migration shape present: %s", forbidden)
		}
	}
}
