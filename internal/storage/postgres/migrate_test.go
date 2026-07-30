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

func TestB4MigrationDefinesAtomicClaimsSequentialEvidenceAndBalancedLinks(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b4 Migration
	for _, migration := range migrations {
		if migration.Version == "000016" {
			b4 = migration
			break
		}
	}
	lower := strings.ToLower(b4.SQL)
	for _, required := range []string{
		"create table triangular_candidates", "create table triangular_candidate_legs",
		"create table b4_claim_resources", "create table b4_claim_groups",
		"create table b4_claim_items", "claim_b4_resources",
		"settle_b4_claim_group", "close_b4_claim_group",
		"security definer set search_path = pg_catalog, public",
		"triangular_candidate_output_chain_mismatch", "triangular_candidate_model_type_mismatch",
		"create table triangular_simulation_outcomes",
		"create table triangular_opportunity_lifetimes",
		"create table triangular_journal_links", "triangular_candidates_immutable",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B4 migration missing %q", required)
		}
	}
	if b4.Version != "000016" {
		t.Fatalf("B4 migration version = %s", b4.Version)
	}
}

func TestB5MigrationDefinesCoherentConcurrentClosedCycleEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b5 Migration
	for _, migration := range migrations {
		if migration.Version == "000017" {
			b5 = migration
			break
		}
	}
	lower := strings.ToLower(b5.SQL)
	for _, required := range []string{
		"create table cross_exchange_candidates",
		"create table cross_exchange_candidate_members",
		"cross_exchange_candidate_member_evidence_mismatch",
		"create table cross_exchange_candidate_legs",
		"create table cross_exchange_inventory_snapshots",
		"marginal_inventory_replacement",
		"usdt_venue_concentration_penalty",
		"expected_closed_cycle_profit",
		"create table b5_claim_resources",
		"claim_b5_resources", "settle_b5_claim_group", "close_b5_claim_group",
		"create table cross_exchange_simulation_outcomes",
		"create table cross_exchange_simulation_legs",
		"delayed_unknown", "create table cross_exchange_rebalancing_needs",
		"advisory_only boolean not null check (advisory_only)",
		"create table cross_exchange_journal_links",
		"security definer set search_path = pg_catalog, public",
		"cross_exchange_candidates_immutable",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B5 migration missing %q", required)
		}
	}
	if b5.Version != "000017" {
		t.Fatalf("B5 migration version = %s", b5.Version)
	}
}

func TestB6MigrationDefinesImmutableReviewedAdvisoryEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b6 Migration
	for _, migration := range migrations {
		if migration.Version == "000018" {
			b6 = migration
			break
		}
	}
	lower := strings.ToLower(b6.SQL)
	for _, required := range []string{
		"create table rebalancing_fact_sets",
		"create table rebalancing_route_facts",
		"fact_schema_version",
		"cost_model_version",
		"provenance_hash",
		"confidence financial_amount",
		"create table rebalancing_recommendations",
		"natural_reverse_arbitrage",
		"reviewed_graph_route",
		"advisory_only boolean not null check (advisory_only)",
		"create table rebalancing_recommendation_steps",
		"create table rebalancing_checklist_steps",
		"rebalancing_selected_fact_ineligible",
		"rebalancing_recommendation_evidence_mismatch",
		"rebalancing_natural_reverse_mismatch",
		"rebalancing_graph_route_mismatch",
		"security definer set search_path = pg_catalog, public",
		"rebalancing_recommendations_immutable",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B6 migration missing %q", required)
		}
	}
	if b6.Version != "000018" {
		t.Fatalf("B6 migration version = %s", b6.Version)
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

func TestB8MigrationDefinesImmutableSimulationOnlyConsoleEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var b8 Migration
	for _, migration := range migrations {
		if migration.Version == "000020" {
			b8 = migration
			break
		}
	}
	lower := strings.ToLower(b8.SQL)
	for _, required := range []string{
		"create table b8_replay_fault_schedule_states",
		"create table b8_replay_fault_schedules",
		"create table b8_report_exports",
		"simulation_only boolean not null check (simulation_only)",
		"b8_fault_schedules_reference_guard",
		"b8_fault_schedules_immutable",
		"b8_report_exports_reference_guard",
		"b8_report_exports_immutable",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("B8 migration missing %q", required)
		}
	}
}

func TestV1CMigrationsDefineClosedDurableAuthenticatedEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	v1cAuth := migrationForVersion(migrations, "000021")
	v1cExecution := migrationForVersion(migrations, "000022")
	v1cBinanceStream := migrationForVersion(migrations, "000023")
	v1cC6 := migrationForVersion(migrations, "000024")
	if v1cAuth.SQL == "" || v1cExecution.SQL == "" ||
		v1cBinanceStream.SQL == "" || v1cC6.SQL == "" {
		t.Fatal("V1C migrations are missing")
	}
	assertV1CBinanceStreamMigration(t, v1cBinanceStream)
	assertV1CAuthMigration(t, v1cAuth)
	assertV1CExecutionMigration(t, v1cExecution)
	assertV1CC6Migration(t, v1cC6)
}

func assertV1CBinanceStreamMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "V1C Binance stream", []string{
		"ws-api.testnet.binance.vision",
		"/ws-api/v3/userdatastream.subscribe.signature",
		"stream-demo.bybit.com",
		"/v5/private/auth",
		"create table v1c_engine_startup_evidence",
		"v1c_engine_startup_evidence_immutable",
		"create table v1c_engine_commands",
		"v1c_engine_commands_claim_idx",
		"create table v1c_engine_observations",
		"create table v1c_canary_evidence",
		"v1c_canary_evidence_immutable",
		"v1c_authenticated_request_route_closed",
	})
}

func assertV1CAuthMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "V1C auth", []string{
		"create table v1c_totp_replay_state",
		"create table v1c_sandbox_authorizations",
		"create table v1c_high_risk_audit_events",
		"create table v1c_session_control_events",
		"session_revision bigint not null",
		"v1c_authorization_session_active",
		"v1c_revoke_all_authorized",
		"v1c_high_risk_audit_immutable",
	})
}

func assertV1CExecutionMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "V1C execution", []string{
		"create table v1c_authenticated_request_evidence",
		"create table v1c_plan_entry_safety",
		"v1c_authenticated_evidence_fields_valid",
		"v1c_authenticated_evidence_enumerations_valid",
		"v1c_authenticated_request_evidence_immutable",
		"v1c_plan_entry_safety_immutable",
		"v1c_credential_rotation_protected",
		"primary key (exchange,request_hash)",
		"host='testnet.binance.vision'",
		"host='api-demo.bybit.com'",
	})
}

func assertV1CC6Migration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "V1C C6 qualification", []string{
		"create table v1c_engine_runtime_events",
		"create view v1c_c6_order_observations",
		"create table v1c_c6_qualification_runs",
		"required_duration_seconds=259200",
		"profitability_evidence boolean not null check (not profitability_evidence)",
		"create table v1c_c6_qualification_accounts",
		"create table v1c_c6_qualification_samples",
		"create table v1c_c6_qualification_failures",
		"create table v1c_c6_chaos_events",
		"protect_v1c_c6_qualification_run",
		"v1c_engine_runtime_events_immutable",
		"v1c_c6_chaos_events_immutable",
	})
}

func migrationForVersion(migrations []Migration, version string) Migration {
	for _, migration := range migrations {
		if migration.Version == version {
			return migration
		}
	}
	return Migration{}
}

func assertMigrationContains(
	t *testing.T,
	migration Migration,
	label string,
	requiredValues []string,
) {
	t.Helper()
	source := strings.ToLower(migration.SQL)
	for _, required := range requiredValues {
		if !strings.Contains(source, required) {
			t.Fatalf("%s migration missing %q", label, required)
		}
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
