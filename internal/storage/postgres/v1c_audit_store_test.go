package postgres

import (
	"strings"
	"testing"
)

func TestV1CHighRiskAuditChainDoesNotRequireUpdatePrivilege(t *testing.T) {
	if !strings.Contains(v1cHighRiskAuditLockSQL, "pg_advisory_xact_lock") {
		t.Fatal("V1C high-risk audit append is not transactionally serialized")
	}
	for _, forbidden := range []string{"FOR UPDATE", "FOR NO KEY UPDATE"} {
		if strings.Contains(
			strings.ToUpper(v1cPreviousHighRiskAuditHashSQL),
			forbidden,
		) {
			t.Fatalf(
				"V1C immutable audit lookup requires mutation privilege: %s",
				forbidden,
			)
		}
	}
}
