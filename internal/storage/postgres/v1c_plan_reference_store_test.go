package postgres

import (
	"strings"
	"testing"
)

func TestV1CPlanAccountLockMatchesRuntimePrivileges(t *testing.T) {
	query := strings.ToUpper(strings.TrimSpace(validateV1CPlanAccountSQL))
	lockIndex := strings.LastIndex(query, "FOR SHARE")
	if lockIndex < 0 || strings.TrimSpace(query[lockIndex:]) !=
		"FOR SHARE OF ACCOUNT" {
		t.Fatalf("unexpected plan-account locking clause: %s", query)
	}

	updates := grantSQL("UPDATE", runtimeUpdateTables, `"axiom_runtime"`)
	if !strings.Contains(updates, `"public"."v1c_exchange_accounts"`) {
		t.Fatal("runtime role cannot lock the mutable exchange-account row")
	}
	if strings.Contains(updates, `"public"."v1c_sandbox_session_accounts"`) {
		t.Fatal("runtime role can mutate immutable sandbox-session membership")
	}
}
