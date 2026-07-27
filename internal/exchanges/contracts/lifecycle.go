package exchangecontracts

import "time"

// RecoveryAction is a fixed collector response retained in qualification
// evidence. It never contains a destination, identifier, or arbitrary text.
type RecoveryAction string

// RecoveryReconnect rebuilds a defective stream or book generation.
const RecoveryReconnect RecoveryAction = "reconnect"

// RecoveryClockResample retries clock health without replacing the stream.
const RecoveryClockResample RecoveryAction = "clock_resample"

// RecoveryScheduledRenewal replaces a connection at its planned lifetime.
const RecoveryScheduledRenewal RecoveryAction = "scheduled_renewal"

// RecoveryTerminate records collector termination without retry.
const RecoveryTerminate RecoveryAction = "terminate"

// FailureAttribution is evidence-based ownership of one observed failure.
// Duration alone must never select one of these values.
type FailureAttribution string

// AttributionInternal identifies an evidenced local implementation cause.
const AttributionInternal FailureAttribution = "internal"

// AttributionNetwork identifies an evidenced DNS or network cause.
const AttributionNetwork FailureAttribution = "network"

// AttributionUpstream identifies an evidenced exchange response cause.
const AttributionUpstream FailureAttribution = "upstream"

// AttributionContractMismatch identifies an evidenced schema or protocol defect.
const AttributionContractMismatch FailureAttribution = "contract_mismatch"

// AttributionScheduled identifies planned renewal.
const AttributionScheduled FailureAttribution = "scheduled"

// AttributionRecovered identifies a successful health restoration.
const AttributionRecovered FailureAttribution = "recovered"

// AttributionExternalUnclassified preserves uncertainty when bounded facts
// cannot support a narrower attribution.
const AttributionExternalUnclassified FailureAttribution = "external_unclassified"

// RecoveryActionCounts is the fixed-cardinality action summary.
type RecoveryActionCounts struct {
	Reconnect        uint64 `json:"reconnect"`
	ClockResample    uint64 `json:"clock_resample"`
	ScheduledRenewal uint64 `json:"scheduled_renewal"`
	Terminate        uint64 `json:"terminate"`
}

// FailureAttributionCounts is the fixed-cardinality evidence attribution summary.
type FailureAttributionCounts struct {
	Internal             uint64 `json:"internal"`
	Network              uint64 `json:"network"`
	Upstream             uint64 `json:"upstream"`
	ContractMismatch     uint64 `json:"contract_mismatch"`
	Scheduled            uint64 `json:"scheduled"`
	Recovered            uint64 `json:"recovered"`
	ExternalUnclassified uint64 `json:"external_unclassified"`
}

// CollectorHealthSnapshot is one immutable combined book-and-clock decision
// boundary. Consumers must use Eligible instead of independently interpreting
// the mutable book and client clock.
type CollectorHealthSnapshot struct {
	ObservedAt       time.Time     `json:"observed_at"`
	Exchange         string        `json:"exchange"`
	Instrument       string        `json:"instrument"`
	BookHealth       string        `json:"book_health"`
	BookHealthy      bool          `json:"book_healthy"`
	BookFresh        bool          `json:"book_fresh"`
	BookEligible     bool          `json:"book_eligible"`
	ClockEligible    bool          `json:"clock_eligible"`
	ClockObservedAt  time.Time     `json:"clock_observed_at,omitempty"`
	ClockOffset      time.Duration `json:"clock_offset_nanos,omitempty"`
	ClockUncertainty time.Duration `json:"clock_uncertainty_nanos,omitempty"`
	Eligible         bool          `json:"eligible"`
	DegradedSince    time.Time     `json:"degraded_since,omitempty"`
}

// CollectorLifecycleEvidence is the bounded synchronous lifecycle fact
// accepted by formal qualification journals.
type CollectorLifecycleEvidence struct {
	ObservedAt       time.Time          `json:"observed_at"`
	Exchange         string             `json:"exchange"`
	Instrument       string             `json:"instrument"`
	Generation       uint64             `json:"generation,omitempty"`
	Cycle            uint64             `json:"cycle"`
	Attempt          uint32             `json:"attempt"`
	Phase            string             `json:"phase"`
	Stage            string             `json:"stage,omitempty"`
	Reason           string             `json:"reason,omitempty"`
	Action           RecoveryAction     `json:"recovery_action,omitempty"`
	Cause            string             `json:"cause,omitempty"`
	Attribution      FailureAttribution `json:"attribution"`
	FailureKind      ErrorKind          `json:"failure_kind,omitempty"`
	Operation        Operation          `json:"operation,omitempty"`
	RetryAfter       time.Duration      `json:"retry_after_nanos,omitempty"`
	HTTPStatus       int                `json:"http_status,omitempty"`
	Transport        FailureMetadata    `json:"transport"`
	ClockOffset      time.Duration      `json:"clock_offset_nanos,omitempty"`
	ClockUncertainty time.Duration      `json:"clock_uncertainty_nanos,omitempty"`
	AttemptDuration  time.Duration      `json:"attempt_duration_nanos,omitempty"`
	Backoff          time.Duration      `json:"backoff_nanos,omitempty"`
	ResyncElapsed    time.Duration      `json:"resync_elapsed_nanos,omitempty"`
	ReachedHealthy   bool               `json:"reached_healthy"`
}

// LifecycleEvidenceSink is optional in ordinary application use and
// synchronous/fail-closed in formal runners.
type LifecycleEvidenceSink interface {
	AppendCollectorLifecycle(CollectorLifecycleEvidence) error
}

// LifecycleEvidenceSinkFunc adapts a function without weakening synchronous
// error propagation.
type LifecycleEvidenceSinkFunc func(CollectorLifecycleEvidence) error

// AppendCollectorLifecycle synchronously forwards one bounded event to the
// wrapped sink function.
func (sink LifecycleEvidenceSinkFunc) AppendCollectorLifecycle(event CollectorLifecycleEvidence) error {
	return sink(event)
}
