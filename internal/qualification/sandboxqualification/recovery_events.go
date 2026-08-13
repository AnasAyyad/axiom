package sandboxQualification

import (
	"context"
	"fmt"
	"time"
)

// RecoveryEventStore is optional so the deterministic runner remains usable
// with small in-memory test stores while the PostgreSQL observer persists the
// immutable recovery lifecycle.
type RecoveryEventStore interface {
	AppendRecoveryEvent(context.Context, RecoveryEvent) error
}

func (runner Runner) appendRecoveryEvents(
	ctx context.Context,
	configuration Config,
	evidence *Evidence,
	sample Sample,
) {
	store, ok := runner.Store.(RecoveryEventStore)
	if !ok {
		return
	}
	seen := make(map[string]struct{}, len(evidence.RecoveryEvents))
	for _, existing := range evidence.RecoveryEvents {
		seen[recoveryEventKey(existing)] = struct{}{}
	}
	for _, account := range sample.Accounts {
		if account.ID == "" || sample.ObservedAt.IsZero() {
			continue
		}
		for _, transition := range recoveryTransitions(account, sample.ObservedAt) {
			if transition.Event == "" || transition.State == "" {
				continue
			}
			event := recoveryEventFromTransition(
				configuration, account, transition, sample.ObservedAt,
			)
			if _, duplicate := seen[recoveryEventKey(event)]; duplicate {
				continue
			}
			if store.AppendRecoveryEvent(ctx, event) != nil {
				appendFailure(evidence, "persistence_failure", sample.ObservedAt)
				continue
			}
			evidence.RecoveryEvents = append(evidence.RecoveryEvents, event)
			seen[recoveryEventKey(event)] = struct{}{}
		}
	}
}

func recoveryTransitions(
	account AccountObservation,
	observedAt time.Time,
) []AccountRecoveryEvent {
	if len(account.RecoveryEvents) > 0 || account.RecoveryEvent == "" {
		return account.RecoveryEvents
	}
	return []AccountRecoveryEvent{{
		Event: account.RecoveryEvent, State: account.RecoveryState,
		IncidentSource: account.IncidentSource,
		FailureKind:    account.FailureKind, CauseCode: account.CauseCode,
		CleanCheckCount:   account.CleanCheckCount,
		RecoveryTimestamp: account.RecoveryTimestamp,
		OccurredAt:        observedAt.UTC(),
	}}
}

func recoveryEventFromTransition(
	configuration Config,
	account AccountObservation,
	transition AccountRecoveryEvent,
	observedAt time.Time,
) RecoveryEvent {
	event := RecoveryEvent{
		RunID: configuration.Identity.RunID, AccountID: account.ID,
		Exchange: account.Exchange, Environment: account.Environment,
		AccountEpoch: account.Epoch, Event: transition.Event,
		State: transition.State,
		IncidentSource: recoveryValue(
			transition.IncidentSource, account.IncidentSource,
		),
		FailureKind:       recoveryValue(transition.FailureKind, account.FailureKind),
		CauseCode:         recoveryValue(transition.CauseCode, account.CauseCode),
		DeadlineAt:        recoveryDeadline(transition, account, observedAt),
		CleanCheckCount:   transition.CleanCheckCount,
		RecoveryTimestamp: transition.RecoveryTimestamp,
		OccurredAt:        recoveryOccurredAt(transition, observedAt),
	}
	event.EvidenceHash = hashValues(
		configuration.Identity.RunID, event.AccountID, event.Exchange,
		event.Environment, fmt.Sprint(event.AccountEpoch),
		event.Event, event.State, event.IncidentSource,
		event.FailureKind, event.CauseCode,
		event.DeadlineAt.Format(time.RFC3339Nano),
		fmt.Sprint(event.CleanCheckCount), event.OccurredAt.Format(time.RFC3339Nano),
	)
	return event
}

func recoveryValue(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func recoveryDeadline(
	transition AccountRecoveryEvent,
	account AccountObservation,
	observedAt time.Time,
) time.Time {
	if !transition.DeadlineAt.IsZero() {
		return transition.DeadlineAt.UTC()
	}
	if account.DeadlineAt != nil {
		return account.DeadlineAt.UTC()
	}
	return observedAt.UTC()
}

func recoveryOccurredAt(
	transition AccountRecoveryEvent,
	observedAt time.Time,
) time.Time {
	if !transition.OccurredAt.IsZero() {
		return transition.OccurredAt.UTC()
	}
	return observedAt.UTC()
}

func recoveryEventKey(event RecoveryEvent) string {
	return event.AccountID + "\x00" + fmt.Sprint(event.AccountEpoch) + "\x00" +
		event.IncidentSource + "\x00" + event.Event + "\x00" +
		fmt.Sprint(event.CleanCheckCount)
}
