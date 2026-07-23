package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

var runtimeReadInsertTables = []string{
	"account_snapshots", "alert_acknowledgements", "alert_deliveries", "alerts", "asset_screening_versions", "assets", "audit_events",
	"api_entity_revisions", "authentication_failures", "authorization_permissions",
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
	"liquidity_domains", "liquidity_reservations", "positions", "projection_revisions", "quarantined_scopes", "reconciliation_cases", "reservations", "runs", "sessions", "startup_recovery_attempts", "strategy_versions",
	"api_entity_revisions", "shadow_sessions", "stream_connections", "users", "virtual_balances",
}

var runtimeDeleteTables = []string{"execution_leases", "sessions", "user_roles"}

var runtimeReadTables = []string{
	"schema_migrations", "b4_claim_resources", "b4_claim_groups", "b4_claim_items",
	"b5_claim_resources", "b5_claim_groups", "b5_claim_items",
}

var recorderReadTables = []string{
	"assets", "configuration_versions", "exchanges", "instruments", "instrument_metadata_versions",
}

var recorderWriteTables = []string{
	"alert_deliveries", "alerts", "data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "market_data_segments",
}

var recorderAppendTables = []string{"audit_events", "dataset_exchange_coverage", "dataset_tier_a_members", "instrument_metadata_versions", "public_clock_samples", "public_connection_events"}

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
	"circuit_breaker_events", "exchanges", "execution_plan_legs", "execution_plans", "fill_journal_postings", "fills", "incidents", "instrument_metadata_versions",
	"instruments", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"public_clock_samples", "public_connection_events",
	"liquidity_domains", "liquidity_reservations", "model_namespaces", "opportunities", "order_attempts", "order_events", "order_reduction_incidents", "orders", "portfolio_ownership", "portfolios", "positions",
	"projection_revisions", "quarantined_scopes", "reconciliation_cases", "reconciliation_differences", "reconciliation_suspense", "reservations", "risk_evaluation_policies", "risk_evaluations", "risk_policies", "risk_policy_limits", "risk_state_events",
	"experiment_final_test_consumptions", "research_generations", "research_reports", "run_canonical_outputs", "run_checkpoints", "run_manifests", "run_results", "runs", "shadow_sessions", "startup_recovery_attempts", "startup_recovery_evidence", "strategy_definitions", "strategy_parameters", "strategy_portfolios",
	"strategy_versions", "trend_decisions", "virtual_accounts", "virtual_balances",
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

type tableGrant struct {
	privileges string
	tables     []string
}

func existingPublicTables(ctx context.Context, tx pgx.Tx) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `SELECT relation.relname
FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = 'public' AND relation.relkind IN ('r','p','v','m','f')`)
	if err != nil {
		return nil, fmt.Errorf("role_table_lookup_failed")
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("role_table_lookup_failed")
		}
		result[table] = struct{}{}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("role_table_lookup_failed")
	}
	return result, nil
}

func filterTableGrants(grants []tableGrant, available map[string]struct{}) []tableGrant {
	result := make([]tableGrant, 0, len(grants))
	for _, grant := range grants {
		tables := make([]string, 0, len(grant.tables))
		for _, table := range grant.tables {
			if _, exists := available[table]; exists {
				tables = append(tables, table)
			}
		}
		if len(tables) > 0 {
			result = append(result, tableGrant{privileges: grant.privileges, tables: tables})
		}
	}
	return result
}

func validDistinctRoles(roles []string) bool {
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if !roleNamePattern.MatchString(role) {
			return false
		}
		seen[role] = struct{}{}
	}
	return len(seen) == len(roles)
}

func applyTableGrants(ctx context.Context, tx pgx.Tx, roleName string, grants []tableGrant) error {
	role := pgx.Identifier{roleName}.Sanitize()
	if _, err := tx.Exec(ctx, "REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "+role); err != nil {
		return fmt.Errorf("role_revoke_failed")
	}
	for _, grant := range grants {
		if _, err := tx.Exec(ctx, grantSQL(grant.privileges, grant.tables, role)); err != nil {
			return fmt.Errorf("role_grant_failed")
		}
	}
	return nil
}

func grantSQL(privileges string, tables []string, role string) string {
	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(quoted, pgx.Identifier{"public", table}.Sanitize())
	}
	return "GRANT " + privileges + " ON " + strings.Join(quoted, ", ") + " TO " + role
}
