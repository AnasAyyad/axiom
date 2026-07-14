package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"axiom/internal/alerting"
	runtimecore "axiom/internal/runtime"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA5DurableAlertFailClosedIntegration(t *testing.T) {
	dsn := os.Getenv("AXIOM_A5_TEST_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A5_TEST_DSN is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_a5_test") {
		t.Fatal("A5 integration requires a dedicated database ending _a5_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if applied, applyErr := ApplyMigrations(ctx, pool); applyErr != nil || applied != len(migrations) {
		t.Fatalf("initial migration = %d/%d, %v", applied, len(migrations), applyErr)
	}
	store, service, first, second, now := triggerA5Alerts(t, ctx, pool)
	assertA5Acknowledgement(t, ctx, pool, service, first, second)
	assertA5FailedDelivery(t, ctx, pool, store, second, now)
}

func triggerA5Alerts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*AlertStore, *alerting.Service, alerting.Alert, alerting.Alert, time.Time) {
	t.Helper()
	store, err := NewAlertStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	gate := runtimecore.NewSafetyGate()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	service, err := alerting.NewService(store, nil, gate, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	fault := alerting.Fault{
		Severity: alerting.SeverityCritical, Reason: alerting.ReasonFencingLeaseLost,
		Component: "engine-shadow", CorrelationID: "integration-1", OccurredAt: now,
	}
	first, err := service.Trigger(ctx, fault)
	if err != nil {
		t.Fatal(err)
	}
	fault.CorrelationID, fault.OccurredAt = "integration-2", now.Add(time.Second)
	second, err := service.Trigger(ctx, fault)
	if err != nil {
		t.Fatal(err)
	}
	state, reason := gate.State()
	if state != runtimecore.StateLocked || reason != string(alerting.ReasonFencingLeaseLost) || first.ID != second.ID || second.Occurrences != 2 {
		t.Fatalf("fail-closed/dedup state = %s %s %#v %#v", state, reason, first, second)
	}
	return store, service, first, second, now
}

func assertA5Acknowledgement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *alerting.Service, first, second alerting.Alert) {
	t.Helper()
	acknowledged, err := service.Acknowledge(ctx, first.ID, "operator-a", alerting.AcknowledgementInvestigated)
	if err != nil || acknowledged.Revision <= second.Revision {
		t.Fatalf("acknowledgement = %#v %v", acknowledged, err)
	}
	var alertState string
	var acknowledgements int
	var audits int
	if err = pool.QueryRow(ctx, "SELECT state FROM alerts WHERE id=$1", first.ID).Scan(&alertState); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM alert_acknowledgements WHERE alert_id=$1", first.ID).Scan(&acknowledgements); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE causation_id=$1", first.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if alertState != "acknowledged" || acknowledgements != 1 || audits != 3 {
		t.Fatalf("durable state = %s/%d/%d", alertState, acknowledgements, audits)
	}
}

func assertA5FailedDelivery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *AlertStore, alert alerting.Alert, now time.Time) {
	t.Helper()
	deliveryID, err := store.PrepareDelivery(ctx, alert, "webhook", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteDelivery(ctx, deliveryID, false, "sink_unavailable", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	var deliveryState string
	var attempts int
	if err = pool.QueryRow(ctx, "SELECT state,attempts FROM alert_deliveries WHERE id=$1", deliveryID).Scan(&deliveryState, &attempts); err != nil {
		t.Fatal(err)
	}
	if deliveryState != "failed" || attempts != 1 {
		t.Fatalf("delivery = %s/%d", deliveryState, attempts)
	}
}
