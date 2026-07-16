package risk

import (
	"sync"
	"time"
)

const cautiousHysteresis = 5 * time.Minute

// Engine serializes risk evaluation and non-auto-unpausing state.
type Engine struct {
	mutex        sync.Mutex
	state        State
	healthySince time.Time
	audit        AuditSink
	alerts       AlertSink
}

// NewEngine constructs a central engine in mandatory PAUSED state.
func NewEngine(audit AuditSink, alerts AlertSink) (*Engine, error) {
	if audit == nil || alerts == nil {
		return nil, riskError("risk_engine_configuration_invalid")
	}
	return &Engine{state: StatePaused, audit: audit, alerts: alerts}, nil
}

// BeginStartupRecovery enters LOCKED before protected state is restored.
func (engine *Engine) BeginStartupRecovery(at time.Time) error {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if at.IsZero() || at.Location() != time.UTC || engine.state != StatePaused {
		return riskError("startup_recovery_rejected")
	}
	return engine.transitionLocked(StateLocked, "startup_recovery", "system", at)
}

// CompleteStartupRecovery establishes administrative readiness at PAUSED only.
func (engine *Engine) CompleteStartupRecovery(at time.Time) error {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if at.IsZero() || at.Location() != time.UTC || engine.state != StateLocked {
		return riskError("startup_recovery_rejected")
	}
	return engine.transitionLocked(StatePaused, "startup_recovery_complete", "system", at)
}

// State returns the current global engine posture.
func (engine *Engine) State() State {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	return engine.state
}

// Evaluate applies all scope policies and persists escalations immediately.
func (engine *Engine) Evaluate(request Request) (Decision, error) {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if !validRequest(request) {
		return Decision{}, riskError("risk_request_invalid")
	}
	decision := evaluatePolicies(request, engine.state)
	if stateRank(decision.EffectiveState) > stateRank(engine.state) {
		if err := engine.transitionLocked(decision.EffectiveState, decision.ReasonCode, "system", request.EvaluatedAt); err != nil {
			return Decision{}, err
		}
	}
	if decision.Action != ActionApprove && engine.alerts.Emit(decision.ReasonCode, decision.Action, decision.EffectiveState) != nil {
		return Decision{}, riskError("risk_alert_failed")
	}
	return decision, nil
}

// ObserveHealthy automatically recovers only CAUTIOUS after five healthy minutes.
func (engine *Engine) ObserveHealthy(now time.Time) error {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if now.IsZero() || now.Location() != time.UTC {
		return riskError("healthy_observation_invalid")
	}
	if engine.state != StateCautious {
		engine.healthySince = time.Time{}
		return nil
	}
	if engine.healthySince.IsZero() {
		engine.healthySince = now
		return nil
	}
	if now.Sub(engine.healthySince) < cautiousHysteresis {
		return nil
	}
	return engine.transitionLocked(StateNormal, "cautious_hysteresis_recovered", "system", now)
}

// ObserveUnhealthy resets cautious hysteresis without weakening current state.
func (engine *Engine) ObserveUnhealthy() {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	engine.healthySince = time.Time{}
}

// RecoveryEvidence contains every manual transition prerequisite.
type RecoveryEvidence struct {
	Reconciled, PersistenceHealthy, BooksFresh, UnknownOrdersResolved bool
	Reauthenticated, AuditDurable                                     bool
	Actor, Reason                                                     string
	At                                                                time.Time
}

// ManualTransition is the only PAUSED/LOCKED recovery path.
func (engine *Engine) ManualTransition(target State, evidence RecoveryEvidence) error {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if !validRecovery(evidence) || !validState(target) {
		return riskError("manual_transition_rejected")
	}
	if engine.state == StateLocked && target != StatePaused {
		return riskError("manual_transition_rejected")
	}
	if engine.state == StatePaused && target != StateNormal && target != StateCautious {
		return riskError("manual_transition_rejected")
	}
	return engine.transitionLocked(target, evidence.Reason, evidence.Actor, evidence.At)
}

func (engine *Engine) transitionLocked(next State, reason, actor string, at time.Time) error {
	prior := engine.state
	if prior == next {
		return nil
	}
	if engine.audit.Append(AuditEvent{Type: "risk_state_transition", ReasonCode: reason,
		Prior: prior, Next: next, Actor: actor, At: at}) != nil {
		return riskError("risk_audit_failed")
	}
	engine.state, engine.healthySince = next, time.Time{}
	return nil
}

func validRequest(request Request) bool {
	if !validIntent(request.Intent) || request.EvaluatedAt.IsZero() || request.EvaluatedAt.Location() != time.UTC ||
		len(request.Policies) == 0 {
		return false
	}
	for _, policy := range request.Policies {
		if !validPolicy(policy) {
			return false
		}
	}
	return true
}

func validIntent(intent Intent) bool {
	return intent == IntentEntry || intent == IntentExit || intent == IntentCancel ||
		intent == IntentRecovery || intent == IntentReconciliation
}

func validRecovery(value RecoveryEvidence) bool {
	return value.Reconciled && value.PersistenceHealthy && value.BooksFresh && value.UnknownOrdersResolved &&
		value.Reauthenticated && value.AuditDurable && value.Actor != "" && value.Reason != "" &&
		!value.At.IsZero() && value.At.Location() == time.UTC
}
