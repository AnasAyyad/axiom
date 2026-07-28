package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"axiom/internal/execution"
)

type recoveryQueryBroker struct {
	events []PrivateEvent
	err    error
}

func (*recoveryQueryBroker) Submit(context.Context, Submission) (PrivateEvent, error) {
	return PrivateEvent{}, errors.New("submit_not_supported")
}

func (*recoveryQueryBroker) Cancel(context.Context, AccountID, uint64, string) (PrivateEvent, error) {
	return PrivateEvent{}, errors.New("cancel_not_supported")
}

func (broker *recoveryQueryBroker) Query(
	context.Context,
	AccountID,
	uint64,
	string,
) ([]PrivateEvent, error) {
	return append([]PrivateEvent(nil), broker.events...), broker.err
}

type staticReconciler struct {
	result ReconciliationResult
	err    error
}

func (reconciler staticReconciler) Reconcile(
	context.Context,
	AccountID,
	uint64,
) (ReconciliationResult, error) {
	return reconciler.result, reconciler.err
}

func TestUnknownRecoveryResolvesOnlyFromDurableOrderFacts(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	repository, plan, outboxID := unknownSandboxFixture(t, at)
	fill := privateFillEvent(t, plan.Submissions[0], at.Add(time.Second))
	harness, err := NewUnknownRecoveryHarness(
		"binance-testnet-a",
		1,
		"worker-a",
		1,
		repository,
		&recoveryQueryBroker{events: []PrivateEvent{fill}},
		staticReconciler{result: cleanRecoveryResult(at.Add(2 * time.Second))},
		NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count, recoverErr := harness.RecoverOnce(
		context.Background(),
		at.Add(3*time.Second),
		1,
	); recoverErr != nil || count != 1 {
		t.Fatalf("recover count=%d error=%v", count, recoverErr)
	}
	if repository.outbox[outboxID].State != OutboxTerminal ||
		repository.reservations["reservation-1-0"].State != ReservationConsumed ||
		repository.planStates[plan.ID] != "COMPLETED" {
		t.Fatalf(
			"resolved state=%s reservation=%s plan=%s",
			repository.outbox[outboxID].State,
			repository.reservations["reservation-1-0"].State,
			repository.planStates[plan.ID],
		)
	}
}

func TestUnknownRecoveryCleanSnapshotWithoutOrderFactKeepsCapacity(t *testing.T) {
	at := time.Date(2026, 7, 27, 13, 0, 0, 0, time.UTC)
	repository, plan, outboxID := unknownSandboxFixture(t, at)
	harness, err := NewUnknownRecoveryHarness(
		"binance-testnet-a",
		1,
		"worker-a",
		1,
		repository,
		&recoveryQueryBroker{},
		staticReconciler{result: cleanRecoveryResult(at.Add(time.Second))},
		NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = harness.RecoverOnce(context.Background(), at.Add(2*time.Second), 1); err != nil {
		t.Fatal(err)
	}
	reservation := repository.reservations["reservation-1-0"]
	if repository.outbox[outboxID].State != OutboxUnknown ||
		reservation.ReleasedAt != nil || reservation.State == ReservationReleased ||
		repository.planStates[plan.ID] != "RECOVERY_REQUIRED" {
		t.Fatalf(
			"clean unresolved state=%s reservation=%#v plan=%s",
			repository.outbox[outboxID].State,
			reservation,
			repository.planStates[plan.ID],
		)
	}
}

func TestUnknownCanceledOrderReleasesOnlyAfterCleanReconciliation(t *testing.T) {
	at := time.Date(2026, 7, 27, 13, 30, 0, 0, time.UTC)
	repository, plan, outboxID := unknownSandboxFixture(t, at)
	canceled := privateOrderEvent(
		plan.Submissions[0],
		execution.OrderCanceled,
		"CANCELED",
		7,
		at.Add(time.Second),
	)
	harness, err := NewUnknownRecoveryHarness(
		"binance-testnet-a",
		1,
		"worker-a",
		1,
		repository,
		&recoveryQueryBroker{events: []PrivateEvent{canceled}},
		staticReconciler{result: cleanRecoveryResult(at.Add(2 * time.Second))},
		NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = harness.RecoverOnce(context.Background(), at.Add(3*time.Second), 1); err != nil {
		t.Fatal(err)
	}
	reservation := repository.reservations["reservation-1-0"]
	if repository.outbox[outboxID].State != OutboxTerminal ||
		reservation.State != ReservationReleased || reservation.ReleasedAt == nil ||
		repository.planStates[plan.ID] != "FAILED" {
		t.Fatalf(
			"reconciled cancel state=%s reservation=%#v plan=%s",
			repository.outbox[outboxID].State,
			reservation,
			repository.planStates[plan.ID],
		)
	}
}

func TestUnknownRecoveryCriticalDifferenceQuarantinesAccountCapacity(t *testing.T) {
	at := time.Date(2026, 7, 27, 14, 0, 0, 0, time.UTC)
	repository, plan, _ := unknownSandboxFixture(t, at)
	harness, err := NewUnknownRecoveryHarness(
		"binance-testnet-a",
		1,
		"worker-a",
		1,
		repository,
		&recoveryQueryBroker{},
		staticReconciler{result: quarantinedRecoveryResult(at.Add(time.Second))},
		NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = harness.RecoverOnce(context.Background(), at.Add(2*time.Second), 1); err != nil {
		t.Fatal(err)
	}
	if repository.reservations["reservation-1-0"].State != ReservationQuarantined ||
		repository.planStates[plan.ID] != "QUARANTINED" {
		t.Fatalf(
			"quarantine reservation=%s plan=%s",
			repository.reservations["reservation-1-0"].State,
			repository.planStates[plan.ID],
		)
	}
}

func unknownSandboxFixture(
	t *testing.T,
	at time.Time,
) (*memoryDispatcherRepository, ApprovedSandboxPlan, string) {
	t.Helper()
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(),
		plan,
		defaultLimits(),
		NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimOutbox(
		context.Background(),
		"binance-testnet-a",
		1,
		"worker-a",
		1,
		at,
		time.Minute,
		1,
		NoKillPoint{},
	)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim count=%d error=%v", len(claimed), err)
	}
	if err = repository.MarkSubmitting(
		context.Background(),
		claimed[0].ID,
		1,
		at,
		NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	if err = repository.MarkUnknown(
		context.Background(),
		claimed[0].ID,
		1,
		at,
		NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	return repository, plan, claimed[0].ID
}

func cleanRecoveryResult(at time.Time) ReconciliationResult {
	return ReconciliationResult{
		ID:           "reconciliation-clean",
		AccountID:    "binance-testnet-a",
		AccountEpoch: 1,
		State:        "clean",
		EvidenceHash: hashText("clean"),
		ReconciledAt: at,
	}
}

func quarantinedRecoveryResult(at time.Time) ReconciliationResult {
	return ReconciliationResult{
		ID:           "reconciliation-quarantined",
		AccountID:    "binance-testnet-a",
		AccountEpoch: 1,
		State:        "quarantined",
		Differences: []Difference{{
			Category:       "balance",
			Classification: "unexplained",
			ExpectedHash:   hashText("expected"),
			ActualHash:     hashText("actual"),
			Critical:       true,
		}},
		EvidenceHash: hashText("quarantined"),
		ReconciledAt: at,
	}
}
