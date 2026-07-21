package binance

import (
	"errors"
	"log/slog"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const maximumReconnectDiagnostics = 512

// ReconnectDiagnostic is a bounded, sanitized lifecycle fact retained in
// rolling and terminal qualification evidence. It never contains URLs,
// payloads, addresses, or arbitrary error text.
type ReconnectDiagnostic struct {
	ObservedAt       time.Time                   `json:"observed_at"`
	Instrument       string                      `json:"instrument"`
	Cycle            uint64                      `json:"cycle"`
	Attempt          uint32                      `json:"attempt"`
	Generation       uint64                      `json:"generation,omitempty"`
	Phase            string                      `json:"phase"`
	Stage            string                      `json:"stage,omitempty"`
	Reason           string                      `json:"reason,omitempty"`
	Cause            string                      `json:"cause,omitempty"`
	Attribution      string                      `json:"attribution"`
	FailureKind      exchangecontracts.ErrorKind `json:"failure_kind,omitempty"`
	Operation        exchangecontracts.Operation `json:"operation,omitempty"`
	RetryAfter       time.Duration               `json:"retry_after_nanos,omitempty"`
	HTTPStatus       int                         `json:"http_status,omitempty"`
	ClockOffset      time.Duration               `json:"clock_offset_nanos,omitempty"`
	ClockUncertainty time.Duration               `json:"clock_uncertainty_nanos,omitempty"`
	SnapshotSequence uint64                      `json:"snapshot_sequence,omitempty"`
	BufferedDepth    int                         `json:"buffered_depth,omitempty"`
	AttemptDuration  time.Duration               `json:"attempt_duration_nanos,omitempty"`
	Backoff          time.Duration               `json:"backoff_nanos,omitempty"`
	ResyncElapsed    time.Duration               `json:"resync_elapsed_nanos,omitempty"`
	ReachedHealthy   bool                        `json:"reached_healthy"`
}

func (collector *InstrumentCollector) outcomeDiagnostic(
	outcome generationOutcome,
	phase string,
	attemptDuration, backoff, resyncElapsed time.Duration,
) ReconnectDiagnostic {
	diagnostic := ReconnectDiagnostic{ObservedAt: collector.lifecycle.Now().UTC(),
		Instrument: collector.config.Instrument.Symbol(), Cycle: collector.lifecycleCycle.Load(),
		Attempt: uint32(collector.lifecycleAttempt.Load()), Generation: outcome.generation,
		Phase: phase, Stage: outcome.stage, Reason: outcome.reason.String(), Cause: outcome.cause,
		FailureKind: outcome.failureKind, Operation: outcome.operation, RetryAfter: outcome.retryAfter, HTTPStatus: outcome.httpStatus,
		ClockOffset: outcome.clockOffset, ClockUncertainty: outcome.clockUncertainty,
		AttemptDuration: attemptDuration, Backoff: backoff, ResyncElapsed: resyncElapsed,
		ReachedHealthy: outcome.reachedHealthy}
	diagnostic.Attribution = reconnectAttribution(diagnostic)
	return diagnostic
}

func reconnectAttribution(diagnostic ReconnectDiagnostic) string {
	if diagnostic.Phase == "health_restored" {
		return "recovered"
	}
	if diagnostic.Reason == reconnectScheduledRenewal.String() {
		return "scheduled"
	}
	switch diagnostic.Reason {
	case reconnectQueue.String(), reconnectBuffer.String(), reconnectInvalidEvent.String(),
		reconnectSequenceGap.String(), reconnectSnapshotBridge.String():
		return "internal"
	}

	switch diagnostic.FailureKind {
	case exchangecontracts.ErrorRateLimit:
		if diagnostic.HTTPStatus != 0 {
			return "upstream"
		}
		return "internal"
	case exchangecontracts.ErrorMaintenance:
		return "upstream"
	case exchangecontracts.ErrorValidation, exchangecontracts.ErrorTimestamp:
		return "internal"
	case exchangecontracts.ErrorTransient:
		if diagnostic.HTTPStatus >= 500 {
			return "upstream"
		}
		switch diagnostic.Cause {
		case "dns_failure", "network_timeout", "tcp_connect_failure", "network_io_failure":
			return "network"
		}
		return "external_unclassified"
	}
	if diagnostic.Cause == "recorder" {
		return "internal"
	}
	return "unclassified"
}

func (collector *InstrumentCollector) recordOperationDiagnostic(
	stage string,
	generation uint64,
	duration time.Duration,
	clockOffset time.Duration,
	clockUncertainty time.Duration,
	snapshotSequence uint64,
	bufferedDepth int,
) {
	diagnostic := collector.outcomeDiagnostic(generationOutcome{reachedHealthy: stage == "healthy",
		generation: generation, stage: stage, cause: "success", clockOffset: clockOffset,
		clockUncertainty: clockUncertainty}, "operation_succeeded", duration, 0, 0)
	diagnostic.Attribution = "observed"
	diagnostic.SnapshotSequence = snapshotSequence
	diagnostic.BufferedDepth = bufferedDepth
	collector.recordDiagnostic(diagnostic)
}

func (collector *InstrumentCollector) recordDiagnostic(diagnostic ReconnectDiagnostic) {
	collector.stats.recordDiagnostic(diagnostic)
	if diagnostic.Instrument == "" {
		return
	}
	slog.Info("binance_collector_lifecycle",
		"instrument", diagnostic.Instrument,
		"cycle", diagnostic.Cycle,
		"attempt", diagnostic.Attempt,
		"generation", diagnostic.Generation,
		"phase", diagnostic.Phase,
		"stage", diagnostic.Stage,
		"reason", diagnostic.Reason,
		"cause", diagnostic.Cause,
		"attribution", diagnostic.Attribution,
		"failure_kind", diagnostic.FailureKind,
		"operation", diagnostic.Operation,
		"retry_after_nanos", diagnostic.RetryAfter.Nanoseconds(),
		"http_status", diagnostic.HTTPStatus,
		"attempt_duration_nanos", diagnostic.AttemptDuration.Nanoseconds(),
		"backoff_nanos", diagnostic.Backoff.Nanoseconds(),
		"clock_offset_nanos", diagnostic.ClockOffset.Nanoseconds(),
		"snapshot_sequence", diagnostic.SnapshotSequence,
		"buffered_depth", diagnostic.BufferedDepth,
		"clock_uncertainty_nanos", diagnostic.ClockUncertainty.Nanoseconds(),
		"resync_elapsed_nanos", diagnostic.ResyncElapsed.Nanoseconds(),
		"reached_healthy", diagnostic.ReachedHealthy)
}

func generationFailure(outcome generationOutcome, stage, cause string, err error) generationOutcome {
	outcome.stage, outcome.cause = stage, cause
	var failure *exchangecontracts.Error
	if errors.As(err, &failure) && failure != nil {
		outcome.failureKind, outcome.operation, outcome.retryAfter = failure.Kind, failure.Operation, failure.RetryAfter
		outcome.httpStatus = failure.HTTPStatus
		if failure.Cause != "" {
			outcome.cause = failure.Cause
		}
	}
	return outcome
}
