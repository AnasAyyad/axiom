package d5

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Clock supplies UTC observations and cancelable waits to the runner.
type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

// PreflightChecker obtains one bounded pre-clock readiness snapshot.
type PreflightChecker interface {
	Check(context.Context) (Preflight, error)
}

// Probe obtains one bounded all-subsystem sample.
type Probe interface {
	Observe(context.Context, uint64, time.Time) (Sample, error)
}

// FaultSource obtains the terminal outcomes for the approved drill schedule.
type FaultSource interface {
	Events(context.Context, string) ([]FaultEvent, error)
}

// Store persists no-replace start, sample-chain, and terminal evidence.
type Store interface {
	Begin(context.Context, Config, Preflight, time.Time) error
	AppendSample(context.Context, string, Sample) error
	Finish(context.Context, Evidence) error
}

// Runner executes one non-resumable D5 evidence clock.
type Runner struct {
	Clock     Clock
	Preflight PreflightChecker
	Probe     Probe
	Faults    FaultSource
	Store     Store
}

// Run performs fail-closed preflight before starting the clock, then creates a
// single non-resumable evidence chain. Any failure is terminal.
func (runner Runner) Run(ctx context.Context, configuration Config) (Evidence, error) {
	if ValidateConfig(configuration) != nil || runner.Clock == nil || runner.Preflight == nil ||
		runner.Probe == nil || runner.Faults == nil || runner.Store == nil {
		return Evidence{}, fmt.Errorf("d5_runner_rejected")
	}
	preflight, err := runner.Preflight.Check(ctx)
	if err != nil || validatePreflight(preflight, configuration.Identity.Mode) != nil {
		return Evidence{}, fmt.Errorf("d5_preflight_failed")
	}
	started := runner.Clock.Now()
	if started.IsZero() || started.Location() != time.UTC || started.Before(preflight.CheckedAt) ||
		started.Sub(preflight.CheckedAt) > MaximumPreflightAge ||
		runner.Store.Begin(ctx, configuration, preflight, started) != nil {
		return Evidence{}, fmt.Errorf("d5_runner_start_failed")
	}
	evidence := newEvidence(configuration, preflight, started)
	runner.collect(ctx, configuration, &evidence)
	return runner.finish(ctx, configuration, evidence)
}

func validatePreflight(preflight Preflight, mode Mode) error {
	if preflight.CheckedAt.IsZero() || preflight.CheckedAt.Location() != time.UTC ||
		!preflight.ClockSynchronized || preflight.ClockThresholdMillis != ClockThresholdMillis ||
		preflight.ClockOffsetMillis > preflight.ClockThresholdMillis ||
		!preflight.RouteClockThresholdPassed || !preflight.TLSValid ||
		!preflight.PinnedImageDigests || !preflight.NonRootExecution ||
		!preflight.ResourceLimitsPassed || !preflight.DiskCapacityPassed ||
		!preflight.RemoteBackupIndependent || preflight.BackupAgeSeconds > 24*60*60 ||
		!preflight.CleanRestorePassed || preflight.CleanRestoreDurationSeconds > uint64(CleanRestoreRTO.Seconds()) ||
		!preflight.MarketDataRecoveryPassed ||
		!preflight.SchemaUpgradePassed || !preflight.RollbackForwardFixPassed ||
		!preflight.SBOMPresent || !preflight.SecurityScanPassed ||
		!preflight.ProductionPrivateSubmissionImpossible {
		return fmt.Errorf("d5_preflight_invalid")
	}
	if mode == ModeFormal && !preflight.ReferenceServerApproved {
		return fmt.Errorf("d5_reference_server_unapproved")
	}
	return nil
}

func (runner Runner) collect(ctx context.Context, configuration Config, evidence *Evidence) {
	var ordinal uint64
	for {
		ordinal++
		observedAt := runner.Clock.Now()
		sample, err := runner.Probe.Observe(ctx, ordinal, observedAt)
		if err != nil {
			appendFailure(evidence, "sample_unavailable", observedAt)
			break
		}
		sample.Ordinal, sample.ObservedAt = ordinal, observedAt
		if len(evidence.Samples) > 0 {
			prior := evidence.Samples[len(evidence.Samples)-1]
			if sample.SourceRevision <= prior.SourceRevision {
				appendFailure(evidence, "sample_chain_invalid", observedAt)
				break
			}
			sample.PriorSampleHash = prior.SampleHash
		}
		sample.SampleHash, err = sampleDigest(configuration.Identity.RunID, sample)
		if err != nil || runner.Store.AppendSample(ctx, configuration.Identity.RunID, sample) != nil {
			appendFailure(evidence, "evidence_failure", observedAt)
			break
		}
		evidence.Samples = append(evidence.Samples, sample)
		evaluateSample(evidence, sample)
		if len(evidence.Failures) > 0 || observedAt.Sub(evidence.StartedAt).Truncate(time.Second) >= configuration.Duration {
			break
		}
		if err = runner.Clock.Wait(ctx, configuration.SampleInterval); err != nil {
			appendFailure(evidence, "operator_abort", runner.Clock.Now())
			break
		}
	}
}

func (runner Runner) finish(ctx context.Context, configuration Config, evidence Evidence) (Evidence, error) {
	evidence.EndedAt = runner.Clock.Now()
	evidence.ObservedDurationSeconds = int64(evidence.EndedAt.Sub(evidence.StartedAt).Truncate(time.Second).Seconds())
	faults, err := runner.Faults.Events(ctx, configuration.Identity.RunID)
	if err != nil {
		appendFailure(&evidence, "evidence_failure", evidence.EndedAt)
	}
	evidence.Faults = faults
	evaluateFaults(&evidence)
	evaluateMemory(&evidence)
	finalizeVerdict(&evidence, configuration)
	if err = sealEvidence(configuration.SigningKey, &evidence); err != nil {
		return Evidence{}, err
	}
	if err = runner.Store.Finish(ctx, evidence); err != nil {
		return Evidence{}, fmt.Errorf("d5_runner_finish_failed")
	}
	if evidence.State == StateFailed {
		return evidence, fmt.Errorf("d5_qualification_failed")
	}
	return evidence, nil
}

func newEvidence(configuration Config, preflight Preflight, started time.Time) Evidence {
	return Evidence{SchemaVersion: "axiom.d5.readiness.v1", Identity: configuration.Identity,
		Preflight: preflight, DeclaredLoad: configuration.DeclaredLoad,
		FaultSchedule: configuration.FaultSchedule, State: StateFailed, Qualified: false,
		ProfitabilityEvidence: false, StartedAt: started,
		RequiredDurationSeconds: int64(configuration.Duration.Seconds()),
		Samples:                 []Sample{}, Faults: []FaultEvent{}, Failures: []Failure{}}
}

func evaluateSample(evidence *Evidence, sample Sample) {
	checks := []struct {
		failed bool
		reason string
	}{
		{sample.StaleDecisions > 0, "stale_decision"},
		{sample.UninvalidatedGaps > 0, "uninvalidated_gap"},
		{sample.DuplicateOrders > 0, "duplicate_order"},
		{sample.LostFills > 0, "lost_fill"},
		{sample.DoublePostedFills > 0, "double_posted_fill"},
		{sample.UnbalancedJournals > 0, "unbalanced_journal"},
		{sample.ReplayMismatches > 0, "replay_mismatch"},
		{sample.DecodeBookP99Millis > 10 || sample.StrategyRiskP99Millis > 25 || sample.ResyncP95Millis > 15_000, "latency_slo"},
		{sample.CriticalAlertMillis > uint64(CriticalAlertSLO.Milliseconds()) || sample.ExternalAlertP95Millis > uint64(ExternalAlertSLO.Milliseconds()), "alert_slo"},
		{sample.GracefulShutdownMillis > uint64(GracefulShutdownSLO.Milliseconds()), "shutdown_slo"},
		{sample.ShadowRecoveryMillis > uint64(ShadowRecoveryRTO.Milliseconds()) || sample.SandboxRecoveryMillis > uint64(SandboxRecoveryRTO.Milliseconds()), "recovery_rto"},
		{!sample.DatabaseCommitRPOZero || !sample.RecorderWithinFlushRPO, "rpo_breach"},
		{sample.MemoryLimitBytes == 0 || sample.ResidentMemoryBytes > sample.MemoryLimitBytes || !sample.AllDeclaredLoadHealthy, "resource_limit"},
		{sample.DiskLevel != "NORMAL" && sample.DiskLevel != "HIGH" && sample.DiskLevel != "CRITICAL", "disk_pressure_unsafe"},
		{sample.DiskLevel == "HIGH" && !sample.HeavyJobsRejectedAtHigh, "disk_pressure_unsafe"},
		{sample.DiskLevel == "CRITICAL" && (!sample.RecordingPausedAtCritical || !sample.JournalAuditWritable), "disk_pressure_unsafe"},
		{sample.ProductionTargetObserved, "production_target"},
		{sample.ProhibitedCapabilityObserved, "prohibited_capability"},
	}
	for _, check := range checks {
		if check.failed {
			appendFailure(evidence, check.reason, sample.ObservedAt)
		}
	}
}

func evaluateFaults(evidence *Evidence) {
	passed := make(map[string]bool, len(evidence.Faults))
	expected := make(map[string]FaultSpec, len(evidence.FaultSchedule.Faults))
	for _, fault := range evidence.FaultSchedule.Faults {
		expected[fault.Scenario] = fault
	}
	for _, event := range evidence.Faults {
		spec, known := expected[event.Scenario]
		due := evidence.StartedAt.Add(time.Duration(spec.OffsetSeconds) * time.Second)
		if !known || event.RunID != evidence.Identity.RunID || passed[event.Scenario] || event.State != "passed" ||
			!shaPattern.MatchString(event.EvidenceHash) || event.OccurredAt.IsZero() ||
			event.OccurredAt.Before(due) || event.OccurredAt.After(due.Add(15*time.Minute)) {
			appendFailure(evidence, "fault_schedule_incomplete", evidence.EndedAt)
			continue
		}
		passed[event.Scenario] = true
	}
	for _, expected := range evidence.FaultSchedule.Faults {
		if !passed[expected.Scenario] {
			appendFailure(evidence, "fault_schedule_incomplete", evidence.EndedAt)
		}
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
		appendFailure(evidence, "memory_leak", evidence.EndedAt)
	}
}

func finalizeVerdict(evidence *Evidence, configuration Config) {
	if len(evidence.Failures) > 0 || evidence.ObservedDurationSeconds < evidence.RequiredDurationSeconds {
		evidence.State, evidence.Qualified = StateFailed, false
		return
	}
	if configuration.Identity.Mode == ModeSmoke {
		evidence.State, evidence.Qualified = StateSmokePassed, false
		return
	}
	evidence.State, evidence.Qualified = StatePassed, true
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
	payload := reason + "\x1f" + occurredAt.Format(time.RFC3339Nano) + "\x1f" + evidence.Identity.RunID
	digest := sha256.Sum256([]byte(payload))
	evidence.Failures = append(evidence.Failures, Failure{Reason: reason, OccurredAt: occurredAt,
		EvidenceHash: hex.EncodeToString(digest[:])})
}

func sampleDigest(runID string, sample Sample) (string, error) {
	sample.SampleHash = ""
	payload, err := json.Marshal(struct {
		RunID  string `json:"run_id"`
		Sample Sample `json:"sample"`
	}{runID, sample})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func sealEvidence(key ed25519.PrivateKey, evidence *Evidence) error {
	evidence.EvidenceHash, evidence.SigningKeyFingerprint, evidence.Signature = "", "", ""
	payload, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("d5_evidence_encode_failed")
	}
	digest := sha256.Sum256(payload)
	public := key.Public().(ed25519.PublicKey)
	fingerprint := sha256.Sum256(public)
	evidence.EvidenceHash = hex.EncodeToString(digest[:])
	evidence.SigningKeyFingerprint = hex.EncodeToString(fingerprint[:16])
	evidence.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest[:]))
	return nil
}

// RealClock supplies live UTC time and context-aware waits.
type RealClock struct{}

// Now returns the current UTC instant.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// Wait blocks for one sample interval or until cancellation.
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
