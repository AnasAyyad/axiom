package sandbox

import (
	"context"
	"time"
)

// ArmCommand binds one exact session revision and consumed sandbox-arm
// authorization to a fixed 15-minute arm.
type ArmCommand struct {
	Arm                     Arm
	AuthorizationID         string
	SourceHash              string
	ExpectedSessionRevision uint64
}

// RiskUnlockCommand binds a consumed risk-unlock authorization to one clean,
// persisted account-epoch reconciliation.
type RiskUnlockCommand struct {
	ID                  string
	AccountID           AccountID
	ExpectedRevision    uint64
	AuthorizationID     string
	ActorUserID         string
	ActorSessionID      string
	SourceHash          string
	ReasonHash          string
	ReconciliationID    string
	ReconciliationEpoch uint64
	Now                 time.Time
}

// SandboxControlStore persists authorization-gated arm and risk-unlock state
// transitions. Cancellation deliberately does not depend on this interface.
type SandboxControlStore interface {
	CreateSandboxArm(context.Context, ArmCommand) (Arm, error)
	RiskUnlock(context.Context, RiskUnlockCommand) error
}

// Validate checks one redacted arm command.
func (command ArmCommand) Validate() error {
	if command.Arm.Validate() != nil || command.AuthorizationID == "" ||
		!recoveryHash(command.SourceHash) || command.ExpectedSessionRevision == 0 {
		return contractError("sandbox_arm_command_invalid")
	}
	return nil
}

// Validate checks one reconciliation-bound risk-unlock command.
func (command RiskUnlockCommand) Validate() error {
	if command.ID == "" || command.AccountID == "" || command.ExpectedRevision == 0 ||
		command.AuthorizationID == "" || command.ActorUserID == "" ||
		command.ActorSessionID == "" || !recoveryHash(command.SourceHash) ||
		!recoveryHash(command.ReasonHash) || command.ReconciliationID == "" ||
		command.ReconciliationEpoch == 0 || command.Now.IsZero() ||
		command.Now.Location() != time.UTC {
		return contractError("risk_unlock_command_invalid")
	}
	return nil
}
