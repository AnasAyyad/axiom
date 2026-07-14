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
		"audit_events", "fills", "inbox_events", "journal_transactions", "ledger_entries", "order_events", "run_results",
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

func TestReadOnlyReportingExcludesCredentialTables(t *testing.T) {
	statement := grantSQL("SELECT", readOnlyTables, `"axiom_readonly"`)
	for _, forbidden := range []string{"users", "sessions", "authorization_roles", "user_roles"} {
		if strings.Contains(statement, `"`+forbidden+`"`) {
			t.Fatalf("reporting grant exposes %s", forbidden)
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
		"runtime delete": runtimeDeleteTables, "recorder read": recorderReadTables,
		"recorder write": recorderWriteTables, "recorder append": recorderAppendTables,
		"reporting read": readOnlyTables,
	}
	for name, tables := range groups {
		seen := make(map[string]struct{}, len(tables))
		for _, table := range tables {
			if _, duplicate := seen[table]; duplicate {
				t.Fatalf("%s repeats %s", name, table)
			}
			seen[table] = struct{}{}
			if !strings.Contains(schema.String(), "create table "+table+" (") {
				t.Fatalf("%s references absent table %s", name, table)
			}
		}
	}
}
