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
	"authorization_roles", "command_requests", "configuration_activations", "configuration_versions", "consumer_cursors",
	"data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions",
	"exchange_capabilities", "exchanges", "execution_lease_epochs", "execution_leases", "execution_plan_legs",
	"execution_plans", "experiment_registrations", "fills", "inbox_events", "incidents", "instrument_metadata_versions",
	"instruments", "jobs", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"opportunities", "order_attempts", "order_events", "orders", "outbox_events", "portfolios", "positions",
	"projection_revisions", "reconciliation_cases", "reconciliation_suspense", "recovery_attempts", "reservations",
	"risk_evaluations", "run_checkpoints", "run_results", "runs", "sessions", "strategy_definitions", "strategy_parameters",
	"strategy_portfolios", "strategy_versions", "user_roles", "users", "virtual_accounts", "virtual_balances",
}

var runtimeUpdateTables = []string{
	"alert_deliveries", "alerts", "assets", "command_requests", "consumer_cursors", "dataset_manifests", "execution_lease_epochs",
	"execution_leases", "incidents", "jobs", "market_data_segments", "model_versions", "orders", "outbox_events",
	"positions", "projection_revisions", "reconciliation_cases", "reservations", "runs", "sessions", "strategy_versions",
	"users", "virtual_balances",
}

var runtimeDeleteTables = []string{"execution_leases", "sessions", "user_roles"}

var recorderReadTables = []string{
	"assets", "configuration_versions", "exchanges", "instruments", "instrument_metadata_versions",
}

var recorderWriteTables = []string{
	"alert_deliveries", "alerts", "data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "market_data_segments",
}

var recorderAppendTables = []string{"audit_events"}

var readOnlyTables = []string{
	"alert_acknowledgements", "alert_deliveries", "alerts", "asset_screening_versions", "assets", "audit_events",
	"configuration_activations", "configuration_versions", "consumer_cursors", "data_quality_events",
	"dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions", "exchange_capabilities",
	"exchanges", "execution_plan_legs", "execution_plans", "fills", "incidents", "instrument_metadata_versions",
	"instruments", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"opportunities", "order_attempts", "order_events", "orders", "portfolios", "positions",
	"projection_revisions", "reconciliation_cases", "reconciliation_suspense", "reservations", "risk_evaluations",
	"run_checkpoints", "run_results", "runs", "strategy_definitions", "strategy_parameters", "strategy_portfolios",
	"strategy_versions", "virtual_accounts", "virtual_balances",
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
	grants := map[string][]tableGrant{
		runtimeRole: {
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
	for _, role := range roles {
		if err = applyTableGrants(ctx, tx, role, grants[role]); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("role_grant_commit_failed")
	}
	return nil
}

type tableGrant struct {
	privileges string
	tables     []string
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
