package postgres

import (
	"time"

	"axiom/internal/qualification/sandboxqualification"
)

func applySingleSandboxQualificationRecovery(
	now time.Time,
	runtime *sandboxQualificationAccountRuntime,
) {
	account := &runtime.account
	incident := runtime.first
	account.IncidentSource = sandboxQualificationRecoverySource(incident.kind)
	account.FailureKind = stableSandboxQualificationFailureKind(incident.failureKind)
	account.CauseCode = stableSandboxQualificationCause(incident.causeCode)
	deadline := incident.at.UTC().Add(2 * time.Minute)
	account.DeadlineAt = &deadline
	streamRecovered := account.StreamHealthy &&
		(account.IncidentSource != "private_stream" || runtime.reconnectAt != nil)
	switch {
	case runtime.secondCleanAt != nil && account.State == "READY_PAUSED" &&
		streamRecovered && account.EvidenceHealthy && account.LeaseHeld &&
		account.AccountSafe && runtime.runtimeSucceeded:
		applyRecoveredSandboxQualificationAccount(runtime, deadline)
	case !now.Before(deadline):
		applyExpiredSandboxQualificationAccount(runtime, deadline)
	case account.State == "DEGRADED":
		account.RecoveryState, account.RecoveryEvent = "active", "detected"
		if runtime.firstCleanAt != nil {
			account.RecoveryEvent, account.CleanCheckCount = "first_clean_check", 1
		}
	default:
		applyUnrecoverableSandboxQualificationAccount(now, runtime, deadline)
	}
}

func applyRecoveredSandboxQualificationAccount(
	runtime *sandboxQualificationAccountRuntime,
	deadline time.Time,
) {
	account := &runtime.account
	account.RecoveryState, account.RecoveryEvent = "recovered", "recovered"
	account.CleanCheckCount = 2
	recoveredAt := runtime.secondCleanAt.UTC()
	account.RecoveryTimestamp = &recoveredAt
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			"recovered", "recovered", runtime.first.kind,
			account.FailureKind, account.CauseCode, deadline, 2,
			recoveredAt, &recoveredAt,
		),
	)
}

func applyExpiredSandboxQualificationAccount(
	runtime *sandboxQualificationAccountRuntime,
	deadline time.Time,
) {
	account := &runtime.account
	account.RecoveryState, account.RecoveryEvent = "expired", "expired"
	if runtime.firstCleanAt != nil {
		account.CleanCheckCount = 1
	}
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			"expired", "expired", runtime.first.kind,
			account.FailureKind, "recovery_deadline_exceeded",
			deadline, account.CleanCheckCount, deadline, nil,
		),
	)
}

func applyUnrecoverableSandboxQualificationAccount(
	now time.Time,
	runtime *sandboxQualificationAccountRuntime,
	deadline time.Time,
) {
	account := &runtime.account
	account.RecoveryState, account.RecoveryEvent = "unrecoverable", "unrecoverable"
	account.FailureKind = "validation_rejected"
	account.CauseCode = "recovery_state_not_degraded"
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			"unrecoverable", "unrecoverable", runtime.first.kind,
			account.FailureKind, account.CauseCode, deadline,
			account.CleanCheckCount, now.UTC(), nil,
		),
	)
}

func normalizeSandboxQualificationRecoveryHealth(
	runtime *sandboxQualificationAccountRuntime,
) {
	account := &runtime.account
	if account.IncidentSource == "private_stream" &&
		account.RecoveryState == "active" {
		account.StreamHealthy = runtime.reconnectAt != nil
	}
	if account.RecoveryState == "active" ||
		account.RecoveryState == "expired" ||
		account.RecoveryState == "repeated" ||
		account.RecoveryState == "unrecoverable" {
		account.ReconciliationClean = false
	}
}

func sandboxQualificationObservedAccountHealth(
	now time.Time,
	runtime sandboxQualificationAccountRuntime,
) sandboxQualificationAccountHealth {
	account := runtime.account
	streamAllowed := account.StreamHealthy ||
		(account.IncidentSource == "private_stream" && account.CleanCheckCount == 0)
	beforeDeadline := account.DeadlineAt != nil && now.Before(account.DeadlineAt.UTC())
	active := account.RecoveryState == "active" && account.State == "DEGRADED" &&
		streamAllowed && account.EvidenceHealthy && account.LeaseHeld &&
		account.AccountSafe && beforeDeadline &&
		permittedSandboxQualificationRecoveryFailure(account.FailureKind)
	fresh := account.State == "READY_PAUSED" && account.StreamHealthy &&
		account.EvidenceHealthy && account.LeaseHeld && account.ReconciliationClean &&
		runtime.runtimeSucceeded && runtime.runtimeAt != nil &&
		!runtime.runtimeAt.After(now) && now.Sub(runtime.runtimeAt.UTC()) <= 2*time.Minute
	return sandboxQualificationAccountHealth{
		fresh: fresh || active,
		lease: account.LeaseHeld,
		safe: account.AccountSafe && account.StreamHealthy &&
			account.EvidenceHealthy && account.LeaseHeld,
		active: active,
	}
}

func sandboxQualificationRecoverySource(runtimeKind string) string {
	if runtimeKind == "PRIVATE_STREAM" || runtimeKind == "PRIVATE_RECONNECT" {
		return "private_stream"
	}
	return "reconciliation"
}

func permittedSandboxQualificationRecoveryFailure(kind string) bool {
	return kind == "transient_outage" || kind == "maintenance"
}

func stableSandboxQualificationFailureKind(kind string) string {
	if kind == "" {
		return "validation_rejected"
	}
	return kind
}

func stableSandboxQualificationCause(cause string) string {
	if cause == "" {
		return "untyped_failure"
	}
	return cause
}

func sandboxQualificationAccountRecoveryEvent(
	event, state, runtimeKind, failureKind, causeCode string,
	deadline time.Time,
	cleanChecks uint8,
	occurredAt time.Time,
	recoveryTimestamp *time.Time,
) sandboxQualification.AccountRecoveryEvent {
	return sandboxQualification.AccountRecoveryEvent{
		Event: event, State: state,
		IncidentSource: sandboxQualificationRecoverySource(runtimeKind),
		FailureKind:    stableSandboxQualificationFailureKind(failureKind),
		CauseCode:      stableSandboxQualificationCause(causeCode), DeadlineAt: deadline.UTC(),
		CleanCheckCount: cleanChecks, RecoveryTimestamp: recoveryTimestamp,
		OccurredAt: occurredAt.UTC(),
	}
}
