package observability

import (
	"fmt"
	"slices"
	"time"
)

var (
	sandboxQualificationOrderStates = []string{
		"APPROVED", "SUBMITTING", "ACKNOWLEDGED", "PARTIALLY_FILLED",
		"FILLED", "CANCEL_PENDING", "CANCELED", "REJECTED", "EXPIRED",
		"UNKNOWN", "RECOVERY_REQUIRED",
	}
	sandboxQualificationFailureReasons = []string{
		"duplicate_create", "lost_fill", "double_posted_fill",
		"unresolved_unknown", "reconciliation_mismatch", "suspense",
		"stale_data", "lease_loss", "persistence_failure",
		"unsafe_restart", "production_target", "cap_violation",
		"memory_leak", "critical_alert_slo", "operator_abort",
		"evidence_failure", "recovery_expired", "recovery_repeated",
		"recovery_unrecoverable",
	}
)

// RecordSandboxOrder records one bounded test/demo order lifecycle fact.
func (metrics *Metrics) RecordSandboxOrder(exchange, state string) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains(sandboxQualificationOrderStates, state) {
		return fmt.Errorf("metric_label_rejected:sandbox_order")
	}
	metrics.sandboxOrders.WithLabelValues(exchange, state).Inc()
	return nil
}

// RecordSandboxOrderAnomaly records a zero-tolerance create/fill defect.
func (metrics *Metrics) RecordSandboxOrderAnomaly(exchange, kind string) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains(
			[]string{"duplicate_create", "lost_fill", "double_posted_fill"},
			kind,
		) {
		return fmt.Errorf("metric_label_rejected:sandbox_anomaly")
	}
	metrics.sandboxAnomalies.WithLabelValues(exchange, kind).Inc()
	return nil
}

// SetSandboxUnknown publishes current count and oldest age without order IDs.
func (metrics *Metrics) SetSandboxUnknown(
	exchange string,
	count int,
	oldest time.Duration,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		count < 0 || oldest < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_unknown")
	}
	metrics.sandboxUnknown.WithLabelValues(exchange, "count").Set(float64(count))
	metrics.sandboxUnknown.WithLabelValues(exchange, "oldest_seconds").
		Set(oldest.Seconds())
	return nil
}

// SetSandboxReconciliation publishes mismatch and suspense counts.
func (metrics *Metrics) SetSandboxReconciliation(
	exchange string,
	mismatches, suspense int,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		mismatches < 0 || suspense < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_reconciliation")
	}
	metrics.sandboxReconciliation.WithLabelValues(exchange, "mismatch").
		Set(float64(mismatches))
	metrics.sandboxReconciliation.WithLabelValues(exchange, "suspense").
		Set(float64(suspense))
	return nil
}

// SetSandboxArms publishes bounded active/expired counts and expiry seconds.
func (metrics *Metrics) SetSandboxArms(
	exchange, state string,
	count int,
	nearestExpiry time.Duration,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains([]string{"active", "expired", "revoked"}, state) ||
		count < 0 || nearestExpiry < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_arm")
	}
	metrics.sandboxArms.WithLabelValues(exchange, state, "count").
		Set(float64(count))
	metrics.sandboxArms.WithLabelValues(exchange, state, "expiry_seconds").
		Set(nearestExpiry.Seconds())
	return nil
}

// SetSandboxCap publishes integer microunits so exact values never originate
// in binary floating point.
func (metrics *Metrics) SetSandboxCap(
	scope string,
	usedMicrounits, remainingMicrounits, limitMicrounits int64,
) error {
	if !slices.Contains(
		[]string{"per_order", "daily", "account_open", "global_open"}, scope,
	) || usedMicrounits < 0 || remainingMicrounits < 0 ||
		limitMicrounits < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_cap")
	}
	values := map[string]int64{
		"used": usedMicrounits, "remaining": remainingMicrounits,
		"limit": limitMicrounits,
	}
	for measure, value := range values {
		metrics.sandboxCapUsage.WithLabelValues(scope, measure).
			Set(float64(value) / 1_000_000)
	}
	return nil
}

// RecordSandboxCapRejection records one fixed policy rejection.
func (metrics *Metrics) RecordSandboxCapRejection(scope string) error {
	if !slices.Contains(
		[]string{"per_order", "daily", "account_open", "global_open"}, scope,
	) {
		return fmt.Errorf("metric_label_rejected:sandbox_cap")
	}
	metrics.sandboxCapRejections.WithLabelValues(scope).Inc()
	return nil
}

// RecordSandboxReset records one account-epoch incident transition.
func (metrics *Metrics) RecordSandboxReset(exchange, state string) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains(
			[]string{"OPEN", "RECONCILING", "RESOLVED", "QUARANTINED"},
			state,
		) {
		return fmt.Errorf("metric_label_rejected:sandbox_reset")
	}
	metrics.sandboxResets.WithLabelValues(exchange, state).Inc()
	return nil
}

// SetSandboxEngine publishes readiness plus bounded reconnect/restart events.
func (metrics *Metrics) SetSandboxEngine(
	exchange string,
	ready bool,
	event string,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		(event != "" && !slices.Contains(
			[]string{"reconnect", "restart", "lease_loss"}, event,
		)) {
		return fmt.Errorf("metric_label_rejected:sandbox_engine")
	}
	value := 0.0
	if ready {
		value = 1
	}
	metrics.sandboxEngineReady.WithLabelValues(exchange).Set(value)
	if event != "" {
		metrics.sandboxEngineEvents.WithLabelValues(exchange, event).Inc()
	}
	return nil
}

// ObserveSandboxRecovery records bounded reconciliation and recovery latency.
func (metrics *Metrics) ObserveSandboxRecovery(
	exchange, operation string,
	duration time.Duration,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains(
			[]string{"reconciliation", "unknown", "restart"}, operation,
		) || duration < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_recovery")
	}
	metrics.sandboxRecovery.WithLabelValues(exchange, operation).
		Observe(duration.Seconds())
	return nil
}

// ObserveCriticalAlert records the in-app alert creation SLO.
func (metrics *Metrics) ObserveCriticalAlert(
	reason Reason,
	duration time.Duration,
) error {
	if validateReason(reason) != nil || duration < 0 {
		return fmt.Errorf("metric_value_rejected:critical_alert")
	}
	metrics.criticalAlertLatency.WithLabelValues(string(reason)).
		Observe(duration.Seconds())
	return nil
}

// SetSandboxQualificationSoak publishes a single bounded qualification state.
func (metrics *Metrics) SetSandboxQualificationSoak(
	mode, state string,
	duration time.Duration,
) error {
	if !slices.Contains([]string{"smoke", "formal"}, mode) ||
		!slices.Contains(
			[]string{"PENDING", "RUNNING", "SMOKE_PASSED", "PASSED", "FAILED"},
			state,
		) || duration < 0 {
		return fmt.Errorf("metric_value_rejected:sandbox_qualification_soak")
	}
	metrics.sandboxQualificationSoakState.WithLabelValues(mode, state).Set(1)
	metrics.sandboxQualificationSoakDuration.WithLabelValues(mode).Set(duration.Seconds())
	return nil
}

// RecordSandboxQualificationFailure records one closed qualification failure.
func (metrics *Metrics) RecordSandboxQualificationFailure(reason string) error {
	if !slices.Contains(sandboxQualificationFailureReasons, reason) {
		return fmt.Errorf("metric_label_rejected:sandbox_qualification_failure")
	}
	metrics.sandboxQualificationSoakFailures.WithLabelValues(reason).Inc()
	return nil
}

// SetSandboxQualificationRecovery publishes one bounded read-only recovery
// state per exchange.
// State is closed so cardinality cannot grow with account or error data.
func (metrics *Metrics) SetSandboxQualificationRecovery(
	exchange, state string,
	count int,
) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) ||
		!slices.Contains([]string{
			"active", "recovered", "expired", "repeated", "unrecoverable",
		}, state) || count < 0 {
		return fmt.Errorf("metric_label_rejected:sandbox_qualification_recovery")
	}
	metrics.sandboxQualificationRecoveryIncidents.WithLabelValues(exchange, state).
		Set(float64(count))
	return nil
}

// SetSandboxQualificationMemoryTrend records the signed resident-memory delta after warm-up.
func (metrics *Metrics) SetSandboxQualificationMemoryTrend(window string, deltaBytes int64) error {
	if !slices.Contains([]string{"15m", "1h", "run"}, window) {
		return fmt.Errorf("metric_label_rejected:sandbox_qualification_memory_window")
	}
	metrics.sandboxQualificationMemoryTrend.WithLabelValues(window).Set(float64(deltaBytes))
	return nil
}
