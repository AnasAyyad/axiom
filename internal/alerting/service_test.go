package alerting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	alerts        map[string]Alert
	fail          bool
	deliveryState string
	pending       PendingDelivery
}

func (store *memoryStore) Upsert(_ context.Context, alert Alert) (Alert, error) {
	if store.fail {
		return Alert{}, errors.New("fixture storage detail")
	}
	if store.alerts == nil {
		store.alerts = map[string]Alert{}
	}
	if current, found := store.alerts[alert.DeduplicationKey]; found {
		current.Occurrences++
		current.Revision++
		current.LastSeenAt = alert.LastSeenAt
		current.CorrelationID = alert.CorrelationID
		store.alerts[alert.DeduplicationKey] = current
		return current, nil
	}
	store.alerts[alert.DeduplicationKey] = alert
	return alert, nil
}
func (store *memoryStore) Acknowledge(_ context.Context, id, _, _ string, _ time.Time) (Alert, error) {
	for _, alert := range store.alerts {
		if alert.ID == id {
			alert.Revision++
			return alert, nil
		}
	}
	return Alert{}, errors.New("missing")
}
func (store *memoryStore) PrepareDelivery(_ context.Context, alert Alert, _ string, _ time.Time) (string, error) {
	store.deliveryState = "pending"
	store.pending = PendingDelivery{ID: "delivery_1", SinkName: "webhook", Alert: alert}
	return "delivery_1", nil
}
func (store *memoryStore) CompleteDelivery(_ context.Context, _ string, delivered bool, _ string, _ time.Time) error {
	if delivered {
		store.deliveryState = "delivered"
	} else {
		store.deliveryState = "failed"
	}
	return nil
}
func (store *memoryStore) DueDeliveries(_ context.Context, _ time.Time, _ int32) ([]PendingDelivery, error) {
	if store.deliveryState == "failed" || store.deliveryState == "pending" {
		return []PendingDelivery{store.pending}, nil
	}
	return nil, nil
}

type testGate struct{ reason string }

func (gate *testGate) Lock(reason string) { gate.reason = reason }

type testSink struct{ err error }

func (testSink) Name() string                              { return "webhook" }
func (sink testSink) Deliver(context.Context, Alert) error { return sink.err }

type recoveringSink struct{ failures int }

func (*recoveringSink) Name() string { return "webhook" }
func (sink *recoveringSink) Deliver(context.Context, Alert) error {
	if sink.failures > 0 {
		sink.failures--
		return errors.New("fixture")
	}
	return nil
}

type objectiveStore struct {
	memoryStore
	record func()
}

func (store *objectiveStore) Upsert(ctx context.Context, alert Alert) (Alert, error) {
	stored, err := store.memoryStore.Upsert(ctx, alert)
	if err == nil {
		store.record()
	}
	return stored, err
}

type objectiveDurations struct {
	mu       sync.Mutex
	started  time.Time
	inApp    []time.Duration
	external []time.Duration
}

func (durations *objectiveDurations) begin() {
	durations.mu.Lock()
	durations.started = time.Now()
	durations.mu.Unlock()
}

func (durations *objectiveDurations) record(destination *[]time.Duration) {
	durations.mu.Lock()
	*destination = append(*destination, time.Since(durations.started))
	durations.mu.Unlock()
}

func criticalFault(now time.Time) Fault {
	return Fault{Severity: SeverityCritical, Reason: ReasonPersistenceFailure, Component: "postgres", CorrelationID: "correlation-1", OccurredAt: now}
}

func TestCriticalFaultLocksBeforePersistenceAndDeduplicates(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	store, gate := &memoryStore{}, &testGate{}
	service, err := NewService(store, nil, gate, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Trigger(context.Background(), criticalFault(now))
	if err != nil {
		t.Fatal(err)
	}
	secondFault := criticalFault(now.Add(time.Second))
	secondFault.CorrelationID = "correlation-2"
	second, err := service.Trigger(context.Background(), secondFault)
	if err != nil {
		t.Fatal(err)
	}
	if gate.reason != string(ReasonPersistenceFailure) || first.ID != second.ID || second.Occurrences != 2 || second.Revision != 2 {
		t.Fatalf("unsafe alert state: gate=%q first=%#v second=%#v", gate.reason, first, second)
	}
}

func TestCriticalFaultRemainsLockedWhenAlertPersistenceFails(t *testing.T) {
	store, gate := &memoryStore{fail: true}, &testGate{}
	service, _ := NewService(store, nil, gate, nil)
	_, err := service.Trigger(context.Background(), criticalFault(time.Now().UTC()))
	if err == nil || gate.reason != string(ReasonPersistenceFailure) {
		t.Fatalf("failure did not lock: %v %q", err, gate.reason)
	}
}

func TestCriticalReasonCannotBeDowngraded(t *testing.T) {
	store, gate := &memoryStore{}, &testGate{}
	service, _ := NewService(store, nil, gate, nil)
	fault := criticalFault(time.Now().UTC())
	fault.Severity = SeverityWarning
	if _, err := service.Trigger(context.Background(), fault); err == nil || gate.reason != "" {
		t.Fatal("downgraded critical fault accepted")
	}
}

func TestExternalDeliveryFailureIsDurablyRetained(t *testing.T) {
	store, gate := &memoryStore{}, &testGate{}
	service, _ := NewService(store, testSink{err: errors.New("fixture")}, gate, nil)
	_, err := service.Trigger(context.Background(), criticalFault(time.Now().UTC()))
	if err == nil || store.deliveryState != "failed" {
		t.Fatalf("delivery state = %q, err=%v", store.deliveryState, err)
	}
}

func TestEveryCriticalOperationalReasonFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	for _, reason := range failClosedReasons {
		t.Run(string(reason), func(t *testing.T) {
			store, gate := &memoryStore{}, &testGate{}
			service, err := NewService(store, nil, gate, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Trigger(context.Background(), Fault{
				Severity: SeverityCritical, Reason: reason, Component: "safety-monitor",
				CorrelationID: "fault-matrix-1", OccurredAt: now,
			})
			if err != nil || gate.reason != string(reason) || len(store.alerts) != 1 {
				t.Fatalf("reason did not alert and lock: %v %q %d", err, gate.reason, len(store.alerts))
			}
		})
	}
}

func TestFailedExternalDeliveryRetriesFromDurableState(t *testing.T) {
	store, gate, sink := &memoryStore{}, &testGate{}, &recoveringSink{failures: 1}
	service, _ := NewService(store, sink, gate, nil)
	if _, err := service.Trigger(context.Background(), criticalFault(time.Now().UTC())); err == nil || store.deliveryState != "failed" {
		t.Fatalf("initial delivery was not retained: %v %s", err, store.deliveryState)
	}
	delivered, err := service.RetryDue(context.Background(), 10)
	if err != nil || delivered != 1 || store.deliveryState != "delivered" {
		t.Fatalf("retry = %d %v %s", delivered, err, store.deliveryState)
	}
}

func TestAlertDeliveryServiceObjectivesAtDeclaredLocalLoad(t *testing.T) {
	durations := &objectiveDurations{}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		durations.record(&durations.external)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	sink, err := NewWebhookSink(
		"https://alerts.example.invalid/v1/axiom", "fixture-bearer-token",
		[]string{"alerts.example.invalid"}, client,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &objectiveStore{record: func() { durations.record(&durations.inApp) }}
	service, err := NewService(store, sink, &testGate{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 100; index++ {
		durations.begin()
		_, err = service.Trigger(context.Background(), Fault{
			Severity: SeverityCritical, Reason: ReasonPersistenceFailure,
			Component: "postgres", CorrelationID: "objective-local",
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(durations.inApp) != 100 || len(durations.external) != 100 {
		t.Fatalf("incomplete measurements: in-app=%d external=%d", len(durations.inApp), len(durations.external))
	}
	inAppP95, externalP95 := percentile95(durations.inApp), percentile95(durations.external)
	t.Logf("declared local load: samples=100 in_app_p95=%s external_https_p95=%s", inAppP95, externalP95)
	if inAppP95 > 5*time.Second {
		t.Fatalf("in-app p95 %s exceeds 5s objective", inAppP95)
	}
	if externalP95 > 60*time.Second {
		t.Fatalf("external p95 %s exceeds 60s objective", externalP95)
	}
}

func percentile95(samples []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	return ordered[(len(ordered)*95+99)/100-1]
}
