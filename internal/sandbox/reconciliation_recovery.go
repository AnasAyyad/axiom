package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Bounded read-only recovery is deliberately narrower than generic sandbox
// recovery. It is usable only for one typed reconciliation or private-stream
// outage per account in one C6 run; every other failure remains terminal.
const (
	ReconciliationRecoveryWindow   = 72 * time.Hour
	ReconciliationRecoveryDeadline = 2 * time.Minute
	ReconciliationCleanInterval    = 30 * time.Second
	ReconciliationCleanChecks      = 2
)

// RecoveryState is the redacted state shown to qualification and operations.
type RecoveryState string

const (
	RecoveryNotRequired   RecoveryState = "not_required"
	RecoveryActive        RecoveryState = "active"
	RecoveryRecovered     RecoveryState = "recovered"
	RecoveryExpired       RecoveryState = "expired"
	RecoveryRepeated      RecoveryState = "repeated"
	RecoveryUnrecoverable RecoveryState = "unrecoverable"
)

// RecoveryIncidentSource is the closed, redacted boundary where an incident
// was detected. It never contains an endpoint, topic, or exchange payload.
type RecoveryIncidentSource string

const (
	RecoverySourceReconciliation RecoveryIncidentSource = "reconciliation"
	RecoverySourcePrivateStream  RecoveryIncidentSource = "private_stream"
)

// RecoveryEventKind is the immutable lifecycle event vocabulary.
type RecoveryEventKind string

const (
	RecoveryDetected           RecoveryEventKind = "detected"
	RecoveryFirstClean         RecoveryEventKind = "first_clean_check"
	RecoveryRecoveredEvent     RecoveryEventKind = "recovered"
	RecoveryExpiredEvent       RecoveryEventKind = "expired"
	RecoveryRepeatedEvent      RecoveryEventKind = "repeated"
	RecoveryUnrecoverableEvent RecoveryEventKind = "unrecoverable"
)

// ReconciliationRecoveryHealth is the complete safety proof required for a
// clean check. Recovery never treats a reconciliation result alone as safe.
type ReconciliationRecoveryHealth struct {
	StreamHealthy       bool
	EvidenceHealthy     bool
	LeaseHeld           bool
	AccountSafe         bool
	ReconciliationClean bool
}

// Safe reports whether every independent recovery gate is healthy.
func (health ReconciliationRecoveryHealth) Safe() bool {
	return health.StreamHealthy && health.EvidenceHealthy && health.LeaseHeld &&
		health.AccountSafe && health.ReconciliationClean
}

// RecoveryTransition is one redacted state-machine output. It contains no
// exchange payload, URL, secret, signed request, or raw error text.
type RecoveryTransition struct {
	Changed         bool
	State           RecoveryState
	Event           RecoveryEventKind
	IncidentSource  RecoveryIncidentSource
	FailureKind     exchangecontracts.ErrorKind
	CauseCode       string
	IncidentAt      time.Time
	DeadlineAt      time.Time
	CleanCheckCount uint8
	RecoveredAt     *time.Time
	EvidenceHash    string
}

// ErrReconciliationRecoveryTerminal marks a failure that must terminate C6.
var ErrReconciliationRecoveryTerminal = errors.New("reconciliation_recovery_terminal")

var recoveryCausePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// ClassifyRecoveryFailure converts an adapter error into the existing typed
// exchange kind plus one sanitized cause code. Unknown errors are terminal and
// never expose their text.
func ClassifyRecoveryFailure(err error) (exchangecontracts.ErrorKind, string) {
	if err == nil {
		return "", ""
	}
	kind := exchangecontracts.KindOf(err)
	cause, _, _, _ := exchangecontracts.DiagnosticOf(err)
	if !validRecoveryCause(cause) {
		cause = "untyped_failure"
	}
	return kind, cause
}

// ClassifyReconciliationFailure preserves the reconciliation-specific caller
// contract while sharing the bounded recovery classifier.
func ClassifyReconciliationFailure(err error) (exchangecontracts.ErrorKind, string) {
	return ClassifyRecoveryFailure(err)
}

// ReconciliationRecovery owns one account's bounded recovery lifecycle.
type ReconciliationRecovery struct {
	runStarted  time.Time
	incidentAt  time.Time
	deadline    time.Time
	lastClean   time.Time
	cleanChecks uint8
	used        bool
	state       RecoveryState
	source      RecoveryIncidentSource
}

// NewReconciliationRecovery constructs an unused account controller.
func NewReconciliationRecovery(runStarted time.Time) (*ReconciliationRecovery, error) {
	if runStarted.IsZero() || runStarted.Location() != time.UTC {
		return nil, fmt.Errorf("reconciliation_recovery_start_invalid")
	}
	return &ReconciliationRecovery{
		runStarted: runStarted,
		state:      RecoveryNotRequired,
	}, nil
}

// State returns the current fail-closed state.
func (recovery *ReconciliationRecovery) State() RecoveryState {
	if recovery == nil {
		return RecoveryUnrecoverable
	}
	return recovery.state
}

// Active reports whether the engine must remain DEGRADED and dispatch-free.
func (recovery *ReconciliationRecovery) Active() bool {
	return recovery != nil && recovery.state == RecoveryActive
}

// DispatchAllowed is false for the entire active recovery window.
func (recovery *ReconciliationRecovery) DispatchAllowed() bool {
	return recovery != nil && recovery.state != RecoveryActive &&
		recovery.state != RecoveryExpired && recovery.state != RecoveryRepeated &&
		recovery.state != RecoveryUnrecoverable
}

// ObserveFailure starts the one permitted recovery or returns a terminal
// transition. A second failure is terminal even when it has a permitted kind.
func (recovery *ReconciliationRecovery) ObserveFailure(
	at time.Time,
	kind exchangecontracts.ErrorKind,
	causeCode string,
) (RecoveryTransition, error) {
	return recovery.ObserveIncident(
		at, RecoverySourceReconciliation, kind, causeCode,
	)
}

// ObserveIncident starts the one permitted read-only recovery or returns a
// terminal transition. Reconciliation and private-stream incidents share one
// budget, so switching sources cannot create a second allowance.
func (recovery *ReconciliationRecovery) ObserveIncident(
	at time.Time,
	source RecoveryIncidentSource,
	kind exchangecontracts.ErrorKind,
	causeCode string,
) (RecoveryTransition, error) {
	if recovery == nil || !recovery.validTime(at) ||
		!validRecoverySource(source) || !validRecoveryCause(causeCode) {
		return RecoveryTransition{}, ErrReconciliationRecoveryTerminal
	}
	if !PermittedRecoveryKind(kind) {
		return recovery.terminal(
			at, RecoveryUnrecoverableEvent, source, kind, causeCode,
		)
	}
	if recovery.used {
		return recovery.terminal(
			at, RecoveryRepeatedEvent, source, kind, causeCode,
		)
	}
	recovery.used = true
	recovery.source = source
	recovery.incidentAt = at
	recovery.deadline = at.Add(ReconciliationRecoveryDeadline)
	recovery.cleanChecks = 0
	recovery.lastClean = time.Time{}
	recovery.state = RecoveryActive
	return recovery.transition(
		at, RecoveryDetected, source, kind, causeCode, true,
	), nil
}

// ObserveClean records a fully healthy clean reconciliation. Two checks must
// be at least 30 seconds apart before the account returns to READY_PAUSED.
func (recovery *ReconciliationRecovery) ObserveClean(
	at time.Time,
	health ReconciliationRecoveryHealth,
) (RecoveryTransition, error) {
	if recovery == nil || !recovery.validTime(at) {
		return RecoveryTransition{}, ErrReconciliationRecoveryTerminal
	}
	if recovery.state != RecoveryActive {
		return RecoveryTransition{}, nil
	}
	if !at.Before(recovery.deadline) {
		return recovery.terminal(at, RecoveryExpiredEvent,
			recovery.source, exchangecontracts.ErrorTransient,
			"recovery_deadline_exceeded")
	}
	if !health.Safe() {
		return recovery.terminal(at, RecoveryUnrecoverableEvent,
			recovery.source, exchangecontracts.ErrorValidation,
			"recovery_health_not_safe")
	}
	if recovery.cleanChecks > 0 && at.Sub(recovery.lastClean) < ReconciliationCleanInterval {
		return RecoveryTransition{State: RecoveryActive, Changed: false,
			IncidentAt: recovery.incidentAt, DeadlineAt: recovery.deadline,
			CleanCheckCount: recovery.cleanChecks}, nil
	}
	recovery.cleanChecks++
	recovery.lastClean = at
	if recovery.cleanChecks < ReconciliationCleanChecks {
		return recovery.transition(
			at, RecoveryFirstClean, recovery.source, "", "", true,
		), nil
	}
	recovery.state = RecoveryRecovered
	recoveredAt := at
	transition := recovery.transition(
		at, RecoveryRecoveredEvent, recovery.source, "", "", true,
	)
	transition.RecoveredAt = &recoveredAt
	return transition, nil
}

func (recovery *ReconciliationRecovery) validTime(at time.Time) bool {
	return at.Location() == time.UTC && !at.Before(recovery.runStarted)
}

func (recovery *ReconciliationRecovery) terminal(
	at time.Time,
	event RecoveryEventKind,
	source RecoveryIncidentSource,
	kind exchangecontracts.ErrorKind,
	causeCode string,
) (RecoveryTransition, error) {
	switch event {
	case RecoveryExpiredEvent:
		recovery.state = RecoveryExpired
	case RecoveryRepeatedEvent:
		recovery.state = RecoveryRepeated
	default:
		recovery.state = RecoveryUnrecoverable
	}
	return recovery.transition(
		at, event, source, kind, causeCode, true,
	), ErrReconciliationRecoveryTerminal
}

func (recovery *ReconciliationRecovery) transition(
	at time.Time,
	event RecoveryEventKind,
	source RecoveryIncidentSource,
	kind exchangecontracts.ErrorKind,
	causeCode string,
	changed bool,
) RecoveryTransition {
	return RecoveryTransition{
		Changed: changed, State: recovery.state, Event: event,
		IncidentSource: source, FailureKind: kind, CauseCode: causeCode,
		IncidentAt: recovery.incidentAt, DeadlineAt: recovery.deadline,
		CleanCheckCount: recovery.cleanChecks,
		EvidenceHash: recoveryEvidenceHash(at, event, source, kind, causeCode,
			recovery.incidentAt, recovery.deadline, recovery.cleanChecks),
	}
}

func recoveryEvidenceHash(
	at time.Time,
	event RecoveryEventKind,
	source RecoveryIncidentSource,
	kind exchangecontracts.ErrorKind,
	causeCode string,
	incidentAt, deadline time.Time,
	cleanChecks uint8,
) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d",
		event, source, kind, causeCode, at.UTC().Format(time.RFC3339Nano),
		incidentAt.UnixNano(), deadline.UnixNano(), cleanChecks,
		ReconciliationRecoveryDeadline.Milliseconds(),
	)))
	return hex.EncodeToString(digest[:])
}

// PermittedRecoveryKind reports whether a typed failure may consume the one
// bounded C6 recovery allowance.
func PermittedRecoveryKind(kind exchangecontracts.ErrorKind) bool {
	return kind == exchangecontracts.ErrorTransient || kind == exchangecontracts.ErrorMaintenance
}

func validRecoverySource(source RecoveryIncidentSource) bool {
	return source == RecoverySourceReconciliation ||
		source == RecoverySourcePrivateStream
}

func validRecoveryCause(value string) bool {
	return recoveryCausePattern.MatchString(value)
}
