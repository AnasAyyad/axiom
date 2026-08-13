package sandbox

import (
	"errors"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestClassifyReconciliationFailureRedactsUntypedError(t *testing.T) {
	kind, cause := ClassifyReconciliationFailure(errors.New("https://secret.invalid/token"))
	if kind != exchangecontracts.ErrorValidation || cause != "untyped_failure" {
		t.Fatalf("untyped failure was not redacted: kind=%s cause=%s", kind, cause)
	}
}

func TestReconciliationRecoveryRequiresTwoSpacedCleanChecks(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	recovery, err := NewReconciliationRecovery(started)
	if err != nil {
		t.Fatal(err)
	}
	detected, err := recovery.ObserveFailure(
		started.Add(time.Second), exchangecontracts.ErrorTransient, "http_503",
	)
	if err != nil || detected.State != RecoveryActive ||
		detected.Event != RecoveryDetected || !recovery.Active() ||
		recovery.DispatchAllowed() {
		t.Fatalf("detected=%+v err=%v", detected, err)
	}
	health := ReconciliationRecoveryHealth{
		StreamHealthy: true, EvidenceHealthy: true, LeaseHeld: true,
		AccountSafe: true, ReconciliationClean: true,
	}
	first, err := recovery.ObserveClean(started.Add(2*time.Second), health)
	if err != nil || first.Event != RecoveryFirstClean || first.CleanCheckCount != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	tooSoon, err := recovery.ObserveClean(started.Add(10*time.Second), health)
	if err != nil || tooSoon.Changed || tooSoon.CleanCheckCount != 1 {
		t.Fatalf("too soon=%+v err=%v", tooSoon, err)
	}
	recovered, err := recovery.ObserveClean(started.Add(32*time.Second), health)
	if err != nil || recovered.State != RecoveryRecovered ||
		recovered.Event != RecoveryRecoveredEvent || !recovery.DispatchAllowed() {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}

func TestPrivateStreamRecoverySharesTheSingleIncidentBudget(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	recovery, err := NewReconciliationRecovery(started)
	if err != nil {
		t.Fatal(err)
	}
	detected, err := recovery.ObserveIncident(
		started.Add(time.Second), RecoverySourcePrivateStream,
		exchangecontracts.ErrorTransient, "private_stream_receive_failed",
	)
	if err != nil || detected.IncidentSource != RecoverySourcePrivateStream ||
		detected.State != RecoveryActive || recovery.DispatchAllowed() {
		t.Fatalf("private stream detection=%+v error=%v", detected, err)
	}
	repeated, err := recovery.ObserveIncident(
		started.Add(2*time.Second), RecoverySourceReconciliation,
		exchangecontracts.ErrorMaintenance, "exchange_maintenance",
	)
	if err != ErrReconciliationRecoveryTerminal ||
		repeated.State != RecoveryRepeated || repeated.Event != RecoveryRepeatedEvent {
		t.Fatalf("cross-source repeat=%+v error=%v", repeated, err)
	}
}

func TestReconciliationRecoveryExpiresAndRejectsSecondIncident(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	recovery, err := NewReconciliationRecovery(started)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recovery.ObserveFailure(
		started, exchangecontracts.ErrorMaintenance, "exchange_maintenance",
	); err != nil {
		t.Fatal(err)
	}
	expired, err := recovery.ObserveClean(
		started.Add(ReconciliationRecoveryDeadline),
		ReconciliationRecoveryHealth{
			StreamHealthy: true, EvidenceHealthy: true, LeaseHeld: true,
			AccountSafe: true, ReconciliationClean: true,
		},
	)
	if err != ErrReconciliationRecoveryTerminal || expired.State != RecoveryExpired ||
		expired.Event != RecoveryExpiredEvent {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	if recovery.DispatchAllowed() {
		t.Fatal("expired recovery enabled dispatch")
	}

	recovery, err = NewReconciliationRecovery(started)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recovery.ObserveFailure(
		started, exchangecontracts.ErrorTransient, "transport_failure",
	); err != nil {
		t.Fatal(err)
	}
	repeated, err := recovery.ObserveFailure(
		started.Add(time.Second), exchangecontracts.ErrorMaintenance, "maintenance",
	)
	if err != ErrReconciliationRecoveryTerminal || repeated.State != RecoveryRepeated ||
		repeated.Event != RecoveryRepeatedEvent {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
}

func TestReconciliationRecoveryRejectsImmediateTerminalClasses(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []exchangecontracts.ErrorKind{
		exchangecontracts.ErrorCapability,
		exchangecontracts.ErrorRateLimit,
		exchangecontracts.ErrorTimestamp,
		exchangecontracts.ErrorFilter,
		exchangecontracts.ErrorInsufficientFunds,
		exchangecontracts.ErrorValidation,
		exchangecontracts.ErrorAmbiguousState,
		exchangecontracts.ErrorCanceled,
	} {
		recovery, err := NewReconciliationRecovery(started)
		if err != nil {
			t.Fatal(err)
		}
		transition, err := recovery.ObserveFailure(started, kind, "stable_failure")
		if err != ErrReconciliationRecoveryTerminal ||
			transition.State != RecoveryUnrecoverable ||
			transition.Event != RecoveryUnrecoverableEvent || recovery.Active() {
			t.Fatalf("kind=%s transition=%+v err=%v", kind, transition, err)
		}
	}
}
