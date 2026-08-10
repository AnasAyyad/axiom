package postgres

import (
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func assertV1CC6ObservationCutoff(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	observedAt := fixture.now.Add(20 * time.Second)
	priorAt := observedAt.Add(-time.Second)
	events := []struct {
		kind        string
		succeeded   bool
		failureKind exchangecontracts.ErrorKind
		causeCode   string
		occurredAt  time.Time
	}{
		{kind: "RECONCILIATION", succeeded: true, occurredAt: priorAt},
		{
			kind: "RECONCILIATION", succeeded: true,
			occurredAt: observedAt.Add(86 * time.Millisecond),
		},
		{
			kind: "PRIVATE_STREAM", succeeded: false,
			failureKind: exchangecontracts.ErrorRateLimit,
			causeCode:   "private_stream_rate_limited",
			occurredAt:  observedAt.Add(87 * time.Millisecond),
		},
	}
	for _, event := range events {
		err := fixture.store.RecordEngineRuntimeRecoveryEvent(
			fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
			fixture.account.Exchange, fixture.fence, event.kind,
			10*time.Millisecond, event.succeeded, event.failureKind,
			event.causeCode, event.occurredAt,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	var runtimeAt time.Time
	var firstIncidentKind string
	var failureCount int
	err := fixture.pool.QueryRow(
		fixture.ctx,
		"SELECT runtime_at,first_incident_kind,runtime_failure_count FROM ("+
			c6ObserveAccountDetailsSQL+
			`) AS details(
account_id,exchange,environment,account_epoch,account_state,
stream_healthy,evidence_healthy,lease_held,reconciliation_clean,
runtime_succeeded,runtime_at,first_incident_kind,first_failure_kind,
first_cause,first_incident_at,latest_incident_kind,latest_failure_kind,
latest_cause,latest_incident_at,runtime_failure_count,has_terminal_failure,
terminal_kind,terminal_failure_kind,terminal_cause,terminal_at,reconnect_at,
first_clean_at,second_clean_at
) WHERE account_id=$3`,
		observedAt,
		fixture.now,
		fixture.account.AccountID,
	).Scan(&runtimeAt, &firstIncidentKind, &failureCount)
	if err != nil {
		t.Fatal(err)
	}
	var reconnects, duration int64
	var runtimeHealthy bool
	err = fixture.pool.QueryRow(
		fixture.ctx,
		c6ObserveRuntimeSQL,
		fixture.now,
		observedAt,
	).Scan(&reconnects, &duration, &runtimeHealthy)
	if err != nil {
		t.Fatal(err)
	}
	if !runtimeAt.Equal(priorAt) || firstIncidentKind != "" || failureCount != 0 {
		t.Fatalf(
			"C6 cutoff runtime=%s want=%s incident=%q failures=%d",
			runtimeAt, priorAt, firstIncidentKind, failureCount,
		)
	}
	if reconnects != 0 || duration != 10 || !runtimeHealthy {
		t.Fatalf("C6 runtime cutoff reconnects=%d duration=%d healthy=%t",
			reconnects, duration, runtimeHealthy)
	}
}
