package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplySandboxRuntimeEngineRoleGrants keeps authenticated engines on distinct database
// principals and restricts both to sandbox runtime execution plus the bounded operational
// alert records emitted by every process role.
func ApplySandboxRuntimeEngineRoleGrants(
	ctx context.Context,
	pool *pgxpool.Pool,
	binanceRole, bybitRole string,
) error {
	if pool == nil || !validDistinctRoles([]string{binanceRole, bybitRole}) {
		return fmt.Errorf("sandbox_runtime_database_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("sandbox_runtime_role_grant_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	available, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	tables := filterTableGrants(sandboxRuntimeEngineTableGrants(), available)
	for _, role := range []string{binanceRole, bybitRole} {
		if err = applyTableGrants(ctx, tx, role, tables); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_role_grant_commit_failed")
	}
	return nil
}

func sandboxRuntimeEngineTableGrants() []tableGrant {
	readWrite := append(append([]string(nil), sandboxRuntimeEngineReadWriteTables...),
		sandboxRuntimeEngineAlertReadWriteTables...)
	appendOnly := append(append(append([]string(nil), sandboxRuntimeEngineAlertAppendTables...),
		sandboxRuntimeEngineRuntimeAppendTables...), processAlertAppendTables...)
	appendOnly = append(appendOnly, sandboxRuntimeEngineRiskStateAppendTables...)
	stateUpdates := append(append([]string(nil), processAlertUpdateTables...), sandboxRuntimeEngineRiskStateUpdateTables...)
	return []tableGrant{
		{privileges: "SELECT, INSERT, UPDATE", tables: readWrite},
		{privileges: "SELECT, INSERT", tables: appendOnly},
		{privileges: "SELECT, UPDATE", tables: stateUpdates},
		{privileges: "SELECT", tables: sandboxRuntimeEngineReadOnlyTables},
	}
}

// ApplySandboxQualificationRoleGrants restricts the manual soak observer to
// redacted operational reads and append-only qualification evidence.
func ApplySandboxQualificationRoleGrants(
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) error {
	if pool == nil || !roleNamePattern.MatchString(roleName) {
		return fmt.Errorf("sandbox_qualification_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("sandbox_qualification_role_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	available, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	grants := filterTableGrants([]tableGrant{
		{privileges: "SELECT", tables: sandboxQualificationReadTables},
		{
			privileges: "SELECT, INSERT",
			tables:     sandboxQualificationAppendTables,
		},
		{
			privileges: "UPDATE",
			tables:     []string{"sandbox_qualification_runs"},
		},
	}, available)
	if err = applyTableGrants(ctx, tx, roleName, grants); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_qualification_role_commit_failed")
	}
	return nil
}

// ApplyOperationalReadinessObserverRoleGrants resets the dedicated D5 observer
// role to six aggregate evidence sources. It has no private inbox, credential,
// owner, journal-write, or qualification-write access.
func ApplyOperationalReadinessObserverRoleGrants(
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
	protectedRoles ...string,
) error {
	roles := append([]string{roleName}, protectedRoles...)
	if pool == nil || !validDistinctRoles(roles) {
		return fmt.Errorf("operational_readiness_observer_role_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("operational_readiness_observer_role_transaction_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	available, err := existingPublicTables(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range operationalReadinessObserverTables {
		if _, exists := available[table]; !exists {
			return fmt.Errorf("operational_readiness_observer_role_table_unavailable")
		}
	}
	role := pgx.Identifier{roleName}.Sanitize()
	for _, statement := range []string{
		"REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM " + role,
		"REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM " + role,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("operational_readiness_observer_role_revoke_failed")
		}
	}
	if err = applyTableGrants(ctx, tx, roleName, []tableGrant{{
		privileges: "SELECT", tables: operationalReadinessObserverTables,
	}}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("operational_readiness_observer_role_commit_failed")
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
	if err = applyEvaluationRecorderColumnGrants(ctx, tx, recorderRole, availableTables); err != nil {
		return err
	}
	if err = applyStrategyFunctionGrants(ctx, tx, runtimeRole); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("role_grant_commit_failed")
	}
	return nil
}

func applyEvaluationRecorderColumnGrants(ctx context.Context, tx pgx.Tx, recorderRole string,
	available map[string]struct{}) error {
	role := pgx.Identifier{recorderRole}.Sanitize()
	grants := []struct {
		table   string
		columns []string
	}{
		{table: "evaluation_campaigns", columns: []string{
			"valid_recording_seconds", "valid_shadow_seconds", "recording_last_valid_at",
			"shadow_last_valid_at", "campaign_recorded_bytes", "measured_bytes_per_hour", "updated_at",
		}},
		{table: "evaluation_recorder_requests", columns: []string{
			"state", "previous_session_id", "recorded_bytes", "valid_recording_seconds", "last_valid_at",
			"measured_bytes_per_hour", "reason_code", "finalized_at", "activated_at", "completed_at", "updated_at",
		}},
	}
	for _, grant := range grants {
		if _, exists := available[grant.table]; !exists {
			continue
		}
		columns := make([]string, 0, len(grant.columns))
		for _, column := range grant.columns {
			columns = append(columns, pgx.Identifier{column}.Sanitize())
		}
		statement := "GRANT UPDATE (" + strings.Join(columns, ", ") + ") ON " +
			pgx.Identifier{"public", grant.table}.Sanitize() + " TO " + role
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("evaluation_recorder_column_grant_failed")
		}
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
		"public.register_triangular_arbitrage_claim_resource(text,text,text,text,text,financial_amount,timestamptz)",
		"public.claim_triangular_arbitrage_resources(text,text,text,bigint,text,text,text[],numeric[],timestamptz)",
		"public.settle_triangular_arbitrage_claim_group(text,bigint,bigint,text[],numeric[],boolean,timestamptz)",
		"public.close_triangular_arbitrage_claim_group(text,bigint,bigint,text,timestamptz)",
		"public.register_cross_exchange_arbitrage_claim_resource(text,text,text,text,text,financial_amount,timestamptz)",
		"public.claim_cross_exchange_arbitrage_resources(text,text,bigint,text,text,text[],numeric[],timestamptz)",
		"public.settle_cross_exchange_arbitrage_claim_group(text,bigint,bigint,text[],numeric[],boolean,timestamptz)",
		"public.close_cross_exchange_arbitrage_claim_group(text,bigint,bigint,text,timestamptz)",
		"public.apply_research_promotion_maturity_promotion(text,text,text,sha256_hex,text,bigint,text,text,text,sha256_hex,text,timestamptz)",
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
