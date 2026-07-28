package sandbox

import (
	"testing"
	"time"
)

func TestHighRiskControlCommandsRequireBoundAuthorizationEvidence(t *testing.T) {
	at := time.Date(2026, 7, 27, 16, 0, 0, 0, time.UTC)
	arm := Arm{
		ID: "arm-1", SessionID: "sandbox-session-1",
		AccountIDs:        []AccountID{"binance-testnet-a"},
		AuthorizationHash: hashText("authorization"),
		ActorUserID:       "owner-1", ActorSessionID: "session-1",
		ReasonHash: hashText("reason"), CreatedAt: at,
		ExpiresAt: at.Add(ArmLifetime), Revision: 1,
	}
	command := ArmCommand{
		Arm: arm, AuthorizationID: "authorization-1",
		SourceHash: hashText("source"), ExpectedSessionRevision: 1,
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("valid arm command rejected: %v", err)
	}
	command.AuthorizationID = ""
	if err := command.Validate(); err == nil {
		t.Fatal("arm without consumed authorization identity accepted")
	}
	unlock := RiskUnlockCommand{
		ID: "unlock-1", AccountID: "binance-testnet-a", ExpectedRevision: 2,
		AuthorizationID: "authorization-2", ActorUserID: "owner-1",
		ActorSessionID: "session-1", SourceHash: hashText("source"),
		ReasonHash: hashText("reason"), ReconciliationID: "reconciliation-1",
		ReconciliationEpoch: 1, Now: at,
	}
	if err := unlock.Validate(); err != nil {
		t.Fatalf("valid unlock rejected: %v", err)
	}
	unlock.ReconciliationID = ""
	if err := unlock.Validate(); err == nil {
		t.Fatal("risk unlock without clean reconciliation identity accepted")
	}
}
