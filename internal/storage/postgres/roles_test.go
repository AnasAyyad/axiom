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
		"sandbox_runtime_high_risk_audit_events",
	} {
		if strings.Contains(updates, `"`+table+`"`) || strings.Contains(deletes, `"`+table+`"`) {
			t.Fatalf("runtime can mutate immutable history table %s", table)
		}
	}
	for _, table := range []string{"execution_leases", "sessions"} {
		if !strings.Contains(deletes, `"public"."`+table+`"`) {
			t.Fatalf("runtime delete grant omits %s", table)
		}
	}
	for _, historical := range []string{
		"authorization_permissions", "authorization_roles", "role_permissions", "user_roles", "owner_console_role_change_events",
	} {
		if strings.Contains(updates, `"`+historical+`"`) || strings.Contains(deletes, `"`+historical+`"`) ||
			containsGrantTable(runtimeReadInsertTables, historical) {
			t.Fatalf("runtime grant can mutate historical authorization table %s", historical)
		}
	}
}

func TestStrategySessionEvaluationTimelineIsAppendOnlyForRuntimeAndEngine(t *testing.T) {
	const table = "sandbox_strategy_session_evaluations"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) ||
		containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime evaluation grants are not append-only")
	}
	if !containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, table) ||
		containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) {
		t.Fatalf("engine evaluation grants are not append-only")
	}
	if !containsGrantTable(readOnlyTables, table) {
		t.Fatalf("reporting role cannot read evaluation timeline")
	}
}

func TestStrategyPlanDecisionEvidenceIsAppendOnlyForRuntimeAndEngine(t *testing.T) {
	const table = "sandbox_runtime_strategy_plan_decisions"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) ||
		containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime strategy-decision grants are not append-only")
	}
	if !containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) ||
		!containsGrantTable(readOnlyTables, table) {
		t.Fatalf("engine/reporting strategy-decision grants are incomplete")
	}
}

func TestStrategyDecisionJournalIsAppendOnlyForRuntimeAndEngine(t *testing.T) {
	const table = "sandbox_strategy_decisions"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) ||
		containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime strategy-decision journal grants are not append-only")
	}
	if !containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, table) ||
		containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) {
		t.Fatalf("engine strategy-decision journal grants are not append-only")
	}
	if !containsGrantTable(readOnlyTables, table) {
		t.Fatalf("reporting role cannot read strategy-decision journal")
	}
}

func TestShadowStrategyDecisionEvidenceIsRuntimeAppendOnlyAndReportable(t *testing.T) {
	const table = "shadow_strategy_decision_evidence"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) || containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime shadow strategy evidence grants are not append-only")
	}
	if !containsGrantTable(readOnlyTables, table) {
		t.Fatal("reporting role cannot read shadow strategy evidence")
	}
}

func TestShadowMultilegExecutionEvidenceIsRuntimeAppendOnlyAndReportable(t *testing.T) {
	const table = "shadow_multileg_execution_evidence"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) || containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime shadow multi-leg evidence grants are not append-only")
	}
	if !containsGrantTable(readOnlyTables, table) {
		t.Fatal("reporting role cannot read shadow multi-leg evidence")
	}
}

func TestShadowCrossExchangeInventoryEvidenceIsRuntimeAppendOnlyAndReportable(t *testing.T) {
	const table = "shadow_cross_exchange_inventory_initializations"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) || containsGrantTable(runtimeDeleteTables, table) {
		t.Fatalf("runtime paired-shadow inventory evidence grants are not append-only")
	}
	if !containsGrantTable(readOnlyTables, table) {
		t.Fatal("reporting role cannot read paired-shadow inventory evidence")
	}
}

func TestSandboxAccountingJournalIsAppendOnlyForEngineAndReadOnlyElsewhere(t *testing.T) {
	for _, table := range []string{"sandbox_accounting_transactions", "sandbox_accounting_entries"} {
		if !containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, table) ||
			containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) ||
			containsGrantTable(runtimeUpdateTables, table) ||
			containsGrantTable(runtimeDeleteTables, table) ||
			!containsGrantTable(runtimeReadTables, table) ||
			!containsGrantTable(readOnlyTables, table) {
			t.Fatalf("sandbox accounting grants are not append-only/read-only for %s", table)
		}
	}
}

func TestSandboxAccountingProjectionsAreEngineMaintainedAndReadOnlyElsewhere(t *testing.T) {
	for _, table := range []string{"sandbox_accounting_positions", "sandbox_accounting_position_fees"} {
		if !containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) ||
			containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, table) ||
			!containsGrantTable(runtimeReadTables, table) ||
			containsGrantTable(runtimeReadInsertTables, table) ||
			containsGrantTable(runtimeUpdateTables, table) ||
			containsGrantTable(runtimeDeleteTables, table) ||
			!containsGrantTable(readOnlyTables, table) {
			t.Fatalf("sandbox accounting projection grants are not engine-owned/read-only for %s", table)
		}
	}
}

func TestStrategyRiskObservationsAreEngineAppendOnlyAndRuntimeReadOnly(t *testing.T) {
	for _, table := range []string{"sandbox_strategy_risk_observations", "sandbox_strategy_risk_valuations"} {
		if !containsGrantTable(runtimeReadTables, table) ||
			containsGrantTable(runtimeReadInsertTables, table) ||
			containsGrantTable(runtimeUpdateTables, table) ||
			containsGrantTable(runtimeDeleteTables, table) {
			t.Fatalf("runtime risk-observation grants are not read-only for %s", table)
		}
		if !containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, table) ||
			containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) {
			t.Fatalf("engine risk-observation grants are not append-only for %s", table)
		}
		if !containsGrantTable(readOnlyTables, table) {
			t.Fatalf("reporting role cannot read risk evidence table %s", table)
		}
	}
	for _, dependency := range []string{"asset_screening_versions", "risk_policies", "risk_policy_limits"} {
		if !containsGrantTable(sandboxRuntimeEngineReadOnlyTables, dependency) {
			t.Fatalf("engine risk-observation dependency grant omits %s", dependency)
		}
	}
	for _, dependency := range []string{"owner_console_storage_pressure_state"} {
		if !containsGrantTable(sandboxRuntimeEngineReadOnlyTables, dependency) {
			t.Fatalf("engine risk projection dependency grant omits %s", dependency)
		}
	}
	if !containsGrantTable(sandboxRuntimeEngineRiskStateAppendTables, "risk_state_events") ||
		containsGrantTable(sandboxRuntimeEngineReadOnlyTables, "risk_state_events") ||
		containsGrantTable(sandboxRuntimeEngineReadWriteTables, "risk_state_events") {
		t.Fatal("engine central-risk transition evidence is not append-only")
	}
	if !containsGrantTable(sandboxRuntimeEngineRiskStateUpdateTables, "api_entity_revisions") ||
		containsGrantTable(sandboxRuntimeEngineReadWriteTables, "api_entity_revisions") ||
		containsGrantTable(sandboxRuntimeEngineRuntimeAppendTables, "api_entity_revisions") {
		t.Fatal("engine central-risk revision received broader than update-only ownership")
	}
}

func TestRuntimeMigrationLedgerGrantIsReadOnly(t *testing.T) {
	read := grantSQL("SELECT", runtimeReadTables, `"axiom_runtime"`)
	write := grantSQL("SELECT, INSERT", runtimeReadInsertTables, `"axiom_runtime"`)
	if !strings.Contains(read, `"public"."schema_migrations"`) || strings.Contains(write, `"schema_migrations"`) {
		t.Fatalf("runtime migration-ledger grants are not read-only: %s / %s", read, write)
	}
}

func TestOwnerAccountBootstrapGrantIsAppendOnly(t *testing.T) {
	const table = "owner_accounts"
	if !containsGrantTable(runtimeReadInsertTables, table) ||
		containsGrantTable(runtimeUpdateTables, table) ||
		containsGrantTable(runtimeDeleteTables, table) {
		t.Fatal("owner account bootstrap grant is not append-only")
	}
}

func TestOwnerControlRuntimeAndReportingGrantsRemainLeastPrivilege(t *testing.T) {
	for _, table := range []string{
		"owner_console_export_artifacts", "owner_console_artifact_holds", "owner_console_artifact_access_events",
		"owner_console_qualification_runs",
	} {
		if !containsGrantTable(runtimeReadInsertTables, table) {
			t.Errorf("owner control runtime append grant omits %s", table)
		}
	}
	for _, table := range []string{
		"owner_console_strategy_controls", "owner_console_risk_controls", "owner_console_export_artifacts",
		"owner_console_artifact_holds", "owner_console_qualification_runs",
	} {
		if !containsGrantTable(runtimeUpdateTables, table) {
			t.Errorf("owner control runtime update grant omits %s", table)
		}
	}
	for _, immutable := range []string{
		"owner_console_activity_projection", "owner_console_reason_catalogue", "owner_console_artifact_access_events",
		"owner_console_role_change_events",
	} {
		if containsGrantTable(runtimeUpdateTables, immutable) ||
			containsGrantTable(runtimeDeleteTables, immutable) {
			t.Errorf("owner control immutable relation %s received mutation privilege", immutable)
		}
		if !containsGrantTable(readOnlyTables, immutable) {
			t.Errorf("owner control reporting grant omits %s", immutable)
		}
	}
}

func TestOperationalEvidenceOperationalEvidenceGrantsRemainLeastPrivilege(t *testing.T) {
	appendOnly := []string{
		"owner_console_incident_events", "owner_console_incident_replay_inputs", "owner_console_incident_alert_links",
		"owner_console_incident_activity_links", "owner_console_incident_resolution_evidence",
		"owner_console_alert_delivery_attempts", "owner_console_alert_escalations",
	}
	for _, table := range appendOnly {
		if !containsGrantTable(runtimeReadInsertTables, table) {
			t.Errorf("operational evidence runtime append grant omits %s", table)
		}
		if containsGrantTable(runtimeUpdateTables, table) ||
			containsGrantTable(runtimeDeleteTables, table) {
			t.Errorf("operational evidence immutable relation %s received mutation privilege", table)
		}
	}
	for _, table := range []string{
		"owner_console_report_schedules", "owner_console_reports", "owner_console_alert_routes", "owner_console_alert_route_tests",
	} {
		if !containsGrantTable(runtimeUpdateTables, table) {
			t.Errorf("operational evidence runtime update grant omits %s", table)
		}
	}
	for _, table := range []string{
		"owner_console_incident_events", "owner_console_incident_replay_inputs", "owner_console_incident_alert_links",
		"owner_console_incident_activity_links", "owner_console_incident_resolution_evidence",
		"owner_console_report_schedules", "owner_console_reports", "owner_console_alert_routes",
		"owner_console_alert_delivery_attempts", "owner_console_alert_escalations", "owner_console_alert_route_tests",
		"owner_console_audit_chain",
	} {
		if !containsGrantTable(readOnlyTables, table) {
			t.Errorf("operational evidence reporting grant omits %s", table)
		}
	}
	if containsGrantTable(runtimeReadInsertTables, "owner_console_audit_chain") ||
		containsGrantTable(runtimeUpdateTables, "owner_console_audit_chain") ||
		containsGrantTable(runtimeDeleteTables, "owner_console_audit_chain") {
		t.Error("operational evidence audit chain received direct mutation privilege")
	}
}

func TestProcessAlertEvidenceGrantsAreNarrow(t *testing.T) {
	if !containsGrantTable(processAlertAppendTables, "owner_console_alert_delivery_attempts") {
		t.Error("process alert append grant omits owner_console_alert_delivery_attempts")
	}
	for _, table := range []string{"owner_console_alert_routes", "owner_console_alert_route_tests"} {
		if !containsGrantTable(processAlertUpdateTables, table) {
			t.Errorf("process alert update grant omits %s", table)
		}
	}
	for _, immutable := range []string{
		"owner_console_alert_delivery_attempts", "owner_console_alert_escalations", "owner_console_audit_chain",
	} {
		if containsGrantTable(processAlertUpdateTables, immutable) {
			t.Errorf("process alert update grant exposes immutable %s", immutable)
		}
	}
}

func TestRuntimeAuthenticatedEvidenceGrantIsReadOnly(t *testing.T) {
	const table = `"public"."sandbox_runtime_authenticated_request_evidence"`
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
		"sandbox_runtime_submission_plans",
		"sandbox_runtime_submission_outbox",
		"sandbox_runtime_exchange_accounts",
		"sandbox_runtime_authenticated_request_evidence",
		"sandbox_runtime_canary_evidence",
		"sandbox_runtime_engine_observations",
		"sandbox_runtime_engine_market_observations",
		"sandbox_runtime_account_leases",
		"sandbox_runtime_engine_startup_evidence",
		"sandbox_runtime_engine_commands",
		"sandbox_runtime_sandbox_sessions",
		"sandbox_runtime_sandbox_session_accounts",
		"sandbox_runtime_sandbox_arms",
	} {
		if !containsGrantTable(read, table) {
			t.Fatalf("runtime post-create canary read omits %s", table)
		}
	}
	for _, table := range []string{
		"sandbox_runtime_canary_evidence",
		"sandbox_runtime_engine_commands",
	} {
		if !containsGrantTable(runtimeReadInsertTables, table) {
			t.Fatalf("runtime post-create canary insert omits %s", table)
		}
	}
	for _, table := range []string{
		"sandbox_runtime_sandbox_sessions",
		"sandbox_runtime_sandbox_arms",
		"sandbox_runtime_exchange_accounts",
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

func TestSandboxRuntimeEngineGrantIncludesClosedExecutionAndAlertTables(t *testing.T) {
	role := `"axiom_binance_testnet_engine"`
	statement := grantSQL(
		"SELECT, INSERT, UPDATE",
		append(
			append([]string(nil), sandboxRuntimeEngineReadWriteTables...),
			sandboxRuntimeEngineAlertReadWriteTables...,
		),
		role,
	)
	if !strings.Contains(
		statement,
		`"public"."sandbox_runtime_authenticated_request_evidence"`,
	) {
		t.Fatal("sandbox runtime engine cannot persist authenticated request evidence")
	}
	for _, table := range sandboxRuntimeEngineAlertReadWriteTables {
		if !strings.Contains(statement, `"public"."`+table+`"`) {
			t.Fatalf("sandbox runtime engine cannot persist operational alert table %s", table)
		}
	}
	appendOnly := grantSQL(
		"SELECT, INSERT",
		append(
			append([]string(nil), sandboxRuntimeEngineAlertAppendTables...),
			sandboxRuntimeEngineRuntimeAppendTables...,
		),
		role,
	)
	if !strings.Contains(appendOnly, `"public"."audit_events"`) {
		t.Fatal("sandbox runtime engine cannot append alert audit evidence")
	}
	if !strings.Contains(
		appendOnly,
		`"public"."sandbox_runtime_engine_runtime_events"`,
	) {
		t.Fatal("sandbox runtime engine cannot append redacted runtime recovery evidence")
	}
	assertSandboxRuntimeEngineReadOnlyGrants(t, role)
	assertSandboxRuntimeEngineExcludedGrants(t, role, statement)
}

func TestSandboxQualificationRoleIsDedicatedAndLeastPrivilege(t *testing.T) {
	read := grantSQL(
		"SELECT",
		sandboxQualificationReadTables,
		`"axiom_sandbox_qualification"`,
	)
	appendOnly := grantSQL(
		"SELECT, INSERT",
		sandboxQualificationAppendTables,
		`"axiom_sandbox_qualification"`,
	)
	assertSandboxQualificationRoleAppendGrants(t, appendOnly)
	assertSandboxQualificationRoleForbiddenGrants(t, read, appendOnly)
	assertSandboxQualificationRoleReadGrants(t, read)
	assertSandboxQualificationRoleIsolation(t)
}

func assertSandboxQualificationRoleAppendGrants(t *testing.T, appendOnly string) {
	t.Helper()
	for _, table := range []string{
		"sandbox_qualification_runs",
		"sandbox_qualification_accounts",
		"sandbox_qualification_samples",
		"sandbox_qualification_failures",
		"sandbox_qualification_chaos_events",
	} {
		if !strings.Contains(
			appendOnly,
			`"public"."`+table+`"`,
		) {
			t.Fatalf("sandbox qualification qualification append grant omits %s", table)
		}
	}
}

func assertSandboxQualificationRoleForbiddenGrants(t *testing.T, read, appendOnly string) {
	t.Helper()
	for _, forbidden := range []string{
		"users",
		"sessions",
		"sandbox_runtime_sandbox_authorizations",
		"sandbox_runtime_credential_generations",
		"sandbox_runtime_private_inbox",
		"sandbox_runtime_exchange_fills",
	} {
		if strings.Contains(read, `"`+forbidden+`"`) ||
			strings.Contains(appendOnly, `"`+forbidden+`"`) {
			t.Fatalf("sandbox qualification qualification role exposes %s", forbidden)
		}
	}
}

func assertSandboxQualificationRoleReadGrants(t *testing.T, read string) {
	t.Helper()
	for _, required := range []string{
		"sandbox_runtime_engine_runtime_events",
		"sandbox_qualification_order_observations",
	} {
		if !strings.Contains(read, `"public"."`+required+`"`) {
			t.Fatalf("sandbox qualification qualification role omits redacted %s", required)
		}
	}
}

func assertSandboxQualificationRoleIsolation(t *testing.T) {
	t.Helper()
	for _, table := range sandboxQualificationAppendTables {
		if containsGrantTable(runtimeReadInsertTables, table) ||
			containsGrantTable(sandboxRuntimeEngineReadWriteTables, table) {
			t.Fatalf("non-qualification role can append %s", table)
		}
	}
}

func assertSandboxRuntimeEngineReadOnlyGrants(t *testing.T, role string) {
	t.Helper()
	readOnly := grantSQL("SELECT", sandboxRuntimeEngineReadOnlyTables, role)
	for _, table := range []string{"sessions", "users"} {
		if !strings.Contains(readOnly, `"public"."`+table+`"`) {
			t.Fatalf("sandbox runtime engine cannot evaluate authorization safety through %s", table)
		}
	}
	if strings.Contains(
		grantSQL("UPDATE", sandboxRuntimeEngineAlertReadWriteTables,
			role),
		`"public"."audit_events"`,
	) {
		t.Fatal("sandbox runtime engine can update immutable alert audit evidence")
	}
}

func assertSandboxRuntimeEngineExcludedGrants(
	t *testing.T,
	role, statement string,
) {
	t.Helper()
	for _, forbidden := range []string{
		"users", "sessions", "journal_transactions", "api_entity_revisions",
		"public_clock_samples", "command_requests",
	} {
		if strings.Contains(statement, `"public"."`+forbidden+`"`) {
			t.Fatalf("sandbox runtime engine grant exposes non-execution table %s", forbidden)
		}
	}
}

func TestRoleGrantTablesExistAndAreUnique(t *testing.T) {
	schemaText := effectiveRoleGrantSchema(t)
	for name, tables := range roleGrantTableGroups() {
		seen := make(map[string]struct{}, len(tables))
		for _, table := range tables {
			if _, duplicate := seen[table]; duplicate {
				t.Fatalf("%s repeats %s", name, table)
			}
			seen[table] = struct{}{}
			tableDefined := strings.Contains(schemaText, "create table "+table+" (") ||
				strings.Contains(schemaText, "create view "+table)
			if table != "schema_migrations" && !tableDefined {
				t.Fatalf("%s references absent table %s", name, table)
			}
		}
	}
}

func effectiveRoleGrantSchema(t *testing.T) string {
	t.Helper()
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	var schema strings.Builder
	for _, migration := range migrations {
		schema.WriteString(strings.ToLower(migration.SQL))
	}
	schemaText := schema.String()
	// The semantic-name compatibility migration renames historical objects in
	// place. Mirror those prefix mappings so grants are checked against the
	// effective post-migration schema without rewriting immutable migration SQL.
	for _, pair := range [][2]string{
		{"v1c_c6_qualification_", "sandbox_qualification_"}, // semantic-naming: historical-reference
		{"v1c_c6_", "sandbox_qualification_"},               // semantic-naming: historical-reference
		{"v1c_", "sandbox_runtime_"},                        // semantic-naming: historical-reference
		{"v1d_", "owner_console_"},                          // semantic-naming: historical-reference
		{"b4_", "triangular_arbitrage_"},                    // semantic-naming: historical-reference
		{"b5_", "cross_exchange_arbitrage_"},                // semantic-naming: historical-reference
		{"b7_", "research_promotion_"},                      // semantic-naming: historical-reference
		{"b8_", "multi_exchange_console_"},                  // semantic-naming: historical-reference
	} {
		schemaText += "\n" + strings.ReplaceAll(schema.String(), pair[0], pair[1])
	}
	return schemaText
}

func roleGrantTableGroups() map[string][]string {
	return map[string][]string{
		"runtime read/insert": runtimeReadInsertTables, "runtime update": runtimeUpdateTables,
		"runtime read": runtimeReadTables, "runtime delete": runtimeDeleteTables, "recorder read": recorderReadTables,
		"recorder write": recorderWriteTables, "recorder append": recorderAppendTables,
		"reporting read":                             readOnlyTables,
		"sandbox_runtime engine read/write":          sandboxRuntimeEngineReadWriteTables,
		"sandbox_runtime engine read":                sandboxRuntimeEngineReadOnlyTables,
		"sandbox_runtime engine alert read/write":    sandboxRuntimeEngineAlertReadWriteTables,
		"sandbox_runtime engine alert append":        sandboxRuntimeEngineAlertAppendTables,
		"sandbox_runtime engine runtime append":      sandboxRuntimeEngineRuntimeAppendTables,
		"process alert append":                       processAlertAppendTables,
		"process alert update":                       processAlertUpdateTables,
		"sandbox_qualification qualification read":   sandboxQualificationReadTables,
		"sandbox_qualification qualification append": sandboxQualificationAppendTables,
	}
}
