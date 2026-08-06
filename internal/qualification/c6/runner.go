package c6

import (
	"context"
	"fmt"
	"time"
)

// Clock supplies UTC time and cancelable waits to the qualification runner.
type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

// Probe returns one bounded observation for each qualification sample.
type Probe interface {
	Observe(context.Context, uint64, time.Time) (Sample, error)
}

// Store persists immutable run, sample, and terminal evidence.
type Store interface {
	Begin(context.Context, Config, time.Time) error
	AppendSample(context.Context, string, Sample) error
	Finish(context.Context, Evidence) error
}

// ChaosSource returns the deterministic chaos evidence bound to one run.
type ChaosSource interface {
	Events(context.Context, string) ([]ChaosEvent, error)
}

// Runner composes the observation-only C6 clock, probe, store, and chaos
// boundaries.
type Runner struct {
	Clock Clock
	Probe Probe
	Store Store
	Chaos ChaosSource
}

// Run executes one explicitly enabled C6 observation and writes terminal
// evidence without owning exchange credentials or order submission.
func (runner Runner) Run(
	ctx context.Context,
	configuration Config,
) (Evidence, error) {
	evidence, err := runner.start(ctx, configuration)
	if err != nil {
		return Evidence{}, err
	}
	runner.collect(ctx, configuration, &evidence)
	return runner.finish(ctx, configuration, evidence)
}

func (runner Runner) start(
	ctx context.Context,
	configuration Config,
) (Evidence, error) {
	if ValidateConfig(configuration) != nil || runner.Clock == nil ||
		runner.Probe == nil || runner.Store == nil || runner.Chaos == nil {
		return Evidence{}, fmt.Errorf("c6_runner_rejected")
	}
	started := runner.Clock.Now()
	if started.IsZero() || started.Location() != time.UTC ||
		runner.Store.Begin(ctx, configuration, started) != nil {
		return Evidence{}, fmt.Errorf("c6_runner_start_failed")
	}
	return newEvidence(configuration, started), nil
}

func (runner Runner) collect(
	ctx context.Context,
	configuration Config,
	evidence *Evidence,
) {
	var ordinal uint64
	for {
		ordinal++
		observed := runner.Clock.Now()
		sample, err := runner.Probe.Observe(ctx, ordinal, observed)
		if err != nil {
			appendFailure(evidence, "persistence_failure", observed)
			break
		}
		sample.Ordinal = ordinal
		sample.ObservedAt = observed
		evidence.Samples = append(evidence.Samples, sample)
		if runner.Store.AppendSample(
			ctx, configuration.Identity.RunID, sample,
		) != nil {
			appendFailure(evidence, "persistence_failure", observed)
			break
		}
		runner.appendRecoveryEvents(ctx, configuration, evidence, sample)
		evaluateSample(evidence, sample)
		if len(evidence.Failures) > 0 ||
			observed.Sub(evidence.StartedAt).Truncate(time.Second) >=
				configuration.Duration {
			break
		}
		if err = runner.Clock.Wait(ctx, configuration.SampleInterval); err != nil {
			appendFailure(evidence, "operator_abort", runner.Clock.Now())
			break
		}
	}
}

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
		if account.RecoveryEvent == "" || account.ID == "" ||
			sample.ObservedAt.IsZero() {
			continue
		}
		deadline := sample.ObservedAt.UTC()
		if account.DeadlineAt != nil {
			deadline = account.DeadlineAt.UTC()
		}
		event := RecoveryEvent{
			RunID: configuration.Identity.RunID, AccountID: account.ID,
			Exchange: account.Exchange, Environment: account.Environment,
			AccountEpoch: account.Epoch, Event: account.RecoveryEvent,
			State: account.RecoveryState, FailureKind: account.FailureKind,
			CauseCode: account.CauseCode, DeadlineAt: deadline,
			CleanCheckCount:   account.CleanCheckCount,
			RecoveryTimestamp: account.RecoveryTimestamp,
			OccurredAt:        sample.ObservedAt.UTC(),
		}
		event.EvidenceHash = hashValues(
			configuration.Identity.RunID, event.AccountID, event.Exchange,
			event.Event, event.State, event.FailureKind, event.CauseCode,
			event.DeadlineAt.Format(time.RFC3339Nano),
			fmt.Sprint(event.CleanCheckCount), event.OccurredAt.Format(time.RFC3339Nano),
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

func recoveryEventKey(event RecoveryEvent) string {
	return event.AccountID + "\x00" + event.Event + "\x00" +
		fmt.Sprint(event.CleanCheckCount)
}

func (runner Runner) finish(
	ctx context.Context,
	configuration Config,
	evidence Evidence,
) (Evidence, error) {
	evidence.EndedAt = runner.Clock.Now()
	evidence.ObservedDurationSeconds = int64(
		evidence.EndedAt.Sub(evidence.StartedAt).Truncate(time.Second).Seconds(),
	)
	chaosEvents, chaosErr := runner.Chaos.Events(
		ctx, configuration.Identity.RunID,
	)
	evidence.Chaos = chaosEvents
	if chaosErr != nil {
		appendFailure(&evidence, "evidence_failure", evidence.EndedAt)
	}
	evaluateChaos(&evidence)
	evaluateMemory(&evidence)
	finalizeVerdict(&evidence, configuration)
	if err := sealEvidence(configuration, &evidence); err != nil {
		return Evidence{}, err
	}
	if err := runner.Store.Finish(ctx, evidence); err != nil {
		return Evidence{}, fmt.Errorf("c6_runner_finish_failed")
	}
	if evidence.State == StateFailed {
		return evidence, fmt.Errorf("c6_qualification_failed")
	}
	return evidence, nil
}

func sealEvidence(configuration Config, evidence *Evidence) error {
	hash, err := hashEvidence(*evidence)
	if err != nil {
		appendFailure(evidence, "evidence_failure", evidence.EndedAt)
		evidence.State, evidence.Qualified = StateFailed, false
		hash, err = hashEvidence(*evidence)
		if err != nil {
			return fmt.Errorf("c6_evidence_hash_failed")
		}
	}
	evidence.EvidenceHash = hash
	if err = WriteEvidenceNoReplace(configuration.EvidencePath, *evidence); err != nil {
		return fmt.Errorf("c6_evidence_write_failed")
	}
	return nil
}

func newEvidence(configuration Config, started time.Time) Evidence {
	return Evidence{
		SchemaVersion: "axiom.c6.qualification.v1",
		Identity:      configuration.Identity, StartedAt: started,
		RequiredDurationSeconds: int64(configuration.Duration.Seconds()),
		ProfitabilityEvidence:   false, Qualified: false,
		Caps: Caps{
			MaximumOrderMicrounits: 10_000_000,
			MaximumDailyMicrounits: 50_000_000,
			MaximumOpenPerAccount:  1, MaximumOpenGlobal: 2,
			ArmDurationSeconds: 900,
		},
		SLO: SLO{
			CriticalAlertLatencyMillis: uint64(AlertSLO.Milliseconds()),
			RecoveryDurationMillis:     uint64(RecoveryRTO.Milliseconds()),
		},
		Samples: []Sample{}, RecoveryEvents: []RecoveryEvent{},
		Chaos: []ChaosEvent{}, Failures: []Failure{},
	}
}

func evaluateSample(evidence *Evidence, sample Sample) {
	checks := []struct {
		failed bool
		reason string
	}{
		{sample.DuplicateCreates > 0, "duplicate_create"},
		{sample.LostFills > 0, "lost_fill"},
		{sample.DoublePostedFills > 0, "double_posted_fill"},
		{sample.UnknownOrders > 0 && sample.OldestUnknownSeconds >= 30,
			"unresolved_unknown"},
		{sample.ReconciliationMismatches > 0, "reconciliation_mismatch"},
		{sample.SuspenseItems > 0, "suspense"},
		{(!sample.AllAccountsFresh || !sample.EntrySafe) &&
			!sampleAllowsActiveRecovery(sample), "stale_data"},
		{!sample.AllLeasesHeld, "lease_loss"},
		{!sample.PersistenceHealthy, "persistence_failure"},
		{!sample.RestartSafe ||
			sample.RecoveryDurationMillis > uint64(RecoveryRTO.Milliseconds()),
			"unsafe_restart"},
		{sample.ProductionTargetObserved, "production_target"},
		{sample.LargestOrderMicrounits > evidence.Caps.MaximumOrderMicrounits ||
			sample.DailySubmittedMicrounits > evidence.Caps.MaximumDailyMicrounits ||
			sample.MaximumAccountOpen > evidence.Caps.MaximumOpenPerAccount ||
			sample.GlobalOpen > evidence.Caps.MaximumOpenGlobal, "cap_violation"},
		{sample.CriticalAlertLatencyMillis >
			uint64(AlertSLO.Milliseconds()), "critical_alert_slo"},
	}
	for _, check := range checks {
		if check.failed {
			appendFailure(evidence, check.reason, sample.ObservedAt)
		}
	}
	evaluateRecovery(evidence, sample)
}

func sampleAllowsActiveRecovery(sample Sample) bool {
	if !sample.RecoveryActive || len(sample.Accounts) == 0 {
		return false
	}
	active := 0
	for _, account := range sample.Accounts {
		if account.RecoveryState != "active" {
			if (account.RecoveryState != "not_required" &&
				account.RecoveryState != "recovered") ||
				account.State != "READY_PAUSED" ||
				!account.StreamHealthy || !account.EvidenceHealthy ||
				!account.LeaseHeld || !account.AccountSafe ||
				!account.ReconciliationClean {
				return false
			}
			continue
		}
		active++
		if account.State != "DEGRADED" || !account.StreamHealthy ||
			!account.EvidenceHealthy || !account.LeaseHeld || !account.AccountSafe ||
			(account.FailureKind != "transient_outage" && account.FailureKind != "maintenance") ||
			account.DeadlineAt == nil || account.CleanCheckCount > 1 {
			return false
		}
	}
	return active == 1
}

func evaluateRecovery(evidence *Evidence, sample Sample) {
	for _, account := range sample.Accounts {
		switch account.RecoveryState {
		case "expired":
			appendFailure(evidence, "recovery_expired", sample.ObservedAt)
		case "repeated":
			appendFailure(evidence, "recovery_repeated", sample.ObservedAt)
		case "unrecoverable":
			appendFailure(evidence, "recovery_unrecoverable", sample.ObservedAt)
		case "active":
			if !sampleAllowsActiveRecovery(sample) {
				appendFailure(evidence, "recovery_unrecoverable", sample.ObservedAt)
			}
		}
	}
}

func evaluateChaos(evidence *Evidence) {
	if !validateChaos(evidence.Chaos) {
		appendFailure(evidence, "evidence_failure", evidence.EndedAt)
	}
}

func evaluateMemory(evidence *Evidence) {
	if len(evidence.Samples) < 2 {
		return
	}
	first := evidence.Samples[0].ResidentMemoryBytes
	last := evidence.Samples[len(evidence.Samples)-1].ResidentMemoryBytes
	const tolerance = uint64(64 * 1024 * 1024)
	if last > first+tolerance && last > first+first/10 {
		evidence.SLO.PositiveMemoryLeakTrend = true
		appendFailure(evidence, "memory_leak", evidence.EndedAt)
	}
}

func finalizeVerdict(evidence *Evidence, configuration Config) {
	if len(evidence.Failures) > 0 ||
		evidence.ObservedDurationSeconds <
			evidence.RequiredDurationSeconds {
		evidence.State = StateFailed
		evidence.Qualified = false
		return
	}
	if configuration.Identity.Mode == ModeSmoke {
		evidence.State = StateSmokePassed
		evidence.Qualified = false
		return
	}
	evidence.State = StatePassed
	evidence.Qualified = true
}

func appendFailure(evidence *Evidence, reason string, occurredAt time.Time) {
	if !validFailureReason(reason) {
		reason = "evidence_failure"
	}
	for _, existing := range evidence.Failures {
		if existing.Reason == reason {
			return
		}
	}
	evidence.Failures = append(evidence.Failures, Failure{
		Reason: reason, OccurredAt: occurredAt,
		EvidenceHash: hashValues(
			reason, occurredAt.Format(time.RFC3339Nano),
			evidence.Identity.RunID,
		),
	})
}

// RealClock supplies wall-clock UTC time for the explicitly invoked observer.
type RealClock struct{}

// Now returns the current UTC wall-clock instant.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// Wait blocks for the bounded interval or returns when the context is canceled.
func (RealClock) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
