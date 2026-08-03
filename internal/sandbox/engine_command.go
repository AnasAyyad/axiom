package sandbox

import "time"

// EngineCommandKind is one credential-owning engine operation requested
// through the durable database boundary.
type EngineCommandKind string

// Closed engine command kinds deliberately exclude order creation.
// Durable command states advance monotonically under the active fence.
const (
	EngineCommandQuery     EngineCommandKind = "QUERY"
	EngineCommandCancel    EngineCommandKind = "CANCEL"
	EngineCommandReconcile EngineCommandKind = "RECONCILE"
)

// EngineCommandState is the bounded durable command lifecycle.
type EngineCommandState string

// Engine command states advance monotonically under the active fence.
const (
	// EngineCommandPending is an unclaimed durable engine command.
	EngineCommandPending EngineCommandState = "PENDING"
	// EngineCommandClaimed is owned by the current fencing lease.
	EngineCommandClaimed EngineCommandState = "CLAIMED"
	// EngineCommandSucceeded records a redacted successful result.
	EngineCommandSucceeded EngineCommandState = "SUCCEEDED"
	// EngineCommandFailed records a redacted failed result.
	EngineCommandFailed EngineCommandState = "FAILED"
)

// EngineCommand contains no credentials or native private payload.
type EngineCommand struct {
	ID            string
	AccountID     AccountID
	AccountEpoch  uint64
	Kind          EngineCommandKind
	ClientOrderID string
	State         EngineCommandState
	RequestedAt   time.Time
}

// Validate checks one exact query, cancel, or reconciliation command.
func (command EngineCommand) Validate() error {
	if command.ID == "" || len(command.ID) > 128 ||
		command.AccountID == "" || command.AccountEpoch == 0 ||
		command.RequestedAt.IsZero() ||
		command.RequestedAt.Location() != time.UTC ||
		(command.State != "" && command.State != EngineCommandPending) {
		return contractError("engine_command_invalid")
	}
	switch command.Kind {
	case EngineCommandQuery, EngineCommandCancel:
		if command.ClientOrderID == "" ||
			len(command.ClientOrderID) > 64 {
			return contractError("engine_command_invalid")
		}
	case EngineCommandReconcile:
		if command.ClientOrderID != "" {
			return contractError("engine_command_invalid")
		}
	default:
		return contractError("engine_command_invalid")
	}
	return nil
}
