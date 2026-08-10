package authentication

import (
	"context"

	"strings"
	"time"
)

func validPurpose(purpose AuthorizationPurpose) bool {
	switch purpose {
	case PurposeSandboxArm, PurposeRiskUnlock, PurposeCredentialRotate, PurposeRevokeAllSessions,
		PurposeStrategyConfiguration, PurposeRiskControl, PurposeQualificationStart,
		PurposeConfigurationActivation, PurposeArtifactHold:
		return true
	default:
		return false
	}
}

func permissionForPurpose(purpose AuthorizationPurpose) string {
	switch purpose {
	case PurposeSandboxArm:
		return PermissionSandboxArm
	case PurposeStrategyConfiguration, PurposeConfigurationActivation:
		return "configuration.admin"
	case PurposeRiskControl:
		return "operations.control"
	case PurposeQualificationStart:
		return "qualification.start"
	case PurposeArtifactHold:
		return "artifacts.manage"
	default:
		return PermissionSandboxAdmin
	}
}

func isRevisionBoundPurpose(purpose AuthorizationPurpose) bool {
	return RevisionBoundAuthorizationPurpose(purpose)
}

// RevisionBoundAuthorizationPurpose reports whether a purpose must be tied to
// the exact positive resource revision named by a owner control high-risk command.
func RevisionBoundAuthorizationPurpose(purpose AuthorizationPurpose) bool {
	switch purpose {
	case PurposeStrategyConfiguration, PurposeRiskControl, PurposeQualificationStart,
		PurposeConfigurationActivation, PurposeArtifactHold:
		return true
	default:
		return false
	}
}

func (service *SandboxAuthorizationService) recordRejectedAttempt(
	ctx context.Context,
	principal Principal,
	purpose AuthorizationPurpose,
	source, reason string,
	now time.Time,
) {
	if principal.UserID == "" || principal.SessionID == "" || principal.SessionRevision <= 0 ||
		!validPurpose(purpose) {
		return
	}
	id, err := newIdentifier("audit")
	if err != nil {
		return
	}
	sourceHash, reasonHash := stableHash(strings.TrimSpace(source)), stableHash(strings.TrimSpace(reason))
	_ = service.store.AppendHighRiskAudit(ctx, HighRiskAudit{
		ID: id, ActorUserID: principal.UserID, SessionID: principal.SessionID,
		Purpose: purpose, Outcome: "authorization_rejected", SourceHash: sourceHash,
		ReasonHash: reasonHash, Revision: principal.SessionRevision, OccurredAt: now,
		EventHash: stableHash(id, principal.UserID, principal.SessionID, string(purpose),
			"authorization_rejected", sourceHash, reasonHash, now.Format(time.RFC3339Nano)),
	})
}
