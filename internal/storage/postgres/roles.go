package postgres

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

var runtimeReadInsertTables = []string{
	"account_snapshots", "alert_acknowledgements", "alert_deliveries", "alerts", "asset_screening_versions", "assets", "audit_events",
	"api_entity_revisions", "authentication_failures", "authorization_permissions",
	"v1c_high_risk_audit_events", "v1c_sandbox_authorizations", "v1c_session_control_events",
	"v1c_totp_replay_state", "v1c_sandbox_sessions", "v1c_sandbox_session_accounts", "v1c_sandbox_arms",
	"v1c_submission_plans", "v1c_plan_eligibility", "v1c_plan_entry_safety",
	"v1c_sandbox_reservations", "v1c_submission_outbox",
	"v1c_daily_cap_counters", "v1c_engine_commands", "v1c_canary_evidence",
	"v1d_export_artifacts", "v1d_artifact_holds", "v1d_artifact_access_events",
	"v1d_qualification_runs", "v1d_role_change_events",
	"v1d_incident_events", "v1d_incident_replay_inputs", "v1d_incident_alert_links",
	"v1d_incident_activity_links", "v1d_incident_resolution_evidence",
	"v1d_report_schedules", "v1d_reports", "v1d_alert_delivery_attempts",
	"v1d_alert_escalations", "v1d_alert_route_tests",
	"allocation_candidates", "allocation_reservations", "allocation_score_components",
	"authorization_roles", "command_requests", "configuration_activations", "configuration_versions", "consumer_cursors",
	"data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions",
	"cross_market_view_headers", "cross_market_view_members", "dataset_exchange_coverage", "dataset_tier_a_members", "mean_reversion_decisions",
	"triangular_candidates", "triangular_candidate_legs", "triangular_simulation_outcomes",
	"triangular_opportunity_lifetimes", "triangular_journal_links",
	"cross_exchange_candidates", "cross_exchange_candidate_members",
	"cross_exchange_candidate_legs", "cross_exchange_inventory_snapshots",
	"cross_exchange_simulation_outcomes", "cross_exchange_simulation_legs",
	"cross_exchange_rebalancing_needs", "cross_exchange_journal_links",
	"rebalancing_fact_sets", "rebalancing_route_facts", "rebalancing_recommendations",
	"rebalancing_recommendation_steps", "rebalancing_checklist_steps",
	"b7_experiment_preregistrations", "b7_validation_suites", "b7_champion_challenger_reports",
	"b8_replay_fault_schedule_states", "b8_replay_fault_schedules", "b8_report_exports",
	"exchange_capabilities", "exchanges", "execution_lease_epochs", "execution_leases", "execution_plan_legs",
	"execution_plans", "experiment_registrations", "fills", "inbox_events", "incidents", "instrument_metadata_versions",
	"instruments", "jobs", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"circuit_breaker_events", "fill_journal_postings", "liquidity_domains", "liquidity_reservations", "model_namespaces", "opportunities", "order_attempts", "order_events", "order_reduction_incidents", "orders", "outbox_events", "portfolio_ownership", "portfolios", "positions",
	"projection_revisions", "quarantined_scopes", "reconciliation_cases", "reconciliation_differences", "reconciliation_suspense", "recovery_attempts", "reservations",
	"public_clock_samples", "public_connection_events",
	"risk_evaluation_policies", "risk_evaluations", "risk_policies", "risk_policy_limits", "risk_state_events", "run_checkpoints", "run_results", "runs", "sessions", "startup_recovery_attempts", "startup_recovery_evidence", "strategy_definitions", "strategy_parameters",
	"experiment_final_test_consumptions", "research_generations", "research_reports", "role_permissions", "run_canonical_outputs", "run_manifests", "shadow_sessions", "strategy_portfolios", "strategy_versions", "stream_connections", "trend_decisions", "user_roles", "users", "virtual_accounts", "virtual_balances",
}

var runtimeUpdateTables = []string{
	"alert_deliveries", "alerts", "allocation_candidates", "assets", "command_requests", "consumer_cursors", "dataset_manifests", "execution_lease_epochs",
	"execution_leases", "incidents", "jobs", "market_data_segments", "model_versions", "orders", "outbox_events",
	"liquidity_domains", "liquidity_reservations", "positions", "projection_revisions", "quarantined_scopes", "reconciliation_cases", "reservations", "runs", "sessions", "startup_recovery_attempts",
	"api_entity_revisions", "shadow_sessions", "stream_connections", "users", "virtual_balances",
	"b8_replay_fault_schedule_states",
	"v1c_sandbox_authorizations", "v1c_totp_replay_state", "v1c_sandbox_sessions", "v1c_sandbox_arms",
	"v1c_exchange_accounts", "v1c_daily_cap_counters", "v1c_engine_commands",
	"v1d_strategy_controls", "v1d_risk_controls", "v1d_export_artifacts",
	"v1d_artifact_holds", "v1d_qualification_runs", "v1d_report_schedules",
	"v1d_reports", "v1d_alert_routes", "v1d_alert_route_tests",
}

var runtimeDeleteTables = []string{"execution_leases", "sessions", "user_roles"}

var runtimeReadTables = []string{
	"schema_migrations", "b4_claim_resources", "b4_claim_groups", "b4_claim_items",
	"b5_claim_resources", "b5_claim_groups", "b5_claim_items",
	"strategy_maturity_states", "strategy_maturity_commands", "strategy_maturity_events",
	"v1c_exchange_accounts", "v1c_account_epochs", "v1c_credential_generations",
	"v1c_authenticated_request_evidence",
	"v1c_account_snapshots", "v1c_private_inbox", "v1c_exchange_fills",
	"v1c_exchange_metadata", "v1c_reconciliation_differences",
	"v1c_reconciliations", "v1c_reset_incidents", "v1c_external_adjustments",
	"v1c_risk_unlocks", "v1c_account_leases", "v1c_engine_startup_evidence",
	"v1c_engine_observations",
	"v1c_engine_runtime_events", "v1c_c6_order_observations",
	"v1c_c6_qualification_runs", "v1c_c6_qualification_accounts",
	"v1c_c6_qualification_samples", "v1c_c6_qualification_failures",
	"v1c_c6_chaos_events",
	"v1d_reason_catalogue", "v1d_activity_projection", "v1d_activity_explanations",
	"v1d_strategy_controls", "v1d_risk_controls", "v1d_qualification_catalogue",
	"v1d_alert_routes", "v1d_audit_chain", "v1d_storage_pressure_state",
}

var recorderReadTables = []string{
	"assets", "configuration_versions", "exchanges", "instruments", "instrument_metadata_versions",
}

var recorderWriteTables = []string{
	"alert_deliveries", "alerts", "data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "market_data_segments",
	"v1d_storage_pressure_state",
}

var recorderAppendTables = []string{"audit_events", "dataset_exchange_coverage", "dataset_tier_a_members", "instrument_metadata_versions", "public_clock_samples", "public_connection_events", "v1d_storage_pressure_events"}

// Shared alert services run in every process role. They may append immutable
// delivery evidence and update only the bounded route state used for delivery
// tests and validated webhook availability.
var processAlertAppendTables = []string{"v1d_alert_delivery_attempts"}

var processAlertUpdateTables = []string{"v1d_alert_routes", "v1d_alert_route_tests"}

var v1cEngineReadWriteTables = []string{
	"v1c_exchange_accounts", "v1c_account_epochs", "v1c_credential_generations",
	"v1c_credential_rotations",
	"v1c_sandbox_sessions", "v1c_sandbox_session_accounts", "v1c_sandbox_arms",
	"v1c_authenticated_request_evidence",
	"v1c_account_snapshots", "v1c_daily_cap_counters", "v1c_submission_plans",
	"v1c_plan_eligibility", "v1c_plan_entry_safety", "v1c_sandbox_reservations",
	"v1c_submission_outbox", "v1c_private_inbox",
	"v1c_exchange_fills", "v1c_exchange_metadata", "v1c_reconciliation_differences",
	"v1c_reconciliations", "v1c_reset_incidents", "v1c_external_adjustments",
	"v1c_risk_unlocks", "v1c_account_leases",
	"v1c_engine_startup_evidence",
	"v1c_engine_commands", "v1c_engine_observations",
}

var v1cEngineReadOnlyTables = []string{
	"sessions", "users",
}

var v1cEngineAlertReadWriteTables = []string{
	"alert_deliveries", "alerts",
}

var v1cEngineAlertAppendTables = []string{
	"audit_events",
}

var v1cEngineRuntimeAppendTables = []string{
	"v1c_engine_runtime_events",
}

var c6QualificationReadTables = []string{
	"alerts", "incidents", "v1c_authenticated_request_evidence",
	"v1c_daily_cap_counters", "v1c_engine_observations",
	"v1c_engine_runtime_events", "v1c_c6_order_observations",
	"v1c_exchange_accounts", "v1c_account_leases",
	"v1c_reconciliation_differences",
}

var c6QualificationAppendTables = []string{
	"v1c_c6_qualification_runs", "v1c_c6_qualification_accounts",
	"v1c_c6_qualification_samples", "v1c_c6_qualification_failures",
	"v1c_c6_chaos_events",
}

var readOnlyTables = []string{
	"account_snapshots", "alert_acknowledgements", "alert_deliveries", "alerts", "allocation_candidates", "allocation_reservations", "allocation_score_components", "asset_screening_versions", "assets", "audit_events",
	"configuration_activations", "configuration_versions", "consumer_cursors", "data_quality_events",
	"dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions", "exchange_capabilities",
	"cross_market_view_headers", "cross_market_view_members", "dataset_exchange_coverage", "dataset_tier_a_members", "mean_reversion_decisions",
	"triangular_candidates", "triangular_candidate_legs", "triangular_simulation_outcomes",
	"triangular_opportunity_lifetimes", "triangular_journal_links",
	"b4_claim_resources", "b4_claim_groups", "b4_claim_items",
	"cross_exchange_candidates", "cross_exchange_candidate_members",
	"cross_exchange_candidate_legs", "cross_exchange_inventory_snapshots",
	"cross_exchange_simulation_outcomes", "cross_exchange_simulation_legs",
	"cross_exchange_rebalancing_needs", "cross_exchange_journal_links",
	"b5_claim_resources", "b5_claim_groups", "b5_claim_items",
	"rebalancing_fact_sets", "rebalancing_route_facts", "rebalancing_recommendations",
	"rebalancing_recommendation_steps", "rebalancing_checklist_steps",
	"b7_experiment_preregistrations", "b7_validation_suites", "b7_champion_challenger_reports",
	"b8_replay_fault_schedule_states", "b8_replay_fault_schedules", "b8_report_exports",
	"strategy_maturity_states", "strategy_maturity_commands", "strategy_maturity_events",
	"v1c_exchange_accounts", "v1c_account_epochs", "v1c_credential_generations",
	"v1c_sandbox_sessions", "v1c_sandbox_session_accounts", "v1c_sandbox_arms",
	"v1c_submission_plans", "v1c_plan_entry_safety",
	"v1c_sandbox_reservations", "v1c_submission_outbox", "v1c_private_inbox",
	"v1c_exchange_fills", "v1c_reconciliations",
	"v1c_reconciliation_differences", "v1c_reset_incidents",
	"v1c_external_adjustments", "v1c_engine_startup_evidence",
	"v1c_engine_commands", "v1c_engine_observations",
	"v1c_engine_runtime_events", "v1c_c6_order_observations",
	"v1c_c6_qualification_runs", "v1c_c6_qualification_accounts",
	"v1c_c6_qualification_samples", "v1c_c6_qualification_failures",
	"v1c_c6_chaos_events",
	"circuit_breaker_events", "exchanges", "execution_plan_legs", "execution_plans", "fill_journal_postings", "fills", "incidents", "instrument_metadata_versions",
	"instruments", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"public_clock_samples", "public_connection_events",
	"liquidity_domains", "liquidity_reservations", "model_namespaces", "opportunities", "order_attempts", "order_events", "order_reduction_incidents", "orders", "portfolio_ownership", "portfolios", "positions",
	"projection_revisions", "quarantined_scopes", "reconciliation_cases", "reconciliation_differences", "reconciliation_suspense", "reservations", "risk_evaluation_policies", "risk_evaluations", "risk_policies", "risk_policy_limits", "risk_state_events",
	"experiment_final_test_consumptions", "research_generations", "research_reports", "run_canonical_outputs", "run_checkpoints", "run_manifests", "run_results", "runs", "shadow_sessions", "startup_recovery_attempts", "startup_recovery_evidence", "strategy_definitions", "strategy_parameters", "strategy_portfolios",
	"strategy_versions", "trend_decisions", "virtual_accounts", "virtual_balances",
	"v1d_reason_catalogue", "v1d_activity_projection", "v1d_activity_explanations",
	"v1d_strategy_controls", "v1d_risk_controls", "v1d_export_artifacts",
	"v1d_artifact_holds", "v1d_artifact_access_events", "v1d_qualification_catalogue",
	"v1d_qualification_runs", "v1d_role_change_events",
	"v1d_incident_events", "v1d_incident_replay_inputs", "v1d_incident_alert_links",
	"v1d_incident_activity_links", "v1d_incident_resolution_evidence",
	"v1d_report_schedules", "v1d_reports", "v1d_alert_routes",
	"v1d_alert_delivery_attempts", "v1d_alert_escalations", "v1d_alert_route_tests",
	"v1d_audit_chain", "v1d_storage_pressure_state", "v1d_storage_pressure_events",
}

// ApplyV1CEngineRoleGrants keeps authenticated engines on distinct database
// principals and restricts both to V1C execution plus the bounded operational
// alert records emitted by every process role.
func ApplyV1CEngineRoleGrants(
	ctx context.Context,
	pool *pgxpool.Pool,
	binanceRole, bybitRole string,
) error {
	if pool == nil || !validDistinctRoles([]string{binanceRole, bybitRole}) {
		return fmt.Errorf("v1c_database_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("v1c_role_grant_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	available, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	tables := filterTableGrants(v1cEngineTableGrants(), available)
	for _, role := range []string{binanceRole, bybitRole} {
		if err = applyTableGrants(ctx, tx, role, tables); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_role_grant_commit_failed")
	}
	return nil
}

func v1cEngineTableGrants() []tableGrant {
	readWrite := append(append([]string(nil), v1cEngineReadWriteTables...),
		v1cEngineAlertReadWriteTables...)
	appendOnly := append(append(append([]string(nil), v1cEngineAlertAppendTables...),
		v1cEngineRuntimeAppendTables...), processAlertAppendTables...)
	return []tableGrant{
		{privileges: "SELECT, INSERT, UPDATE", tables: readWrite},
		{privileges: "SELECT, INSERT", tables: appendOnly},
		{privileges: "SELECT, UPDATE", tables: processAlertUpdateTables},
		{privileges: "SELECT", tables: v1cEngineReadOnlyTables},
	}
}

// ApplyC6QualificationRoleGrants restricts the manual soak observer to
// redacted operational reads and append-only qualification evidence.
func ApplyC6QualificationRoleGrants(
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) error {
	if pool == nil || !roleNamePattern.MatchString(roleName) {
		return fmt.Errorf("c6_qualification_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("c6_qualification_role_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	available, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	grants := filterTableGrants([]tableGrant{
		{privileges: "SELECT", tables: c6QualificationReadTables},
		{
			privileges: "SELECT, INSERT",
			tables:     c6QualificationAppendTables,
		},
		{
			privileges: "UPDATE",
			tables:     []string{"v1c_c6_qualification_runs"},
		},
	}, available)
	if err = applyTableGrants(ctx, tx, roleName, grants); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("c6_qualification_role_commit_failed")
	}
	return nil
}

// ApplyRoleGrants applies the closed runtime, recorder, and reporting matrices.
func ApplyRoleGrants(ctx context.Context, pool *pgxpool.Pool, runtimeRole, recorderRole, readOnlyRole string) error {
	roles := []string{runtimeRole, recorderRole, readOnlyRole}
	if pool == nil || !validDistinctRoles(roles) {
		return fmt.Errorf("database_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("role_grant_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	availableTables, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	grants := roleTableGrants(runtimeRole, recorderRole, readOnlyRole)
	for _, role := range roles {
		filtered := filterTableGrants(grants[role], availableTables)
		if err = applyTableGrants(ctx, tx, role, filtered); err != nil {
			return err
		}
	}
	if err = applyStrategyFunctionGrants(ctx, tx, runtimeRole); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("role_grant_commit_failed")
	}
	return nil
}

func roleTableGrants(runtimeRole, recorderRole, readOnlyRole string) map[string][]tableGrant {
	return map[string][]tableGrant{
		runtimeRole: {
			{privileges: "SELECT", tables: runtimeReadTables},
			{privileges: "SELECT, INSERT", tables: runtimeReadInsertTables},
			{privileges: "UPDATE", tables: runtimeUpdateTables},
			{privileges: "DELETE", tables: runtimeDeleteTables},
		},
		recorderRole: {
			{privileges: "SELECT", tables: recorderReadTables},
			{privileges: "SELECT, INSERT, UPDATE", tables: recorderWriteTables},
			{privileges: "SELECT, INSERT", tables: recorderAppendTables},
			{privileges: "SELECT, INSERT", tables: processAlertAppendTables},
			{privileges: "SELECT, UPDATE", tables: processAlertUpdateTables},
		},
		readOnlyRole: {{privileges: "SELECT", tables: readOnlyTables}},
	}
}

func applyStrategyFunctionGrants(ctx context.Context, tx pgx.Tx, runtimeRole string) error {
	role := pgx.Identifier{runtimeRole}.Sanitize()
	functions := []string{
		"public.register_b4_claim_resource(text,text,text,text,text,financial_amount,timestamptz)",
		"public.claim_b4_resources(text,text,text,bigint,text,text,text[],numeric[],timestamptz)",
		"public.settle_b4_claim_group(text,bigint,bigint,text[],numeric[],boolean,timestamptz)",
		"public.close_b4_claim_group(text,bigint,bigint,text,timestamptz)",
		"public.register_b5_claim_resource(text,text,text,text,text,financial_amount,timestamptz)",
		"public.claim_b5_resources(text,text,bigint,text,text,text[],numeric[],timestamptz)",
		"public.settle_b5_claim_group(text,bigint,bigint,text[],numeric[],boolean,timestamptz)",
		"public.close_b5_claim_group(text,bigint,bigint,text,timestamptz)",
		"public.apply_b7_maturity_promotion(text,text,text,sha256_hex,text,bigint,text,text,text,sha256_hex,text,timestamptz)",
	}
	for _, function := range functions {
		var exists bool
		if err := tx.QueryRow(ctx,
			"SELECT pg_catalog.to_regprocedure($1) IS NOT NULL", function,
		).Scan(&exists); err != nil {
			return fmt.Errorf("role_function_lookup_failed")
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+function+" TO "+role); err != nil {
			return fmt.Errorf("role_function_grant_failed")
		}
	}
	return nil
}
