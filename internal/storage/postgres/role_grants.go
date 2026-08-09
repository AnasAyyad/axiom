package postgres

import (
	"context"
	"fmt"

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
