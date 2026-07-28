package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

type deterministicBroker struct {
	mutex     sync.Mutex
	events    map[string]PrivateEvent
	calls     int
	native    int
	now       time.Time
	cancelErr error
}

func (broker *deterministicBroker) Submit(_ context.Context, submission Submission) (PrivateEvent, error) {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	broker.calls++
	if event, exists := broker.events[submission.ClientOrderID]; exists {
		return event, nil
	}
	event := privateOrderEvent(submission, execution.OrderAcknowledged, "NEW", 6, broker.now)
	broker.events[submission.ClientOrderID] = event
	broker.native++
	return event, nil
}

func (broker *deterministicBroker) Cancel(
	_ context.Context,
	_ AccountID,
	_ uint64,
	clientID string,
) (PrivateEvent, error) {
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	if broker.cancelErr != nil {
		return PrivateEvent{}, broker.cancelErr
	}
	event, exists := broker.events[clientID]
	if !exists {
		return PrivateEvent{}, errors.New("not_found")
	}
	event.Identity += "-cancel"
	event.OrderEvent.ID += "-cancel"
	event.OrderEvent.State = execution.OrderCanceled
	event.OrderEvent.ExchangeStatus = "CANCELED"
	event.OrderEvent.Ordinal++
	return event, nil
}

func (*deterministicBroker) Query(context.Context, AccountID, uint64, string) ([]PrivateEvent, error) {
	return nil, nil
}

type crashOnce struct {
	boundary KillBoundary
	hit      bool
}

func (point *crashOnce) Hit(_ context.Context, boundary KillBoundary) error {
	if boundary == point.boundary && !point.hit {
		point.hit = true
		return ErrInjectedCrash
	}
	return nil
}

func TestSandboxPlanCommitKillBoundariesRemainAtomic(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	for _, boundary := range []KillBoundary{KillBeforePlanCommit, KillAfterPlanCommit} {
		t.Run(string(boundary), func(t *testing.T) {
			repository := newMemoryDispatcherRepository()
			plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
			err := repository.ApprovePlan(
				context.Background(), plan, defaultLimits(), &crashOnce{boundary: boundary},
			)
			if !errors.Is(err, ErrInjectedCrash) {
				t.Fatalf("kill result = %v", err)
			}
			wantCommitted := boundary == KillAfterPlanCommit
			if (len(repository.plans) == 1) != wantCommitted ||
				(len(repository.outbox) == 1) != wantCommitted {
				t.Fatalf("partial plan after %s: plans=%d outbox=%d",
					boundary, len(repository.plans), len(repository.outbox))
			}
			if wantCommitted {
				if retryErr := repository.ApprovePlan(
					context.Background(), plan, defaultLimits(), NoKillPoint{},
				); retryErr == nil {
					t.Fatal("committed plan was duplicated")
				}
				if total := repository.dailyReserved[at.Format("2006-01-02")]; total == nil ||
					total.Text('f') != "10" {
					t.Fatalf("daily reservation changed on retry: %v", total)
				}
			}
		})
	}
}

func TestSandboxDispatchKillBoundariesRecoverWithoutNativeDuplicates(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	boundaries := []KillBoundary{
		KillBeforeLeaseTransition, KillAfterLeaseTransition,
		KillBeforeReducerUpdate, KillAfterReducerUpdate,
		KillBeforeNetworkAttempt, KillAfterNetworkAttempt,
		KillBeforeAcknowledgement, KillAfterAcknowledgement,
		KillBeforeInboxAppend, KillAfterInboxAppend,
		KillBeforeInboxCommit, KillAfterInboxCommit,
		KillBeforeReductionCommit, KillAfterReductionCommit,
	}
	for _, boundary := range boundaries {
		t.Run(string(boundary), func(t *testing.T) {
			assertDispatchKillRecovery(t, at, boundary)
		})
	}
}

func assertDispatchKillRecovery(t *testing.T, at time.Time, boundary KillBoundary) {
	t.Helper()
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	first, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker,
		&crashOnce{boundary: boundary},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.DispatchOnce(context.Background(), at, 1); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("kill result = %v", err)
	}
	second, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-b", 2, repository, broker, NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.DispatchOnce(context.Background(), at.Add(31*time.Second), 1); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	assertNonterminalRecovery(t, repository, broker, boundary)
}

func assertNonterminalRecovery(
	t *testing.T,
	repository *memoryDispatcherRepository,
	broker *deterministicBroker,
	boundary KillBoundary,
) {
	t.Helper()
	if broker.native > 1 {
		t.Fatalf("native duplicates after %s: calls=%d native=%d", boundary, broker.calls, broker.native)
	}
	if record := onlyOutbox(t, repository); record.State != OutboxAcknowledged {
		t.Fatalf("outbox after %s = %s", boundary, record.State)
	}
	if repository.reservations["reservation-1-0"].ReleasedAt != nil {
		t.Fatalf("nonterminal reservation released after %s", boundary)
	}
}

func TestSandboxTerminalKillBoundariesRecoverFillAndReservationExactlyOnce(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	boundaries := []KillBoundary{
		KillBeforeInboxAppend, KillAfterInboxAppend,
		KillBeforeInboxCommit, KillAfterInboxCommit,
		KillBeforeReducerUpdate, KillAfterReducerUpdate,
		KillBeforeFillPosting, KillAfterFillPosting,
		KillBeforeReservationRelease, KillAfterReservationRelease,
		KillBeforeReductionCommit, KillAfterReductionCommit,
	}
	for _, boundary := range boundaries {
		t.Run(string(boundary), func(t *testing.T) {
			assertTerminalKillRecovery(t, at, boundary)
		})
	}
}

func assertTerminalKillRecovery(t *testing.T, at time.Time, boundary KillBoundary) {
	t.Helper()
	repository, plan := dispatchedSandboxFixture(t, at)
	fill := privateFillEvent(t, plan.Submissions[0], at.Add(time.Second))
	err := repository.AppendPrivateEvent(
		context.Background(), "", 1, fill, &crashOnce{boundary: boundary},
	)
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("kill result = %v", err)
	}
	if err = repository.AppendPrivateEvent(
		context.Background(), "", 1, fill, NoKillPoint{},
	); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	record := onlyOutbox(t, repository)
	reservation := repository.reservations["reservation-1-0"]
	if record.State != OutboxTerminal || reservation.ReleasedAt == nil ||
		reservation.State != ReservationConsumed ||
		reservation.ReleaseReason != string(execution.OrderFilled) {
		t.Fatalf("terminal recovery after %s = %s/%#v", boundary, record.State, reservation)
	}
	if !repository.reducedEvents[fill.Identity] || len(repository.privateEvents) != 2 {
		t.Fatalf("private events after %s: reduced=%t count=%d",
			boundary, repository.reducedEvents[fill.Identity], len(repository.privateEvents))
	}
	if repository.planStates[plan.ID] != "COMPLETED" {
		t.Fatalf("plan state after %s = %s", boundary, repository.planStates[plan.ID])
	}
}

func dispatchedSandboxFixture(
	t *testing.T,
	at time.Time,
) (*memoryDispatcherRepository, ApprovedSandboxPlan) {
	t.Helper()
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	dispatcher, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count, dispatchErr := dispatcher.DispatchOnce(context.Background(), at, 1); dispatchErr != nil ||
		count != 1 {
		t.Fatalf("acknowledgement = %d, %v", count, dispatchErr)
	}
	return repository, plan
}

func TestSandboxCancelIsDurableAndAvailableAfterArmExpiry(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	dispatcher, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count, dispatchErr := dispatcher.DispatchOnce(
		context.Background(), at, 1,
	); dispatchErr != nil || count != 1 {
		t.Fatalf("dispatch count=%d error=%v", count, dispatchErr)
	}
	if err = dispatcher.Cancel(
		context.Background(),
		plan.Submissions[0].ClientOrderID,
		at.Add(ArmLifetime),
	); err != nil {
		t.Fatalf("post-expiry cancellation failed: %v", err)
	}
	record := onlyOutbox(t, repository)
	order := repository.reducers[record.Submission.OrderID.String()].Snapshot()
	reservation := repository.reservations["reservation-1-0"]
	if record.State != OutboxUnknown ||
		order.State != execution.OrderCanceled ||
		reservation.State != ReservationActive ||
		reservation.ReleasedAt != nil {
		t.Fatalf(
			"cancel state=%s order=%s reservation=%#v",
			record.State,
			order.State,
			reservation,
		)
	}
}

func TestSandboxCancelNetworkCrashRetainsCapacityAndRetries(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	for _, boundary := range []KillBoundary{
		KillBeforeNetworkAttempt,
		KillAfterNetworkAttempt,
	} {
		t.Run(string(boundary), func(t *testing.T) {
			assertSandboxCancelNetworkCrash(t, at, boundary)
		})
	}
}

func assertSandboxCancelNetworkCrash(
	t *testing.T,
	at time.Time,
	boundary KillBoundary,
) {
	t.Helper()
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	dispatcher, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if _, err := dispatcher.DispatchOnce(context.Background(), at, 1); err != nil {
		t.Fatal(err)
	}
	assertCancelCrashAndRetry(t, at, boundary, repository, broker, plan)
}

func assertCancelCrashAndRetry(
	t *testing.T,
	at time.Time,
	boundary KillBoundary,
	repository *memoryDispatcherRepository,
	broker *deterministicBroker,
	plan ApprovedSandboxPlan,
) {
	t.Helper()
	crashing, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker,
		&crashOnce{boundary: boundary},
	)
	clientID := plan.Submissions[0].ClientOrderID
	if err := crashing.Cancel(
		context.Background(), clientID, at.Add(time.Second),
	); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("cancel crash=%v", err)
	}
	if reservation := repository.reservations["reservation-1-0"]; reservation.ReleasedAt != nil {
		t.Fatal("ambiguous cancel released reservation")
	}
	retry, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if err := retry.Cancel(
		context.Background(), clientID, at.Add(2*time.Second),
	); err != nil {
		t.Fatalf("cancel retry failed: %v", err)
	}
	order := repository.reducers[plan.Submissions[0].OrderID.String()].Snapshot()
	if order.State != execution.OrderCanceled {
		t.Fatalf("cancel recovery order=%s", order.State)
	}
}

func TestSandboxAmbiguousCancelIsQuarantinedWithoutCapacityRelease(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	dispatcher, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if _, err := dispatcher.DispatchOnce(context.Background(), at, 1); err != nil {
		t.Fatal(err)
	}
	broker.cancelErr = errors.New("ambiguous_timeout")
	if err := dispatcher.Cancel(
		context.Background(),
		plan.Submissions[0].ClientOrderID,
		at.Add(time.Second),
	); err != nil {
		t.Fatalf("ambiguous cancel was not quarantined: %v", err)
	}
	record := onlyOutbox(t, repository)
	order := repository.reducers[record.Submission.OrderID.String()].Snapshot()
	reservation := repository.reservations["reservation-1-0"]
	if record.State != OutboxUnknown ||
		order.State != execution.OrderUnknown ||
		reservation.State != ReservationActive ||
		reservation.ReleasedAt != nil {
		t.Fatalf(
			"ambiguous cancel state=%s order=%s reservation=%#v",
			record.State,
			order.State,
			reservation,
		)
	}
}

func TestSandboxDispatcherCrashAfterNetworkRecoversWithoutNativeDuplicate(t *testing.T) {
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(context.Background(), plan, defaultLimits(), NoKillPoint{}); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	first, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker,
		&crashOnce{boundary: KillAfterNetworkAttempt},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.DispatchOnce(context.Background(), at, 1); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("expected injected crash: %v", err)
	}
	second, err := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-b", 2, repository, broker, NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := second.DispatchOnce(context.Background(), at.Add(31*time.Second), 1); err != nil || count != 1 {
		t.Fatalf("recovery dispatch = %d, %v", count, err)
	}
	if broker.calls != 2 || broker.native != 1 {
		t.Fatalf("deterministic recovery calls=%d native_orders=%d", broker.calls, broker.native)
	}
}

func TestPairedPlanIsAtomicWhenOpenCapacityRejects(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	accounts := []AccountID{"binance-testnet-a", "bybit-demo-a"}
	first := sandboxPlan(t, at, accounts, []string{"10", "10"})
	setPlanStrategy(t, &first, StrategyCrossExchangeArbitrage)
	if err := repository.ApprovePlan(
		context.Background(), first,
		defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	// Terminal capacity may later free, but the 20 USDT daily reservation does
	// not. A second atomic pair taking the total above 50 must fail wholly.
	second := sandboxPlan(t, at.Add(time.Minute), accounts, []string{"10", "10"})
	setPlanStrategy(t, &second, StrategyCrossExchangeArbitrage)
	second.ID = "execution_plan:plan-2"
	for index := range second.Submissions {
		second.Submissions[index].PlanID = mustPlanID(t, "plan-2")
		second.Submissions[index].OrderID = mustOrderID(t, fmt.Sprintf("order-2-%d", index))
		second.Submissions[index].ClientOrderID = fmt.Sprintf("ax-second-%d", index)
		second.Submissions[index].ApprovedAt = second.ApprovedAt
		second.Reservations[index].ID = fmt.Sprintf("reservation-2-%d", index)
		second.Reservations[index].OrderID = second.Submissions[index].OrderID.String()
	}
	second.ApprovalHash = second.Pipeline.HashFor(second)
	if err := repository.ApprovePlan(context.Background(), second, defaultLimits(), NoKillPoint{}); err == nil {
		t.Fatal("open-order capacity did not fail closed")
	}
	if len(repository.plans) != 1 || len(repository.outbox) != 2 {
		t.Fatal("paired plan partially committed")
	}
}

func TestSandboxStrategyAndPairedVenueTopologyFailClosed(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		accounts []AccountID
		strategy string
	}{
		{name: "paired trend", accounts: []AccountID{"binance-testnet-a", "bybit-demo-a"}, strategy: StrategyTrend},
		{name: "single cross exchange", accounts: []AccountID{"binance-testnet-a"}, strategy: StrategyCrossExchangeArbitrage},
		{name: "paired same venue", accounts: []AccountID{"binance-testnet-a", "binance-testnet-b"}, strategy: StrategyCrossExchangeArbitrage},
		{name: "unknown strategy", accounts: []AccountID{"binance-testnet-a"}, strategy: "rebalance"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryDispatcherRepository()
			notionals := make([]string, len(test.accounts))
			for index := range notionals {
				notionals[index] = "10"
			}
			plan := sandboxPlan(t, at, test.accounts, notionals)
			setPlanStrategy(t, &plan, test.strategy)
			if err := repository.ApprovePlan(
				context.Background(), plan, defaultLimits(), NoKillPoint{},
			); err == nil {
				t.Fatal("unsafe strategy topology accepted")
			}
			if len(repository.outbox) != 0 || len(repository.reservations) != 0 {
				t.Fatal("rejected topology partially committed")
			}
		})
	}
}

func TestTypedCanaryUsesTheSameApprovalPipeline(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 30, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"bybit-demo-a"}, []string{"10"})
	setPlanStrategy(t, &plan, StrategySandboxCanary)
	plan.Pipeline.IntentKind = ApprovalCanaryIntent
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatalf("typed canary rejected: %v", err)
	}
	rejected := sandboxPlan(t, at.Add(time.Minute), []AccountID{"binance-testnet-a"}, []string{"10"})
	reidentifyPlan(t, &rejected, 99)
	setPlanStrategy(t, &rejected, StrategySandboxCanary)
	rejected.Pipeline.IntentKind = ApprovalCanaryIntent
	rejected.Pipeline.RiskApproved = false
	rejected.Pipeline.ObservedAt = rejected.ApprovedAt
	rejected.ApprovalHash = rejected.Pipeline.HashFor(rejected)
	if err := repository.ApprovePlan(
		context.Background(), rejected, defaultLimits(), NoKillPoint{},
	); err == nil {
		t.Fatal("canary bypassed central risk approval")
	}
}

func TestDailyCapReservationNeverRefundsAfterTerminalOrders(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	for index := 1; index <= 5; index++ {
		for id, record := range repository.outbox {
			record.State = OutboxTerminal
			repository.outbox[id] = record
		}
		account := AccountID("binance-testnet-a")
		if index%2 == 0 {
			account = "bybit-demo-a"
		}
		plan := sandboxPlan(t, at.Add(time.Duration(index)*time.Minute), []AccountID{account}, []string{"10"})
		reidentifyPlan(t, &plan, index)
		if err := repository.ApprovePlan(context.Background(), plan, defaultLimits(), NoKillPoint{}); err != nil {
			t.Fatalf("daily order %d rejected: %v", index, err)
		}
	}
	for id, record := range repository.outbox {
		record.State = OutboxTerminal
		repository.outbox[id] = record
	}
	sixth := sandboxPlan(t, at.Add(10*time.Minute), []AccountID{"binance-testnet-a"}, []string{"10"})
	reidentifyPlan(t, &sixth, 6)
	if err := repository.ApprovePlan(context.Background(), sixth, defaultLimits(), NoKillPoint{}); err == nil {
		t.Fatal("daily cap refunded after terminal orders")
	}
	total := repository.dailyReserved[at.Format("2006-01-02")]
	if total == nil || total.Text('f') != "50" {
		t.Fatalf("daily reservation = %v", total)
	}
}

func TestExpiredArmAndIneligibleCombinedSnapshotBlockEntries(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	plan.ApprovedAt = at.Add(ArmLifetime)
	plan.Submissions[0].ApprovedAt = plan.ApprovedAt
	if err := repository.ApprovePlan(context.Background(), plan, defaultLimits(), NoKillPoint{}); err == nil {
		t.Fatal("expired arm accepted")
	}
	plan = sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	snapshot := plan.Eligibility[ExchangeBinance]
	snapshot.Eligible = false
	plan.Eligibility[ExchangeBinance] = snapshot
	if err := repository.ApprovePlan(context.Background(), plan, defaultLimits(), NoKillPoint{}); err == nil {
		t.Fatal("ineligible combined snapshot accepted")
	}
}

func TestDurableEntrySafetyAndArmIdentityAreApprovalHashBound(t *testing.T) {
	at := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	plan := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	safety := plan.EntrySafety["binance-testnet-a"]
	safety.PrivateStreamHealthy = false
	plan.EntrySafety["binance-testnet-a"] = safety
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err == nil {
		t.Fatal("entry with unhealthy private stream was accepted")
	}

	plan = sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	plan.Arm.ActorSessionID = "different-session"
	if err := repository.ApprovePlan(
		context.Background(), plan, defaultLimits(), NoKillPoint{},
	); err == nil {
		t.Fatal("arm identity changed outside the approval hash")
	}
}

func TestArmExpiryBlocksClaimButBoundedRecoveryContinues(t *testing.T) {
	at := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	repository := newMemoryDispatcherRepository()
	entry := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	if err := repository.ApprovePlan(
		context.Background(), entry, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	broker := &deterministicBroker{events: map[string]PrivateEvent{}, now: at}
	dispatcher, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1, repository, broker, NoKillPoint{},
	)
	if count, err := dispatcher.DispatchOnce(
		context.Background(), at.Add(ArmLifetime), 1,
	); err != nil || count != 0 || broker.calls != 0 {
		t.Fatalf("expired entry dispatch count=%d calls=%d error=%v", count, broker.calls, err)
	}

	recoveryRepository := newMemoryDispatcherRepository()
	recovery := sandboxPlan(t, at, []AccountID{"binance-testnet-a"}, []string{"10"})
	recovery.ApprovedAt = at.Add(ArmLifetime)
	recovery.Submissions[0].ApprovedAt = recovery.ApprovedAt
	recovery.Submissions[0].Action = IntentRecovery
	recovery.Pipeline.ObservedAt = recovery.ApprovedAt
	recovery.Eligibility = nil
	recovery.EntrySafety = nil
	recovery.ApprovalHash = recovery.Pipeline.HashFor(recovery)
	if err := recoveryRepository.ApprovePlan(
		context.Background(), recovery, defaultLimits(), NoKillPoint{},
	); err != nil {
		t.Fatalf("bounded recovery rejected after arm expiry: %v", err)
	}
	recoveryBroker := &deterministicBroker{
		events: map[string]PrivateEvent{}, now: recovery.ApprovedAt,
	}
	recoveryDispatcher, _ := NewSandboxDispatcher(
		"binance-testnet-a", 1, "worker-a", 1,
		recoveryRepository, recoveryBroker, NoKillPoint{},
	)
	if count, err := recoveryDispatcher.DispatchOnce(
		context.Background(), recovery.ApprovedAt, 1,
	); err != nil || count != 1 || recoveryBroker.calls != 1 {
		t.Fatalf("recovery dispatch count=%d calls=%d error=%v",
			count, recoveryBroker.calls, err)
	}
}

func sandboxPlan(t *testing.T, at time.Time, accounts []AccountID, notionals []string) ApprovedSandboxPlan {
	t.Helper()
	if len(accounts) != len(notionals) {
		t.Fatal("test input mismatch")
	}
	planID := mustPlanID(t, "plan-1")
	strategyID, _ := domain.NewStrategyID("trend")
	base, _ := domain.ParseAssetSymbol("BTC")
	quote, _ := domain.ParseAssetSymbol("USDT")
	instrument, _ := domain.NewSpotInstrument(base, quote)
	quantity, _ := domain.ParseQuantity("0.001")
	price, _ := domain.ParsePrice("10000")
	submissions, reservations := sandboxPlanLegs(
		t, at, planID, strategyID, instrument, quantity, price, accounts, notionals,
	)
	eligibility := sandboxPlanEligibility(at, accounts)
	entrySafety := sandboxPlanEntrySafety(at, accounts)
	pipeline := ApprovalPipelineEvidence{
		IntentKind: ApprovalStrategyIntent,
		IntentHash: hashText("intent"), AllocatorHash: hashText("allocator"),
		RiskHash: hashText("risk"), PlannerHash: hashText("planner"),
		AssetApprovalHash: hashText("asset-approval"),
		RiskApproved:      true, AssetApproved: true, ObservedAt: at,
	}
	plan := ApprovedSandboxPlan{
		ID: planID.String(), SessionID: "sandbox-session-1", Submissions: submissions,
		Reservations: reservations, Arm: Arm{
			ID: "arm-1", SessionID: "sandbox-session-1",
			AccountIDs:        append([]AccountID(nil), accounts...),
			AuthorizationHash: hashText("authorization"), ActorUserID: "owner-1",
			ActorSessionID: "session-1", ReasonHash: hashText("reason"),
			CreatedAt: at, ExpiresAt: at.Add(ArmLifetime), Revision: 1,
		},
		Eligibility: eligibility, EntrySafety: entrySafety,
		Pipeline: pipeline, ApprovedAt: at,
		ConfigurationID: "cfg-1",
	}
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan
}

func sandboxPlanLegs(
	t *testing.T,
	at time.Time,
	planID domain.ExecutionPlanID,
	strategyID domain.StrategyID,
	instrument domain.Instrument,
	quantity domain.Quantity,
	price domain.Price,
	accounts []AccountID,
	notionals []string,
) ([]Submission, []DurableReservation) {
	t.Helper()
	submissions := make([]Submission, 0, len(accounts))
	reservations := make([]DurableReservation, 0, len(accounts))
	for index, account := range accounts {
		orderID := mustOrderID(t, fmt.Sprintf("order-1-%d", index))
		notional, _ := domain.ParseNotional(notionals[index])
		submission := Submission{
			PlanID: planID, OrderID: orderID, AccountID: account, AccountEpoch: 1,
			ClientOrderID: fmt.Sprintf("ax-client-%d", index), StrategyID: strategyID,
			Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
			Notional: notional, Style: OrderStyleLimitGTC, Action: IntentEntry,
			RequestHash: hashText("request", fmt.Sprint(index)), PolicyHash: hashText("policy"),
			ApprovedAt: at,
		}
		submissions = append(submissions, submission)
		reservations = append(reservations, DurableReservation{
			ID: fmt.Sprintf("reservation-1-%d", index), AccountID: account, AccountEpoch: 1,
			OrderID: orderID.String(), Asset: "USDT", Quantity: notionals[index],
		})
	}
	return submissions, reservations
}

func sandboxPlanEligibility(at time.Time, accounts []AccountID) map[Exchange]EligibilitySnapshot {
	eligibility := map[Exchange]EligibilitySnapshot{}
	for _, account := range accounts {
		exchange := exchangeForAccount(account)
		eligibility[exchange] = EligibilitySnapshot{
			ObservedAt: at, Exchange: string(exchange), Instrument: "BTCUSDT",
			BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true,
		}
	}
	return eligibility
}

func sandboxPlanEntrySafety(
	at time.Time,
	accounts []AccountID,
) map[AccountID]EntrySafetySnapshot {
	safety := make(map[AccountID]EntrySafetySnapshot, len(accounts))
	for _, account := range accounts {
		safety[account] = EntrySafetySnapshot{
			AccountID: account, AccountEpoch: 1, Exchange: exchangeForAccount(account),
			ObservedAt: at, State: EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
			ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true,
		}
	}
	return safety
}

func defaultLimits() SubmissionLimits {
	return SubmissionLimits{
		MaximumOrderNotional: "10", MaximumDailyNotional: "50",
		MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
	}
}

func reidentifyPlan(t *testing.T, plan *ApprovedSandboxPlan, sequence int) {
	t.Helper()
	planID := mustPlanID(t, fmt.Sprintf("plan-%d", sequence))
	plan.ID = planID.String()
	for index := range plan.Submissions {
		orderID := mustOrderID(t, fmt.Sprintf("order-%d-%d", sequence, index))
		plan.Submissions[index].PlanID = planID
		plan.Submissions[index].OrderID = orderID
		plan.Submissions[index].ClientOrderID = fmt.Sprintf("ax-client-%d-%d", sequence, index)
		plan.Reservations[index].ID = fmt.Sprintf("reservation-%d-%d", sequence, index)
		plan.Reservations[index].OrderID = orderID.String()
	}
	plan.ApprovalHash = plan.Pipeline.HashFor(*plan)
}

func setPlanStrategy(t *testing.T, plan *ApprovedSandboxPlan, value string) {
	t.Helper()
	strategy, err := domain.NewStrategyID(value)
	if err != nil {
		t.Fatal(err)
	}
	for index := range plan.Submissions {
		plan.Submissions[index].StrategyID = strategy
	}
	plan.ApprovalHash = plan.Pipeline.HashFor(*plan)
}

func privateOrderEvent(
	submission Submission,
	state execution.OrderState,
	status string,
	ordinal uint64,
	at time.Time,
) PrivateEvent {
	event := orderEvent(submission, state, status, ordinal, at)
	return PrivateEvent{
		Identity:  fmt.Sprintf("%s-%d", submission.ClientOrderID, ordinal),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: PrivateOrderEvent, OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		NativeOrderHash: hashText("native", submission.ClientOrderID), OrderEvent: &event,
		OccurredAt: at, ReceivedAt: at,
	}
}

func privateFillEvent(t *testing.T, submission Submission, at time.Time) PrivateEvent {
	t.Helper()
	fillID, err := domain.NewVirtualFillID("fill-one")
	if err != nil {
		t.Fatal(err)
	}
	feeAsset, _ := domain.ParseAssetSymbol("USDT")
	fee, _ := domain.ParseFee("0.01")
	fill := execution.FillFact{
		ID: fillID, Quantity: submission.Quantity, Price: submission.LimitPrice,
		Fee: fee, FeeAsset: feeAsset, Ordinal: 7,
	}
	event := orderEvent(submission, execution.OrderFilled, "FILLED", 7, at)
	event.CumulativeQuantity = submission.Quantity
	event.Fees = []execution.FeeFact{{Asset: feeAsset, Total: fee}}
	event.Fills = []execution.FillFact{fill}
	return PrivateEvent{
		Identity:  fmt.Sprintf("%s-fill-1", submission.ClientOrderID),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: PrivateFillEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: hashText("native", submission.ClientOrderID),
		NativeFillHash:  hashText("fill", submission.ClientOrderID),
		OrderEvent:      &event, OccurredAt: at, ReceivedAt: at,
	}
}

func onlyOutbox(t *testing.T, repository *memoryDispatcherRepository) SubmissionOutbox {
	t.Helper()
	if len(repository.outbox) != 1 {
		t.Fatalf("outbox size = %d", len(repository.outbox))
	}
	for _, record := range repository.outbox {
		return record
	}
	return SubmissionOutbox{}
}

func hashText(values ...string) string {
	hash := sha256.Sum256([]byte(fmt.Sprint(values)))
	return hex.EncodeToString(hash[:])
}

func mustPlanID(t *testing.T, value string) domain.ExecutionPlanID {
	t.Helper()
	id, err := domain.NewExecutionPlanID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustOrderID(t *testing.T, value string) domain.VirtualOrderID {
	t.Helper()
	id, err := domain.NewVirtualOrderID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
