package postgres

import (
	"strings"
	"testing"
)

func TestSandboxRuntimePlanAccountLockMatchesRuntimePrivileges(t *testing.T) {
	query := strings.ToUpper(strings.TrimSpace(validateSandboxRuntimePlanAccountSQL))
	lockIndex := strings.LastIndex(query, "FOR SHARE")
	if lockIndex < 0 || strings.TrimSpace(query[lockIndex:]) !=
		"FOR SHARE OF ACCOUNT" {
		t.Fatalf("unexpected plan-account locking clause: %s", query)
	}

	updates := grantSQL("UPDATE", runtimeUpdateTables, `"axiom_runtime"`)
	if !strings.Contains(updates, `"public"."sandbox_runtime_exchange_accounts"`) {
		t.Fatal("runtime role cannot lock the mutable exchange-account row")
	}
	if strings.Contains(updates, `"public"."sandbox_runtime_sandbox_session_accounts"`) {
		t.Fatal("runtime role can mutate immutable sandbox-session membership")
	}
}
