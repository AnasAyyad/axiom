package postgres

import (
	"strings"
	"testing"
)

func TestRecorderGrantSQLUsesClosedReviewedTables(t *testing.T) {
	statement := grantSQL("SELECT, INSERT, UPDATE", recorderWriteTables, `"axiom_recorder"`)
	for _, table := range recorderWriteTables {
		if !strings.Contains(statement, `"public"."`+table+`"`) {
			t.Fatalf("grant omits %s: %s", table, statement)
		}
	}
	for _, forbidden := range []string{"users", "sessions", "orders", "journal_transactions", "execution_leases"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("recorder grant contains %s", forbidden)
		}
	}
}

func TestRoleNamesRejectSQLAndMixedCase(t *testing.T) {
	for _, role := range []string{"", "AxiomRecorder", "recorder;select", "recorder-role"} {
		if roleNamePattern.MatchString(role) {
			t.Fatalf("unsafe role accepted: %q", role)
		}
	}
	if validDistinctRoles([]string{"axiom_runtime", "axiom_runtime", "axiom_readonly"}) {
		t.Fatal("duplicate database roles accepted")
	}
}

func TestRuntimeMutationGrantsExcludeImmutableHistory(t *testing.T) {
	updates := grantSQL("UPDATE", runtimeUpdateTables, `"axiom_runtime"`)
	deletes := grantSQL("DELETE", runtimeDeleteTables, `"axiom_runtime"`)
	for _, table := range []string{
		"audit_events", "fills", "inbox_events", "journal_transactions", "ledger_entries", "order_events",
		"run_results", "strategy_versions", "strategy_maturity_states",
		"v1c_high_risk_audit_events",
	} {
		if strings.Contains(updates, `"`+table+`"`) || strings.Contains(deletes, `"`+table+`"`) {
			t.Fatalf("runtime can mutate immutable history table %s", table)
		}
	}
	for _, table := range []string{"execution_leases", "sessions", "user_roles"} {
		if !strings.Contains(deletes, `"public"."`+table+`"`) {
			t.Fatalf("runtime delete grant omits %s", table)
		}
	}
}

func TestRuntimeMigrationLedgerGrantIsReadOnly(t *testing.T) {
	read := grantSQL("SELECT", runtimeReadTables, `"axiom_runtime"`)
	write := grantSQL("SELECT, INSERT", runtimeReadInsertTables, `"axiom_runtime"`)
	if !strings.Contains(read, `"public"."schema_migrations"`) || strings.Contains(write, `"schema_migrations"`) {
		t.Fatalf("runtime migration-ledger grants are not read-only: %s / %s", read, write)
	}
}

func TestRuntimeAuthenticatedEvidenceGrantIsReadOnly(t *testing.T) {
	const table = `"public"."v1c_authenticated_request_evidence"`
	read := grantSQL("SELECT", runtimeReadTables, `"axiom_runtime"`)
	insert := grantSQL("SELECT, INSERT", runtimeReadInsertTables, `"axiom_runtime"`)
	update := grantSQL("UPDATE", runtimeUpdateTables, `"axiom_runtime"`)
	deleted := grantSQL("DELETE", runtimeDeleteTables, `"axiom_runtime"`)
	if !strings.Contains(read, table) ||
		strings.Contains(insert, table) ||
		strings.Contains(update, table) ||
		strings.Contains(deleted, table) {
		t.Fatalf(
			"runtime authenticated-evidence grants are not read-only: %s / %s / %s / %s",
			read,
			insert,
			update,
			deleted,
		)
	}
}

func TestRuntimePostCreateCanaryCoordinatorGrantsRemainClosed(t *testing.T) {
	read := append(
		append([]string(nil), runtimeReadTables...),
		runtimeReadInsertTables...,
	)
	for _, table := range []string{
		"v1c_submission_plans",
		"v1c_submission_outbox",
		"v1c_exchange_accounts",
		"v1c_authenticated_request_evidence",
		"v1c_canary_evidence",
		"v1c_engine_observations",
		"v1c_account_leases",
		"v1c_engine_startup_evidence",
		"v1c_engine_commands",
		"v1c_sandbox_sessions",
		"v1c_sandbox_session_accounts",
		"v1c_sandbox_arms",
	} {
		if !containsGrantTable(read, table) {
			t.Fatalf("runtime post-create canary read omits %s", table)
		}
	}
	for _, table := range []string{
		"v1c_canary_evidence",
		"v1c_engine_commands",
	} {
		if !containsGrantTable(runtimeReadInsertTables, table) {
			t.Fatalf("runtime post-create canary insert omits %s", table)
		}
	}
	for _, table := range []string{
		"v1c_sandbox_sessions",
		"v1c_sandbox_arms",
		"v1c_exchange_accounts",
	} {
		if !containsGrantTable(runtimeUpdateTables, table) {
			t.Fatalf("runtime post-create canary update omits %s", table)
		}
	}
}

func containsGrantTable(tables []string, expected string) bool {
	for _, table := range tables {
		if table == expected {
			return true
		}
	}
	return false
}

func TestRoleGrantFilteringPreservesAppliedMigrationPrefix(t *testing.T) {
	available := map[string]struct{}{
		"schema_migrations":     {},
		"triangular_candidates": {},
	}
	filtered := filterTableGrants([]tableGrant{
		{privileges: "SELECT", tables: []string{
			"schema_migrations", "triangular_candidates", "cross_exchange_candidates",
		}},
		{privileges: "UPDATE", tables: []string{"cross_exchange_candidates"}},
	}, available)
	if len(filtered) != 1 ||
		strings.Join(filtered[0].tables, ",") != "schema_migrations,triangular_candidates" {
		t.Fatalf("migration-prefix grant filter = %#v", filtered)
	}
}

func TestReadOnlyReportingExcludesCredentialTables(t *testing.T) {
	statement := grantSQL("SELECT", readOnlyTables, `"axiom_readonly"`)
	for _, forbidden := range []string{"users", "sessions", "authorization_roles", "user_roles"} {
		if strings.Contains(statement, `"`+forbidden+`"`) {
			t.Fatalf("reporting grant exposes %s", forbidden)
		}
	}
}

func TestV1CEngineGrantIncludesClosedExecutionAndAlertTables(t *testing.T) {
	role := `"axiom_binance_testnet_engine"`
	statement := grantSQL(
		"SELECT, INSERT, UPDATE",
		append(
			append([]string(nil), v1cEngineReadWriteTables...),
			v1cEngineAlertReadWriteTables...,
		),
		role,
	)
	if !strings.Contains(
		statement,
		`"public"."v1c_authenticated_request_evidence"`,
	) {
		t.Fatal("V1C engine cannot persist authenticated request evidence")
	}
	for _, table := range v1cEngineAlertReadWriteTables {
		if !strings.Contains(statement, `"public"."`+table+`"`) {
			t.Fatalf("V1C engine cannot persist operational alert table %s", table)
		}
	}
	appendOnly := grantSQL(
		"SELECT, INSERT",
		append(
			append([]string(nil), v1cEngineAlertAppendTables...),
			v1cEngineRuntimeAppendTables...,
		),
		role,
	)
	if !strings.Contains(appendOnly, `"public"."audit_events"`) {
		t.Fatal("V1C engine cannot append alert audit evidence")
	}
	if !strings.Contains(
		appendOnly,
		`"public"."v1c_engine_runtime_events"`,
	) {
		t.Fatal("V1C engine cannot append redacted runtime recovery evidence")
	}
	assertV1CEngineReadOnlyGrants(t, role)
	assertV1CEngineExcludedGrants(t, role, statement)
}

func TestC6QualificationRoleIsDedicatedAndLeastPrivilege(t *testing.T) {
	read := grantSQL(
		"SELECT",
		c6QualificationReadTables,
		`"axiom_c6_qualification"`,
	)
	appendOnly := grantSQL(
		"SELECT, INSERT",
		c6QualificationAppendTables,
		`"axiom_c6_qualification"`,
	)
	assertC6RoleAppendGrants(t, appendOnly)
	assertC6RoleForbiddenGrants(t, read, appendOnly)
	assertC6RoleReadGrants(t, read)
	assertC6RoleIsolation(t)
}

func assertC6RoleAppendGrants(t *testing.T, appendOnly string) {
	t.Helper()
	for _, table := range []string{
		"v1c_c6_qualification_runs",
		"v1c_c6_qualification_accounts",
		"v1c_c6_qualification_samples",
		"v1c_c6_qualification_failures",
		"v1c_c6_chaos_events",
		"v1c_c6_recovery_events",
	} {
		if !strings.Contains(
			appendOnly,
			`"public"."`+table+`"`,
		) {
			t.Fatalf("C6 qualification append grant omits %s", table)
		}
	}
}

func assertC6RoleForbiddenGrants(t *testing.T, read, appendOnly string) {
	t.Helper()
	for _, forbidden := range []string{
		"users",
		"sessions",
		"v1c_sandbox_authorizations",
		"v1c_credential_generations",
		"v1c_private_inbox",
		"v1c_exchange_fills",
	} {
		if strings.Contains(read, `"`+forbidden+`"`) ||
			strings.Contains(appendOnly, `"`+forbidden+`"`) {
			t.Fatalf("C6 qualification role exposes %s", forbidden)
		}
	}
}

func assertC6RoleReadGrants(t *testing.T, read string) {
	t.Helper()
	for _, required := range []string{
		"v1c_engine_runtime_events",
		"v1c_c6_order_observations",
	} {
		if !strings.Contains(read, `"public"."`+required+`"`) {
			t.Fatalf("C6 qualification role omits redacted %s", required)
		}
	}
}

func assertC6RoleIsolation(t *testing.T) {
	t.Helper()
	for _, table := range c6QualificationAppendTables {
		if containsGrantTable(runtimeReadInsertTables, table) ||
			containsGrantTable(v1cEngineReadWriteTables, table) {
			t.Fatalf("non-qualification role can append %s", table)
		}
	}
}

func assertV1CEngineReadOnlyGrants(t *testing.T, role string) {
	t.Helper()
	readOnly := grantSQL("SELECT", v1cEngineReadOnlyTables, role)
	for _, table := range []string{"sessions", "users"} {
		if !strings.Contains(readOnly, `"public"."`+table+`"`) {
			t.Fatalf("V1C engine cannot evaluate authorization safety through %s", table)
		}
	}
	if strings.Contains(
		grantSQL("UPDATE", v1cEngineAlertReadWriteTables,
			role),
		`"public"."audit_events"`,
	) {
		t.Fatal("V1C engine can update immutable alert audit evidence")
	}
}

func assertV1CEngineExcludedGrants(
	t *testing.T,
	role, statement string,
) {
	t.Helper()
	for _, forbidden := range []string{
		"users", "sessions", "journal_transactions", "api_entity_revisions",
		"public_clock_samples", "command_requests",
	} {
		if strings.Contains(statement, `"public"."`+forbidden+`"`) {
			t.Fatalf("V1C engine grant exposes non-execution table %s", forbidden)
		}
	}
}

func TestRoleGrantTablesExistAndAreUnique(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var schema strings.Builder
	for _, migration := range migrations {
		schema.WriteString(strings.ToLower(migration.SQL))
	}
	groups := map[string][]string{
		"runtime read/insert": runtimeReadInsertTables, "runtime update": runtimeUpdateTables,
		"runtime read": runtimeReadTables, "runtime delete": runtimeDeleteTables, "recorder read": recorderReadTables,
		"recorder write": recorderWriteTables, "recorder append": recorderAppendTables,
		"reporting read":              readOnlyTables,
		"v1c engine read/write":       v1cEngineReadWriteTables,
		"v1c engine read":             v1cEngineReadOnlyTables,
		"v1c engine alert read/write": v1cEngineAlertReadWriteTables,
		"v1c engine alert append":     v1cEngineAlertAppendTables,
		"v1c engine runtime append":   v1cEngineRuntimeAppendTables,
		"c6 qualification read":       c6QualificationReadTables,
		"c6 qualification append":     c6QualificationAppendTables,
	}
	for name, tables := range groups {
		seen := make(map[string]struct{}, len(tables))
		for _, table := range tables {
			if _, duplicate := seen[table]; duplicate {
				t.Fatalf("%s repeats %s", name, table)
			}
			seen[table] = struct{}{}
			// The migration ledger is created transactionally by ApplyMigrations
			// before the versioned migration files are executed.
			tableDefined := strings.Contains(
				schema.String(),
				"create table "+table+" (",
			) || strings.Contains(
				schema.String(),
				"create view "+table,
			)
			if table != "schema_migrations" && !tableDefined {
				t.Fatalf("%s references absent table %s", name, table)
			}
		}
	}
}
