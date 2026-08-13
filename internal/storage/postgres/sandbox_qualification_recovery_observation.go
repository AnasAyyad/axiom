package postgres

import (
	"context"
	"time"

	"axiom/internal/qualification/sandboxqualification"
)

func (store *SandboxQualificationStore) observeSandboxQualificationAccountDetails(
	ctx context.Context,
	now time.Time,
	sample *sandboxQualification.Sample,
) error {
	store.mutex.Lock()
	started := store.started
	store.mutex.Unlock()
	rows, err := store.pool.Query(
		ctx, sandboxQualificationObserveAccountDetailsSQL, now, started,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	accounts := make([]sandboxQualification.AccountObservation, 0, 2)
	allFresh, allLeases, allSafe, active := true, true, true, false
	for rows.Next() {
		runtime, scanErr := scanSandboxQualificationAccountRuntime(rows)
		if scanErr != nil {
			return scanErr
		}
		applySandboxQualificationAccountRecovery(now, &runtime)
		health := sandboxQualificationObservedAccountHealth(now, runtime)
		allFresh = allFresh && health.fresh
		allLeases = allLeases && health.lease
		allSafe = allSafe && health.safe
		active = active || health.active
		accounts = append(accounts, runtime.account)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	sample.Accounts = accounts
	sample.RecoveryActive = active
	sample.AllAccountsFresh = len(accounts) == 2 && allFresh
	sample.AllLeasesHeld = len(accounts) == 2 && allLeases
	sample.EntrySafe = len(accounts) == 2 && allSafe
	return nil
}

func scanSandboxQualificationAccountRuntime(
	scanner sandboxQualificationAccountScanner,
) (sandboxQualificationAccountRuntime, error) {
	var runtime sandboxQualificationAccountRuntime
	err := scanner.Scan(
		&runtime.account.ID, &runtime.account.Exchange, &runtime.account.Environment,
		&runtime.account.Epoch, &runtime.account.State,
		&runtime.account.StreamHealthy, &runtime.account.EvidenceHealthy,
		&runtime.account.LeaseHeld, &runtime.account.ReconciliationClean,
		&runtime.runtimeSucceeded, &runtime.runtimeAt,
		&runtime.first.kind, &runtime.first.failureKind,
		&runtime.first.causeCode, &runtime.first.at,
		&runtime.latest.kind, &runtime.latest.failureKind,
		&runtime.latest.causeCode, &runtime.latest.at,
		&runtime.failureCount, &runtime.hasTerminalFailure,
		&runtime.terminal.kind, &runtime.terminal.failureKind,
		&runtime.terminal.causeCode, &runtime.terminal.at,
		&runtime.reconnectAt, &runtime.firstCleanAt, &runtime.secondCleanAt,
	)
	return runtime, err
}

func applySandboxQualificationAccountRecovery(
	now time.Time,
	runtime *sandboxQualificationAccountRuntime,
) {
	account := &runtime.account
	account.AccountSafe = account.State == "DEGRADED" ||
		account.State == "READY_PAUSED"
	account.RecoveryState = "not_required"
	appendInitialSandboxQualificationRecoveryEvents(runtime)
	switch {
	case runtime.hasTerminalFailure && runtime.failureCount <= 1:
		applyTerminalSandboxQualificationRecovery(now, runtime)
	case runtime.failureCount > 1:
		applyRepeatedSandboxQualificationRecovery(now, runtime)
	case runtime.failureCount == 1 && runtime.first.at != nil:
		applySingleSandboxQualificationRecovery(now, runtime)
	}
	normalizeSandboxQualificationRecoveryHealth(runtime)
}

func appendInitialSandboxQualificationRecoveryEvents(
	runtime *sandboxQualificationAccountRuntime,
) {
	incident := runtime.first
	if incident.at == nil ||
		!permittedSandboxQualificationRecoveryFailure(incident.failureKind) {
		return
	}
	account := &runtime.account
	deadline := incident.at.UTC().Add(2 * time.Minute)
	account.DeadlineAt = &deadline
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			"detected", "active", incident.kind, incident.failureKind,
			incident.causeCode, deadline, 0, incident.at.UTC(), nil,
		),
	)
	if runtime.firstCleanAt == nil {
		return
	}
	account.CleanCheckCount = 1
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			"first_clean_check", "active", incident.kind,
			incident.failureKind, incident.causeCode, deadline, 1,
			runtime.firstCleanAt.UTC(), nil,
		),
	)
}

func applyTerminalSandboxQualificationRecovery(
	now time.Time,
	runtime *sandboxQualificationAccountRuntime,
) {
	state := "unrecoverable"
	if runtime.terminal.causeCode == "recovery_deadline_exceeded" {
		state = "expired"
	}
	appendTerminalSandboxQualificationRecovery(
		now, &runtime.account, state, runtime.terminal,
	)
}

func applyRepeatedSandboxQualificationRecovery(
	now time.Time,
	runtime *sandboxQualificationAccountRuntime,
) {
	appendTerminalSandboxQualificationRecovery(
		now, &runtime.account, "repeated", runtime.latest,
	)
}

func appendTerminalSandboxQualificationRecovery(
	now time.Time,
	account *sandboxQualification.AccountObservation,
	state string,
	incident sandboxQualificationRuntimeIncident,
) {
	account.RecoveryState, account.RecoveryEvent = state, state
	account.IncidentSource = sandboxQualificationRecoverySource(incident.kind)
	account.FailureKind = stableSandboxQualificationFailureKind(incident.failureKind)
	account.CauseCode = stableSandboxQualificationCause(incident.causeCode)
	deadline := sandboxQualificationRecoveryDeadline(account, now, incident.at)
	occurredAt := now.UTC()
	if incident.at != nil {
		occurredAt = incident.at.UTC()
	}
	account.RecoveryEvents = append(account.RecoveryEvents,
		sandboxQualificationAccountRecoveryEvent(
			state, state, incident.kind, account.FailureKind,
			account.CauseCode, deadline, account.CleanCheckCount,
			occurredAt, nil,
		),
	)
}

func sandboxQualificationRecoveryDeadline(
	account *sandboxQualification.AccountObservation,
	now time.Time,
	incidentAt *time.Time,
) time.Time {
	if account.DeadlineAt != nil {
		return account.DeadlineAt.UTC()
	}
	if incidentAt == nil {
		return now.UTC()
	}
	deadline := incidentAt.UTC().Add(2 * time.Minute)
	account.DeadlineAt = &deadline
	return deadline
}
