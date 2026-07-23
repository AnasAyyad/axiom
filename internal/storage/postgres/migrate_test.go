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
	if len(migrations) < 2 {
		t.Fatal("B1 migrations are missing")
	}
	var b1 []Migration
	for _, migration := range migrations {
		if migration.Version == "000012" || migration.Version == "000013" {
			b1 = append(b1, migration)
		}
	}
	if len(b1) != 2 {
		t.Fatalf("B1 migration count = %d", len(b1))
	}
	lower := strings.ToLower(b1[0].SQL + "\n" + b1[1].SQL)
	for _, required := range []string{"'bybit'", "public_clock_samples", "public_connection_events",
		"public_clock_samples_immutable", "public_connection_events_immutable",
		"enforce_portfolio_ownership_strategy_reference", "shadow_sessions_public_exchange_alias",
		"exchange_id text references exchanges(id)"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B1 migration missing %q", required)
		}
	}
	if b1[0].Version != "000012" || b1[1].Version != "000013" {
		t.Fatalf("B1 migration versions = %s/%s", b1[0].Version, b1[1].Version)
	}
}

func TestB2MigrationDefinesCoherentViewsAndTierACompleteness(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b2 Migration
	for _, migration := range migrations {
		if migration.Version == "000014" {
			b2 = migration
			break
		}
	}
	lower := strings.ToLower(b2.SQL)
	for _, required := range []string{
		"create table cross_market_view_headers", "create table cross_market_view_members",
		"enforce_cross_market_view_complete", "cross_market_view_headers_immutable",
		"decision_market_scope", "cross_market_view_id", "create table dataset_exchange_coverage",
		"create table dataset_tier_a_members", "enforce_tier_a_dataset_manifest",
		"raw_canonical_linkage_complete", "hidden_gap_count",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B2 migration missing %q", required)
		}
	}
	if b2.Version != "000014" {
		t.Fatalf("B2 migration version = %s", b2.Version)
	}
}

func TestB3MigrationDefinesImmutableMeanReversionEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b3 Migration
	for _, migration := range migrations {
		if migration.Version == "000015" {
			b3 = migration
			break
		}
	}
	lower := strings.ToLower(b3.SQL)
	for _, required := range []string{
		"create table mean_reversion_decisions", "primary_candle_view_id", "higher_candle_view_id",
		"coherent_version_vector_hash", "portfolio_ownership_account_id", "risk_policy_id",
		"mean_reversion_risk_policy_mismatch", "mean_reversion_model_type_mismatch",
		"mean_reversion_ownership_strategy_mismatch", "mean_reversion_decisions_immutable",
		"security definer set search_path = pg_catalog, public",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B3 migration missing %q", required)
		}
	}
	if b3.Version != "000015" {
		t.Fatalf("B3 migration version = %s", b3.Version)
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
