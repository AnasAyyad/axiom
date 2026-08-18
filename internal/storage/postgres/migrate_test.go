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

func TestOwnerConsoleMigrationFailsClosedAndPreservesHistoricalEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000028")
	if migration.Name == "" {
		t.Fatal("owner console migration is missing")
	}
	assertMigrationContains(t, migration, "owner console", []string{
		"create table owner_accounts", "owner_console_multiple_active_users",
		"create view configuration_records", "create view activity_records",
		"create trigger users_single_active_owner",
	})
}

func TestSingleOwnerAuthorizationBoundaryMakesLegacyRecordsReadOnly(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000036")
	if migration.Name == "" {
		t.Fatal("single-owner authorization migration is missing")
	}
	assertMigrationContains(t, migration, "single owner authorization", []string{
		"legacy_authorization_records_are_historical",
		"authorization_permissions_historical", "authorization_roles_historical",
		"role_permissions_historical", "user_roles_historical",
		"from owner_accounts owner", "owner.user_id = p_actor_user_id",
	})
}

func TestSandboxStrategySessionMigrationPreservesArmableAccountTopology(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000029")
	if migration.Name == "" {
		t.Fatal("sandbox strategy-session migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox strategy session", []string{
		"create table sandbox_strategy_sessions",
		"create table sandbox_strategy_session_accounts",
		"cross-exchange-arbitrage",
		"sandbox_strategy_session_account_membership_invalid",
		"sandbox_strategy_session_topology_invalid",
		"sandbox_strategy_session_membership_immutable",
	})
}

func TestSandboxStrategySessionInstrumentMigrationPreservesHistoricalUnknowns(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000030")
	if migration.Name == "" {
		t.Fatal("sandbox strategy-session instrument migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox strategy session instrument", []string{
		"add column instrument text", "instrument is null or instrument in ('btcusdt','ethusdt')",
		"rather than receiving a guessed historical value",
	})
}

func TestInstrumentEligibilityObservationMigrationKeepsAdmissionInstrumentScoped(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000031")
	if migration.Name == "" {
		t.Fatal("instrument eligibility observation migration is missing")
	}
	assertMigrationContains(t, migration, "instrument eligibility observation", []string{
		"create table v1c_engine_market_observations",
		"primary key (account_id,instrument)",
		"instrument in ('btcusdt','ethusdt')",
		"eligibility->>'instrument'=instrument",
	})
}

func TestTriangularMarketObservationMigrationAddsOnlyTheApprovedThirdSpotMarket(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000044")
	if migration.Name == "" {
		t.Fatal("triangular market observation migration is missing")
	}
	assertMigrationContains(t, migration, "triangular market observation", []string{
		"drop constraint v1c_engine_market_observations_instrument_check",
		"instrument in ('btcusdt','ethusdt','ethbtc')",
	})
}

func TestShadowSessionSelectionMigrationPreservesUnknownHistoryAndGuardsNewRuns(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000047")
	if migration.Name == "" {
		t.Fatal("shadow session selection migration is missing")
	}
	assertMigrationContains(t, migration, "shadow session selection", []string{
		"add column instrument_id text references instruments(id)",
		"historical shadow sessions", "no honest single-instrument value to backfill",
		"shadow_session_instrument_exchange_mismatch",
		"exchange.environment = 'production_public'",
	})
}

func TestShadowStrategyDecisionEvidenceMigrationPreservesExactSemanticInputs(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000048")
	if migration.Name == "" {
		t.Fatal("shadow strategy decision evidence migration is missing")
	}
	assertMigrationContains(t, migration, "shadow strategy decision evidence", []string{
		"create table shadow_strategy_decision_evidence",
		"canonical_input bytea not null",
		"canonical_decision bytea not null",
		"mean_reversion_input",
		"shadow_strategy_decision_parent_mismatch",
		"shadow_strategy_decision_evidence_immutable",
	})
}

func TestShadowSessionMarketScopeMigrationGuardsExactMultiMarketTopology(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000049")
	if migration.Name == "" {
		t.Fatal("shadow session market scope migration is missing")
	}
	assertMigrationContains(t, migration, "shadow session market scope", []string{
		"add column market_scope_required boolean not null default false",
		"create table shadow_session_market_scopes",
		"shadow_market_scope_reference_invalid",
		"shadow_single_market_scope_invalid",
		"shadow_triangle_market_scope_invalid",
		"shadow_paired_market_scope_invalid",
		"deferrable initially deferred",
		"shadow_session_market_scopes_immutable",
	})
}

func TestShadowSessionActivityMigrationKeepsWaitingAndInputHealthImmutable(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000050")
	if migration.Name == "" {
		t.Fatal("shadow session activity migration is missing")
	}
	assertMigrationContains(t, migration, "shadow session activity", []string{
		"create table shadow_session_activity_observations",
		"create table shadow_session_input_health_observations",
		"'preparing','warming_up','waiting','evaluating','running','paused','blocked','stopped'",
		"shadow_input_health_outside_market_scope",
		"shadow_activity_input_health_incomplete",
		"deferrable initially deferred",
		"shadow_session_activity_observations_immutable",
		"shadow_session_input_health_observations_immutable",
	})
}

func TestPublicInstrumentMaximumQuantityMigrationPreservesHistoricalUnknowns(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000051")
	if migration.Name == "" {
		t.Fatal("public instrument maximum-quantity migration is missing")
	}
	assertMigrationContains(t, migration, "public instrument maximum quantity", []string{
		"add column maximum_quantity financial_amount",
		"maximum_quantity is null", "historical rows", "rather than receiving a guessed value",
	})
}

func TestShadowMultilegExecutionEvidenceMigrationIsAcceptedOnlyAndImmutable(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000052")
	if migration.Name == "" {
		t.Fatal("shadow multi-leg execution-evidence migration is missing")
	}
	assertMigrationContains(t, migration, "shadow multi-leg execution evidence", []string{
		"create table shadow_multileg_execution_evidence",
		"canonical_execution_plan bytea not null",
		"canonical_simulation bytea not null",
		"canonical_reduction bytea not null",
		"canonical_projected_balances bytea not null",
		"parent_outcome is distinct from 'accepted'",
		"shadow_multileg_execution_parent_mismatch",
		"shadow_multileg_execution_evidence_immutable",
	})
}

func TestShadowCrossExchangeInventoryMigrationKeepsVenueOwnershipImmutable(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000053")
	if migration.Name == "" {
		t.Fatal("paired shadow inventory migration is missing")
	}
	assertMigrationContains(t, migration, "shadow cross-exchange inventory", []string{
		"create table shadow_cross_exchange_inventory_initializations",
		"account_id text not null unique references virtual_accounts",
		"cross-exchange-single-instrument-prefund.v1",
		"retain_unselected_volatile_allocation_as_usdt",
		"shadow_cross_exchange_inventory_reference_mismatch",
		"shadow_cross_exchange_inventory_initializations_immutable",
	})
}

func TestStrategyPlanSnapshotMigrationKeepsInventoryProofImmutable(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000032")
	if migration.Name == "" {
		t.Fatal("strategy plan snapshot migration is missing")
	}
	assertMigrationContains(t, migration, "strategy plan account snapshot", []string{
		"create table v1c_plan_account_snapshots",
		"foreign key (account_id,account_epoch,snapshot_hash)",
		"references v1c_account_snapshots(account_id,account_epoch,snapshot_hash)",
		"v1c_plan_account_snapshots_immutable",
	})
}

func TestStrategySessionEvaluationMigrationPreservesSanitizedImmutableTimeline(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000033")
	if migration.Name == "" {
		t.Fatal("strategy session evaluation migration is missing")
	}
	assertMigrationContains(t, migration, "strategy session evaluation", []string{
		"create table sandbox_strategy_session_evaluations",
		"state in ('waiting','evaluated','blocked')",
		"reason ~ '^[a-z0-9_]{1,96}$'",
		"sandbox_strategy_session_evaluations_immutable",
	})
}

func TestStrategyPlanDecisionMigrationPreservesExactImmutableProvenance(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000034")
	if migration.Name == "" {
		t.Fatal("strategy plan decision migration is missing")
	}
	assertMigrationContains(t, migration, "strategy plan decision", []string{
		"create table v1c_strategy_plan_decisions",
		"canonical_input bytea not null", "canonical_decision bytea not null",
		"strategy in ('trend','mean-reversion')",
		"v1c_strategy_plan_decisions_immutable",
	})
}

func TestStrategyDecisionJournalMigrationPreservesNoOrderStateTransitions(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000035")
	if migration.Name == "" {
		t.Fatal("strategy decision journal migration is missing")
	}
	assertMigrationContains(t, migration, "strategy decision journal", []string{
		"create table sandbox_strategy_decisions",
		"plan_id text references v1c_submission_plans(id)",
		"unique (strategy_session_id,account_id,account_epoch,event_ordinal)",
		"sandbox_strategy_decisions_immutable",
	})
}

func TestMultiLegStrategyDecisionMigrationExpandsBothImmutableDecisionStores(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000045")
	if migration.Name == "" {
		t.Fatal("multi-leg strategy decision migration is missing")
	}
	assertMigrationContains(t, migration, "multi-leg strategy decisions", []string{
		"alter table v1c_strategy_plan_decisions",
		"drop constraint v1c_strategy_plan_decisions_strategy_check",
		"alter table sandbox_strategy_decisions",
		"drop constraint sandbox_strategy_decisions_strategy_check",
		"'triangular','cross-exchange-arbitrage'",
	})
}

func TestTriangularAccountingMigrationRetainsEverySpotLegAndFailsRiskClosed(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000046")
	if migration.Name == "" {
		t.Fatal("triangular accounting projection migration is missing")
	}
	assertMigrationContains(t, migration, "triangular accounting projection", []string{
		"alter table sandbox_accounting_transactions",
		"instrument in ('btcusdt','ethusdt','ethbtc')",
		"sandbox_accounting_transaction_session_identity",
		"sandbox_accounting_positions_last_transaction_session_fkey",
		"'cross_asset_open','inventory_unresolved'",
	})
}

func TestStrategyRiskObservationMigrationBindsCompleteImmutableInputs(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000037")
	if migration.Name == "" {
		t.Fatal("strategy risk observation migration is missing")
	}
	assertMigrationContains(t, migration, "strategy risk observation", []string{
		"create table sandbox_strategy_risk_observations",
		"foreign key (account_id,account_epoch,snapshot_hash)",
		"foreign key (policy_id,policy_version)",
		"account_drawdown financial_amount not null",
		"book_age_nanoseconds bigint not null",
		"lease_lost boolean not null",
		"scope_kind='global' and scope_id='platform'",
		"sandbox_strategy_risk_policy_mismatch",
		"sandbox_strategy_risk_state_not_normal",
		"sandbox_strategy_risk_snapshot_stale",
		"sandbox_strategy_risk_observations_immutable",
	})
}

func TestSandboxAccountingJournalMigrationBindsAndBalancesEveryStrategyFill(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000038")
	if migration.Name == "" {
		t.Fatal("sandbox accounting journal migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox accounting journal", []string{
		"create table sandbox_accounting_transactions",
		"create table sandbox_accounting_entries",
		"transaction_type text not null",
		"source_mode text not null",
		"configuration_id text not null references configuration_versions(id)",
		"policy_hash sha256_hex not null",
		"client_order_id text not null",
		"intent_action text not null",
		"foreign key (strategy_session_id,account_id)",
		"foreign key (account_id,account_epoch,native_fill_id_hash)",
		"unique (account_id,account_epoch,fill_id)",
		"unbalanced_sandbox_accounting_transaction",
		"sandbox_accounting_transactions_immutable",
		"sandbox_accounting_entries_immutable",
		"sandbox_accounting_balanced_on_commit",
	})
}

func TestSandboxStrategySessionMembershipGuardUsesSemanticParentColumn(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000039")
	if migration.Name == "" {
		t.Fatal("sandbox strategy-session membership repair is missing")
	}
	assertMigrationContains(t, migration, "sandbox strategy session membership guard", []string{
		"create or replace function enforce_sandbox_strategy_session_account",
		"select sandbox_session_id into parent_session",
		"membership.session_id = parent_session",
		"sandbox_strategy_session_account_membership_invalid",
		"sandbox_strategy_session_account_exchange_invalid",
	})
}

func TestSandboxStrategySessionLifecycleMigrationPreservesStartHistory(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000040")
	if migration.Name == "" {
		t.Fatal("sandbox strategy-session lifecycle repair is missing")
	}
	assertMigrationContains(t, migration, "sandbox strategy session lifecycle", []string{
		"drop constraint sandbox_strategy_sessions_check",
		"drop constraint sandbox_strategy_sessions_check1",
		"drop constraint sandbox_strategy_sessions_check2",
		"sandbox_strategy_sessions_lifecycle_valid",
		"state = 'blocked'",
		"started_at is not null",
		"blocking_reason is not null",
		"state = 'stopped'",
		"stopped_at is not null",
		"sandbox_strategy_sessions_lifecycle_chronology_valid",
		"stopped_at >= coalesce(started_at, created_at)",
	})
}

func TestSandboxAccountingProjectionMigrationIsJournalRebuildable(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000041")
	if migration.Name == "" {
		t.Fatal("sandbox accounting projection migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox accounting projection", []string{
		"sandbox_accounting_transaction_projection_identity",
		"create table sandbox_accounting_positions",
		"weighted_average_cost financial_amount not null",
		"realized_pnl signed_financial_amount not null",
		"valuation_state text not null",
		"source_transaction_count bigint not null",
		"projection_hash sha256_hex not null",
		"create table sandbox_accounting_position_fees",
		"fee_quantity financial_amount not null",
		"rebate_quantity financial_amount not null",
	})
}

func TestSandboxRiskValuationMigrationBindsExactProjectionAndOperationalEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000042")
	if migration.Name == "" {
		t.Fatal("sandbox strategy risk valuation migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox strategy risk valuation", []string{
		"sandbox_accounting_position_risk_identity",
		"create table sandbox_strategy_risk_valuations",
		"purpose text not null",
		"accounting_projection_hash sha256_hex",
		"account_equity financial_amount not null",
		"strategy_total_pnl signed_financial_amount not null",
		"account_peak_equity financial_amount not null",
		"utc_day_baseline_equity financial_amount not null",
		"rolling_24_hour_baseline_equity financial_amount not null",
		"risk_observation_id text unique references sandbox_strategy_risk_observations(id)",
		"sandbox_strategy_risk_accounting_projection_missing",
		"sandbox_strategy_risk_reconciliation_stale",
		"sandbox_strategy_risk_storage_stale",
		"sandbox_strategy_risk_valuations_immutable",
	})
}

func TestSandboxMultilegMigrationKeepsDependentReservationsInert(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	migration := migrationForVersion(migrations, "000043")
	if migration.Name == "" {
		t.Fatal("sandbox multileg dispatch migration is missing")
	}
	assertMigrationContains(t, migration, "sandbox multileg dispatch", []string{
		"leg_count between 1 and 3",
		"execution_expires_at <= approved_at + interval '250 milliseconds'",
		"primary key (plan_id,exchange,instrument)",
		"depends_on_leg_index integer",
		"state in ('waiting','pending','claimed','acknowledged','unknown','terminal')",
		"state in ('waiting','active','consumed','released','quarantined')",
		"old.state='waiting' and new.state in",
		"'active','released','quarantined'",
	})
}

func TestExchangeExpansionMigrationSeedsBybitAndImmutablePublicEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 2 {
		t.Fatal("exchange expansion migrations are missing")
	}
	var exchange_expansion []Migration
	for _, migration := range migrations {
		if migration.Version == "000012" || migration.Version == "000013" {
			exchange_expansion = append(exchange_expansion, migration)
		}
	}
	if len(exchange_expansion) != 2 {
		t.Fatalf("exchange expansion migration count = %d", len(exchange_expansion))
	}
	lower := strings.ToLower(exchange_expansion[0].SQL + "\n" + exchange_expansion[1].SQL)
	for _, required := range []string{"'bybit'", "public_clock_samples", "public_connection_events",
		"public_clock_samples_immutable", "public_connection_events_immutable",
		"enforce_portfolio_ownership_strategy_reference", "shadow_sessions_public_exchange_alias",
		"exchange_id text references exchanges(id)"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("exchange expansion migration missing %q", required)
		}
	}
	if exchange_expansion[0].Version != "000012" || exchange_expansion[1].Version != "000013" {
		t.Fatalf("exchange expansion migration versions = %s/%s", exchange_expansion[0].Version, exchange_expansion[1].Version)
	}
}

func TestCoherentMarketDataMigrationDefinesCoherentViewsAndTierACompleteness(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var coherent_market_data Migration
	for _, migration := range migrations {
		if migration.Version == "000014" {
			coherent_market_data = migration
			break
		}
	}
	lower := strings.ToLower(coherent_market_data.SQL)
	for _, required := range []string{
		"create table cross_market_view_headers", "create table cross_market_view_members",
		"enforce_cross_market_view_complete", "cross_market_view_headers_immutable",
		"decision_market_scope", "cross_market_view_id", "create table dataset_exchange_coverage",
		"create table dataset_tier_a_members", "enforce_tier_a_dataset_manifest",
		"raw_canonical_linkage_complete", "hidden_gap_count",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("coherent market data migration missing %q", required)
		}
	}
	if coherent_market_data.Version != "000014" {
		t.Fatalf("coherent market data migration version = %s", coherent_market_data.Version)
	}
}

func TestMeanReversionMigrationDefinesImmutableMeanReversionEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var mean_reversion Migration
	for _, migration := range migrations {
		if migration.Version == "000015" {
			mean_reversion = migration
			break
		}
	}
	lower := strings.ToLower(mean_reversion.SQL)
	for _, required := range []string{
		"create table mean_reversion_decisions", "primary_candle_view_id", "higher_candle_view_id",
		"coherent_version_vector_hash", "portfolio_ownership_account_id", "risk_policy_id",
		"mean_reversion_risk_policy_mismatch", "mean_reversion_model_type_mismatch",
		"mean_reversion_ownership_strategy_mismatch", "mean_reversion_decisions_immutable",
		"security definer set search_path = pg_catalog, public",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("mean reversion migration missing %q", required)
		}
	}
	if mean_reversion.Version != "000015" {
		t.Fatalf("mean reversion migration version = %s", mean_reversion.Version)
	}
}

func TestTriangularArbitrageMigrationDefinesAtomicClaimsSequentialEvidenceAndBalancedLinks(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var triangular_arbitrage Migration
	for _, migration := range migrations {
		if migration.Version == "000016" {
			triangular_arbitrage = migration
			break
		}
	}
	lower := strings.ToLower(triangular_arbitrage.SQL)
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
			t.Fatalf("triangular arbitrage migration missing %q", required)
		}
	}
	if triangular_arbitrage.Version != "000016" {
		t.Fatalf("triangular arbitrage migration version = %s", triangular_arbitrage.Version)
	}
}

func TestCrossExchangeArbitrageMigrationDefinesCoherentConcurrentClosedCycleEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var cross_exchange_arbitrage Migration
	for _, migration := range migrations {
		if migration.Version == "000017" {
			cross_exchange_arbitrage = migration
			break
		}
	}
	lower := strings.ToLower(cross_exchange_arbitrage.SQL)
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
			t.Fatalf("cross-exchange arbitrage migration missing %q", required)
		}
	}
	if cross_exchange_arbitrage.Version != "000017" {
		t.Fatalf("cross-exchange arbitrage migration version = %s", cross_exchange_arbitrage.Version)
	}
}

func TestInventoryRebalancingMigrationDefinesImmutableReviewedAdvisoryEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var inventory_rebalancing Migration
	for _, migration := range migrations {
		if migration.Version == "000018" {
			inventory_rebalancing = migration
			break
		}
	}
	lower := strings.ToLower(inventory_rebalancing.SQL)
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
			t.Fatalf("inventory rebalancing migration missing %q", required)
		}
	}
	if inventory_rebalancing.Version != "000018" {
		t.Fatalf("inventory rebalancing migration version = %s", inventory_rebalancing.Version)
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

func TestMultiExchangeConsoleMigrationDefinesImmutableSimulationOnlyConsoleEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var multi_exchange_console Migration
	for _, migration := range migrations {
		if migration.Version == "000020" {
			multi_exchange_console = migration
			break
		}
	}
	lower := strings.ToLower(multi_exchange_console.SQL)
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
			t.Fatalf("multi-exchange console migration missing %q", required)
		}
	}
}

func TestSandboxRuntimeMigrationsDefineClosedDurableAuthenticatedEvidence(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	sandboxRuntimeAuth := migrationForVersion(migrations, "000021")
	sandboxRuntimeExecution := migrationForVersion(migrations, "000022")
	sandboxRuntimeBinanceStream := migrationForVersion(migrations, "000023")
	sandboxQualification := migrationForVersion(migrations, "000024")
	sandboxQualificationRecovery := migrationForVersion(migrations, "000056")
	sandboxQualificationStreamRecovery := migrationForVersion(migrations, "000057")
	if sandboxRuntimeAuth.SQL == "" || sandboxRuntimeExecution.SQL == "" ||
		sandboxRuntimeBinanceStream.SQL == "" || sandboxQualification.SQL == "" ||
		sandboxQualificationRecovery.SQL == "" ||
		sandboxQualificationStreamRecovery.SQL == "" {
		t.Fatal("sandbox runtime migrations are missing")
	}
	assertSandboxRuntimeBinanceStreamMigration(t, sandboxRuntimeBinanceStream)
	assertSandboxRuntimeAuthMigration(t, sandboxRuntimeAuth)
	assertSandboxRuntimeExecutionMigration(t, sandboxRuntimeExecution)
	assertSandboxQualificationMigration(t, sandboxQualification)
	assertSandboxQualificationRecoveryMigration(t, sandboxQualificationRecovery)
	assertSandboxQualificationPrivateStreamRecoveryMigration(
		t, sandboxQualificationStreamRecovery,
	)
}

func assertSandboxQualificationPrivateStreamRecoveryMigration(
	t *testing.T,
	migration Migration,
) {
	t.Helper()
	assertMigrationContains(
		t, migration, "sandbox qualification private-stream recovery", []string{
			"'private_stream'",
			"incident_source text not null",
			"incident_source in ('reconciliation','private_stream')",
			"'recovery_expired','recovery_repeated'",
		},
	)
}

func assertSandboxQualificationRecoveryMigration(
	t *testing.T,
	migration Migration,
) {
	t.Helper()
	assertMigrationContains(
		t, migration, "sandbox qualification bounded recovery", []string{
			"failure_kind text",
			"cause_code text",
			"account_observations jsonb",
			"create table sandbox_qualification_recovery_events",
			"sandbox_qualification_recovery_events_immutable",
		},
	)
}

func TestOwnerControlMigrationDefinesFailClosedControlPlane(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	owner_control := migrationForVersion(migrations, "000025")
	if owner_control.SQL == "" {
		t.Fatal("owner-console release owner control migration is missing")
	}
	assertMigrationContains(t, owner_control, "historical owner-control", []string{
		"create table v1d_reason_catalogue",
		"create table v1d_activity_projection",
		"create view v1d_activity_explanations",
		"security definer set search_path = pg_catalog, public",
		"revoke all on function project_v1d_activity(text,jsonb) from public",
		"create table v1d_strategy_controls",
		"create table v1d_risk_controls",
		"create table v1d_export_artifacts",
		"expires_at = created_at + interval '7 days'",
		"create table v1d_qualification_runs",
		"protect_v1d_qualification_run",
		"v1d_authorization_target_revision_required",
	})
}

func assertSandboxRuntimeBinanceStreamMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "historical sandbox runtime Binance stream", []string{
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

func assertSandboxRuntimeAuthMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "historical sandbox runtime auth", []string{
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

func assertSandboxRuntimeExecutionMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "historical sandbox runtime execution", []string{
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

func assertSandboxQualificationMigration(t *testing.T, migration Migration) {
	t.Helper()
	assertMigrationContains(t, migration, "historical sandbox qualification", []string{
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

func TestSemanticRuntimeNamesMigrationPreservesHistoricalRows(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	semanticNames := migrationForVersion(migrations, "000054")
	if semanticNames.SQL == "" {
		t.Fatal("semantic runtime names migration is missing")
	}
	assertMigrationContains(t, semanticNames, "semantic runtime names", []string{
		"it never rewrites evidence payloads, hashes, or journal data",
		"sandbox_runtime_",
		"sandbox_qualification_",
		"owner_console_",
		"triangular_arbitrage_",
		"cross_exchange_arbitrage_",
		"research_promotion_",
		"multi_exchange_console_",
		"semantic_identifier_name",
		"semantic_schema_name_conflict",
		"semantic_role_name_conflict",
		"semantic_role_rename_requires_database_administrator",
	})
}

func TestStrategyEvaluationMigrationIsFailClosedAndEvidencePreserving(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	evaluationMigration := migrationForVersion(migrations, "000055")
	if evaluationMigration.SQL == "" {
		t.Fatal("strategy evaluation migration is missing")
	}
	assertMigrationContains(t, evaluationMigration, "strategy evaluation", []string{
		"create table evaluation_campaigns",
		"preset='balanced_full_v1'",
		"campaign_recorded_bytes <= 214748364800",
		"create unique index evaluation_campaigns_one_active",
		"create table evaluation_campaign_stages",
		"create table evaluation_campaign_members",
		"create table evaluation_campaign_stress_results",
		"'delayed_data','data_gap','restart_recovery','rejection','partial_fill','cancel_fill_race','unknown_result','persistence_failure'",
		"create table evaluation_historical_imports",
		"create table evaluation_historical_import_segments",
		"'finalizing'",
		"create table evaluation_shadow_sessions",
		"protected_reserve_micros bigint not null default 2000000000",
		"member_ceiling_micros bigint not null default 2000000000",
		"create table evaluation_shadow_member_checkpoints",
		"create table evaluation_shadow_decisions",
		"evaluation_shadow_decisions_immutable",
		"window_start='2023-08-01 00:00:00+00'",
		"window_end='2026-08-01 00:00:00+00'",
		"evaluation_campaign_events_immutable",
		"evaluation_campaign_reports_immutable",
		"evaluation_campaign_stress_results_immutable",
		"evaluation_data_audit_findings_immutable",
		"evaluation_historical_import_segments_immutable",
		"revoke all on function protect_evaluation_immutable_evidence() from public",
	})
	if strings.Contains(strings.ToLower(evaluationMigration.SQL), " to axiom_") {
		t.Fatal("strategy evaluation migration bypasses the deployment role reconciler")
	}
}

func TestShadowGracefulRestartMigrationKeepsHandoffFenced(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	restartMigration := migrationForVersion(migrations, "000059")
	if restartMigration.SQL == "" {
		t.Fatal("shadow graceful-restart migration is missing")
	}
	assertMigrationContains(t, restartMigration, "shadow graceful restart", []string{
		"old.state in ('paused','running') and new.state='queued'",
		"not new.entries_enabled",
		"new.claim_owner=old.claim_owner",
		"new.claim_epoch=old.claim_epoch",
		"new.claim_expires_at=old.claim_expires_at",
		"exists (select 1 from run_checkpoints",
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

func TestMigrationsContainDurableStorageHistoryAndOwnershipGuards(t *testing.T) {
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
