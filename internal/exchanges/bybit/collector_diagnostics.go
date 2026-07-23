package bybit

import (
	"errors"
	"log/slog"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const maximumReconnectDiagnostics = 8192

type reconnectReason uint8

const (
	reconnectNone reconnectReason = iota
	reconnectSubscription
	reconnectStream
	reconnectSnapshot
	reconnectClock
	reconnectHeartbeat
	reconnectStaleBook
	reconnectQueue
	reconnectInvalidEvent
	reconnectSequenceGap
	reconnectScheduledRenewal
	reconnectReasonCount
)

var reconnectReasonNames = [...]string{
	"", "subscription", "stream", "snapshot", "clock", "heartbeat", "stale_book",
	"queue", "invalid_event", "sequence_gap", "scheduled_renewal",
}

func (reason reconnectReason) valid() bool {
	return reason > reconnectNone && reason < reconnectReasonCount
}

// String returns the bounded evidence label for the reconnect reason.
func (reason reconnectReason) String() string {
	if !reason.valid() {
		return ""
	}
	return reconnectReasonNames[reason]
}

// ReconnectDiagnostic is a bounded, sanitized Bybit lifecycle fact.
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
	RequestDuration  time.Duration               `json:"request_duration_nanos,omitempty"`
	HeaderDuration   time.Duration               `json:"response_header_duration_nanos,omitempty"`
	BodyDuration     time.Duration               `json:"response_body_duration_nanos,omitempty"`
	ResponseBytes    uint64                      `json:"response_bytes,omitempty"`
	ContentLength    uint64                      `json:"content_length_bytes,omitempty"`
	ContentKnown     bool                        `json:"content_length_known,omitempty"`
	BodyLimit        uint64                      `json:"body_limit_bytes,omitempty"`
	ClockUncertainty time.Duration               `json:"clock_uncertainty_nanos,omitempty"`
	AttemptDuration  time.Duration               `json:"attempt_duration_nanos,omitempty"`
	Backoff          time.Duration               `json:"backoff_nanos,omitempty"`
	ResyncElapsed    time.Duration               `json:"resync_elapsed_nanos,omitempty"`
	ReachedHealthy   bool                        `json:"reached_healthy"`
}

type generationOutcome struct {
	fatal            error
	reachedHealthy   bool
	reason           reconnectReason
	lostHealthAt     time.Time
	generation       uint64
	stage            string
	cause            string
	failureKind      exchangecontracts.ErrorKind
	operation        exchangecontracts.Operation
	retryAfter       time.Duration
	httpStatus       int
	failureMetadata  exchangecontracts.FailureMetadata
	clockUncertainty time.Duration
}

func generationFailure(outcome generationOutcome, stage, cause string, err error) generationOutcome {
	outcome.stage, outcome.cause = stage, cause
	var failure *exchangecontracts.Error
	if errors.As(err, &failure) && failure != nil {
		outcome.failureKind, outcome.operation, outcome.retryAfter = failure.Kind, failure.Operation, failure.RetryAfter
		outcome.httpStatus, outcome.failureMetadata = failure.HTTPStatus, failure.Metadata
		if failure.Cause != "" {
			outcome.cause = failure.Cause
		}
	}
	return outcome
}

func (collector *InstrumentCollector) outcomeDiagnostic(
	outcome generationOutcome,
	phase string,
	attemptDuration, backoff, resyncElapsed time.Duration,
) ReconnectDiagnostic {
	metadata := outcome.failureMetadata
	diagnostic := ReconnectDiagnostic{ObservedAt: collector.lifecycle.Now().UTC(),
		Instrument: collector.config.Instrument.Symbol(), Cycle: collector.lifecycleCycle.Load(),
		Attempt: uint32(collector.lifecycleAttempt.Load()), Generation: outcome.generation,
		Phase: phase, Stage: outcome.stage, Reason: outcome.reason.String(), Cause: outcome.cause,
		FailureKind: outcome.failureKind, Operation: outcome.operation, RetryAfter: outcome.retryAfter,
		HTTPStatus: outcome.httpStatus, RequestDuration: metadata.RequestDuration,
		HeaderDuration: metadata.ResponseHeaderDuration, BodyDuration: metadata.ResponseBodyDuration,
		ResponseBytes: metadata.ResponseBytes, ContentLength: metadata.ContentLengthBytes,
		ContentKnown: metadata.ContentLengthKnown, BodyLimit: metadata.BodyLimitBytes,
		ClockUncertainty: outcome.clockUncertainty, AttemptDuration: attemptDuration,
		Backoff: backoff, ResyncElapsed: resyncElapsed, ReachedHealthy: outcome.reachedHealthy}
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
	case reconnectQueue.String(), reconnectInvalidEvent.String(), reconnectSequenceGap.String():
		return "internal"
	}
	switch diagnostic.FailureKind {
	case exchangecontracts.ErrorRateLimit:
		if diagnostic.Cause == "rate_budget_exhausted" || diagnostic.Cause == "rate_budget_failure" {
			return "internal"
		}
		if diagnostic.HTTPStatus != 0 {
			return "upstream"
		}
		return "internal"
	case exchangecontracts.ErrorMaintenance:
		return "upstream"
	case exchangecontracts.ErrorValidation:
		if diagnostic.Cause == "response_body_too_large" {
			return "contract_mismatch"
		}
		if diagnostic.HTTPStatus != 0 {
			return "upstream"
		}
		return "internal"
	case exchangecontracts.ErrorTransient:
		if diagnostic.HTTPStatus >= 500 || diagnostic.Cause == "response_body_empty" {
			return "upstream"
		}
		switch diagnostic.Cause {
		case "dns_failure", "network_timeout", "tcp_connect_failure", "network_io_failure",
			"response_body_timeout", "response_body_read_failed", "response_body_close_failed":
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
	clockUncertainty time.Duration,
) {
	diagnostic := collector.outcomeDiagnostic(generationOutcome{reachedHealthy: stage == "healthy",
		generation: generation, stage: stage, cause: "success",
		clockUncertainty: clockUncertainty}, "operation_succeeded", duration, 0, 0)
	diagnostic.Attribution = "observed"
	collector.recordDiagnostic(diagnostic)
}

func (collector *InstrumentCollector) recordDiagnostic(diagnostic ReconnectDiagnostic) {
	collector.stats.recordDiagnostic(diagnostic)
	slog.Info("bybit_collector_lifecycle",
		"instrument", diagnostic.Instrument, "cycle", diagnostic.Cycle, "attempt", diagnostic.Attempt,
		"generation", diagnostic.Generation, "phase", diagnostic.Phase, "stage", diagnostic.Stage,
		"reason", diagnostic.Reason, "cause", diagnostic.Cause, "attribution", diagnostic.Attribution,
		"failure_kind", diagnostic.FailureKind, "operation", diagnostic.Operation,
		"retry_after_nanos", diagnostic.RetryAfter.Nanoseconds(), "http_status", diagnostic.HTTPStatus,
		"request_duration_nanos", diagnostic.RequestDuration.Nanoseconds(),
		"response_header_duration_nanos", diagnostic.HeaderDuration.Nanoseconds(),
		"response_body_duration_nanos", diagnostic.BodyDuration.Nanoseconds(),
		"response_bytes", diagnostic.ResponseBytes, "content_length_bytes", diagnostic.ContentLength,
		"content_length_known", diagnostic.ContentKnown, "body_limit_bytes", diagnostic.BodyLimit,
		"clock_uncertainty_nanos", diagnostic.ClockUncertainty.Nanoseconds(),
		"attempt_duration_nanos", diagnostic.AttemptDuration.Nanoseconds(),
		"backoff_nanos", diagnostic.Backoff.Nanoseconds(),
		"resync_elapsed_nanos", diagnostic.ResyncElapsed.Nanoseconds(),
		"reached_healthy", diagnostic.ReachedHealthy)
}
