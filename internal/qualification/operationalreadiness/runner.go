package operationalReadiness

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

// Runner executes one non-resumable operational readiness evidence clock.
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
		return Evidence{}, fmt.Errorf("operational_readiness_runner_rejected")
	}
	preflight, err := runner.Preflight.Check(ctx)
	if err != nil || validatePreflight(preflight, configuration.Identity.Mode) != nil {
		return Evidence{}, fmt.Errorf("operational_readiness_preflight_failed")
	}
	started := runner.Clock.Now()
	if started.IsZero() || started.Location() != time.UTC ||
		len(preflightWindowFailureReasons(preflight, started)) > 0 ||
		runner.Store.Begin(ctx, configuration, preflight, started) != nil {
		return Evidence{}, fmt.Errorf("operational_readiness_runner_start_failed")
	}
	evidence := newEvidence(configuration, preflight, started)
	runner.collect(ctx, configuration, &evidence)
	return runner.finish(ctx, configuration, evidence)
}

func validatePreflight(preflight Preflight, mode Mode) error {
	if len(preflightFailureReasons(preflight, mode)) > 0 {
		return fmt.Errorf("operational_readiness_preflight_invalid")
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
		return Evidence{}, fmt.Errorf("operational_readiness_runner_finish_failed")
	}
	if evidence.State == StateFailed {
		return evidence, fmt.Errorf("operational_readiness_qualification_failed")
	}
	return evidence, nil
}

func newEvidence(configuration Config, preflight Preflight, started time.Time) Evidence {
	return Evidence{SchemaVersion: "axiom.operationalReadiness.readiness.v1", Identity: configuration.Identity,
		Preflight: preflight, DeclaredLoad: configuration.DeclaredLoad,
		FaultSchedule: configuration.FaultSchedule, State: StateFailed, Qualified: false,
		ProfitabilityEvidence: false, StartedAt: started,
		RequiredDurationSeconds: int64(configuration.Duration.Seconds()),
		Samples:                 []Sample{}, Faults: []FaultEvent{}, Failures: []Failure{}}
}

func evaluateSample(evidence *Evidence, sample Sample) {
	for _, reason := range sampleFailureReasons(sample) {
		appendFailure(evidence, reason, sample.ObservedAt)
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
		return fmt.Errorf("operational_readiness_evidence_encode_failed")
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
