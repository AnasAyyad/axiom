package postgres

import (
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const sandboxQualificationObservationCutoffDetailsSQL = `
SELECT runtime_at,first_incident_kind,runtime_failure_count FROM (
` + sandboxQualificationObserveAccountDetailsSQL + `
) AS details(
account_id,exchange,environment,account_epoch,account_state,
stream_healthy,evidence_healthy,lease_held,reconciliation_clean,
runtime_succeeded,runtime_at,first_incident_kind,first_failure_kind,
first_cause,first_incident_at,latest_incident_kind,latest_failure_kind,
latest_cause,latest_incident_at,runtime_failure_count,has_terminal_failure,
terminal_kind,terminal_failure_kind,terminal_cause,terminal_at,reconnect_at,
first_clean_at,second_clean_at
) WHERE account_id=$3`

type sandboxQualificationCutoffObservation struct {
	runtimeAt         time.Time
	firstIncidentKind string
	failureCount      int
	reconnects        int64
	duration          int64
	runtimeHealthy    bool
}

func assertSandboxQualificationObservationCutoff(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
) {
	t.Helper()
	observedAt := fixture.now.Add(20 * time.Second)
	priorAt := observedAt.Add(-time.Second)
	recordSandboxQualificationCutoffEvents(t, fixture, observedAt, priorAt)
	observation := observeSandboxQualificationCutoff(t, fixture, observedAt)
	if !observation.runtimeAt.Equal(priorAt) ||
		observation.firstIncidentKind != "" || observation.failureCount != 0 {
		t.Fatalf(
			"sandbox qualification cutoff runtime=%s want=%s incident=%q failures=%d",
			observation.runtimeAt, priorAt, observation.firstIncidentKind,
			observation.failureCount,
		)
	}
	if observation.reconnects != 0 || observation.duration != 10 ||
		!observation.runtimeHealthy {
		t.Fatalf(
			"sandbox qualification runtime cutoff reconnects=%d duration=%d healthy=%t",
			observation.reconnects, observation.duration, observation.runtimeHealthy,
		)
	}
}

func recordSandboxQualificationCutoffEvents(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
	observedAt, priorAt time.Time,
) {
	t.Helper()
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
}

func observeSandboxQualificationCutoff(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
	observedAt time.Time,
) sandboxQualificationCutoffObservation {
	t.Helper()
	var observation sandboxQualificationCutoffObservation
	err := fixture.pool.QueryRow(
		fixture.ctx,
		sandboxQualificationObservationCutoffDetailsSQL,
		observedAt,
		fixture.now,
		fixture.account.AccountID,
	).Scan(
		&observation.runtimeAt, &observation.firstIncidentKind,
		&observation.failureCount,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.pool.QueryRow(
		fixture.ctx,
		sandboxQualificationObserveRuntimeSQL,
		fixture.now,
		observedAt,
	).Scan(
		&observation.reconnects, &observation.duration,
		&observation.runtimeHealthy,
	)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}
