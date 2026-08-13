package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/authentication"
)

func TestSandboxRuntimeHighRiskAuditChainDoesNotRequireUpdatePrivilege(t *testing.T) {
	if !strings.Contains(sandboxRuntimeHighRiskAuditLockSQL, "pg_advisory_xact_lock") {
		t.Fatal("sandbox runtime high-risk audit append is not transactionally serialized")
	}
	for _, forbidden := range []string{"FOR UPDATE", "FOR NO KEY UPDATE"} {
		if strings.Contains(
			strings.ToUpper(sandboxRuntimePreviousHighRiskAuditHashSQL),
			forbidden,
		) {
			t.Fatalf(
				"sandbox runtime immutable audit lookup requires mutation privilege: %s",
				forbidden,
			)
		}
	}
}

func TestOwnerControlHighRiskAuditHashUsesRevisionValueNotPointerIdentity(t *testing.T) {
	leftRevision, rightRevision := int64(17), int64(17)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	base := authentication.HighRiskAudit{
		ID: "audit-owner_control", ActorUserID: "owner-owner_control", SessionID: "session-owner_control",
		Purpose: authentication.PurposeConfigurationActivation,
		Outcome: "authorization_issued", SourceHash: strings.Repeat("a", 64),
		ReasonHash: strings.Repeat("b", 64), Revision: 3, OccurredAt: at,
	}
	left, right := base, base
	left.TargetRevision, right.TargetRevision = &leftRevision, &rightRevision
	if sandboxRuntimeAuditHash("", left) != sandboxRuntimeAuditHash("", right) {
		t.Fatal("owner control high-risk audit hash depends on pointer identity")
	}
	rightRevision++
	if sandboxRuntimeAuditHash("", left) == sandboxRuntimeAuditHash("", right) {
		t.Fatal("owner control high-risk audit hash ignored target revision value")
	}
}
