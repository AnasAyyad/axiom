package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/authentication"
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

func TestD1HighRiskAuditHashUsesRevisionValueNotPointerIdentity(t *testing.T) {
	leftRevision, rightRevision := int64(17), int64(17)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	base := authentication.HighRiskAudit{
		ID: "audit-d1", ActorUserID: "owner-d1", SessionID: "session-d1",
		Purpose: authentication.PurposeConfigurationActivation,
		Outcome: "authorization_issued", SourceHash: strings.Repeat("a", 64),
		ReasonHash: strings.Repeat("b", 64), Revision: 3, OccurredAt: at,
	}
	left, right := base, base
	left.TargetRevision, right.TargetRevision = &leftRevision, &rightRevision
	if v1cAuditHash("", left) != v1cAuditHash("", right) {
		t.Fatal("D1 high-risk audit hash depends on pointer identity")
	}
	rightRevision++
	if v1cAuditHash("", left) == v1cAuditHash("", right) {
		t.Fatal("D1 high-risk audit hash ignored target revision value")
	}
}
