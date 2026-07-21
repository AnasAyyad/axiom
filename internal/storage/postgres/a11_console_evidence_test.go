package postgres

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestA11SafeAuditDetailIsBoundedCanonicalAndExcludesSessionSecrets(t *testing.T) {
	command, targetType, targetID := "resume_risk", "risk", "global"
	reason, state := "owner verified recovery", "applied"
	detail := a11SafeAuditDetail(strings.Repeat("a", 64), &command, &targetType, &targetID, &reason, &state,
		`{"state":"NORMAL","real_trading_enabled":false}`)
	if !json.Valid([]byte(detail)) || len(detail) > 2000 || strings.Contains(detail, "session") ||
		!strings.Contains(detail, `"real_trading_enabled":false`) || !strings.Contains(detail, `"target_id":"global"`) {
		t.Fatalf("unsafe audit detail: %q", detail)
	}

	oversized := a11SafeAuditDetail(strings.Repeat("b", 64), &command, &targetType, &targetID, &reason, &state,
		`{"payload":"`+strings.Repeat("x", 3000)+`"}`)
	if !json.Valid([]byte(oversized)) || len(oversized) > 2000 || strings.Contains(oversized, "payload") {
		t.Fatalf("oversized detail was not reduced: %q", oversized)
	}
}
