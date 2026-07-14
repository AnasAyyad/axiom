package alerting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"
)

// Severity is the closed alert level contract.
type Severity string

// Supported alert severity values.
const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Reason is a bounded operational reason code, never arbitrary error text.
type Reason string

// AcknowledgementReason is a closed operator acknowledgement vocabulary.
type AcknowledgementReason string

// Supported fault and acknowledgement reason values.
const (
	ReasonPersistenceFailure     Reason                = "persistence_failure"
	ReasonFencingLeaseLost       Reason                = "fencing_lease_lost"
	ReasonDiskCritical           Reason                = "disk_critical"
	ReasonDiskPressure           Reason                = "disk_pressure"
	ReasonClockDrift             Reason                = "clock_drift"
	ReasonQueueSaturated         Reason                = "queue_saturated"
	ReasonBookUnhealthy          Reason                = "book_unhealthy"
	ReasonStaleData              Reason                = "stale_data"
	ReasonReconciliationMismatch Reason                = "reconciliation_mismatch"
	ReasonAccountingInvariant    Reason                = "accounting_invariant"
	ReasonAlertDelivery          Reason                = "alert_delivery"
	AcknowledgementInvestigated  AcknowledgementReason = "investigated"
	AcknowledgementFalsePositive AcknowledgementReason = "false_positive"
	AcknowledgementRecovery      AcknowledgementReason = "recovery_verified"
	AcknowledgementAcceptedRisk  AcknowledgementReason = "accepted_risk"
)

var reasons = []Reason{
	ReasonPersistenceFailure, ReasonFencingLeaseLost, ReasonDiskCritical, ReasonDiskPressure,
	ReasonClockDrift, ReasonQueueSaturated, ReasonBookUnhealthy, ReasonStaleData,
	ReasonReconciliationMismatch, ReasonAccountingInvariant, ReasonAlertDelivery,
}

var failClosedReasons = []Reason{
	ReasonPersistenceFailure, ReasonFencingLeaseLost, ReasonDiskCritical,
	ReasonClockDrift, ReasonQueueSaturated, ReasonBookUnhealthy, ReasonStaleData,
	ReasonReconciliationMismatch, ReasonAccountingInvariant,
}

// Fault is the sanitized, bounded fact accepted by the alert service.
type Fault struct {
	Severity      Severity
	Reason        Reason
	Component     string
	CorrelationID string
	OccurredAt    time.Time
}

// Alert is durable in-app state. It contains codes and identities, not payloads.
type Alert struct {
	ID               string
	DeduplicationKey string
	Severity         Severity
	Reason           Reason
	Component        string
	CorrelationID    string
	CreatedAt        time.Time
	LastSeenAt       time.Time
	Occurrences      uint64
	Revision         uint64
}

// PendingDelivery is durable retry work reconstructed without raw payloads.
type PendingDelivery struct {
	ID       string
	SinkName string
	Alert    Alert
}

// Store is the durable alert and external-delivery boundary.
type Store interface {
	Upsert(context.Context, Alert) (Alert, error)
	Acknowledge(context.Context, string, string, string, time.Time) (Alert, error)
	PrepareDelivery(context.Context, Alert, string, time.Time) (string, error)
	CompleteDelivery(context.Context, string, bool, string, time.Time) error
	DueDeliveries(context.Context, time.Time, int32) ([]PendingDelivery, error)
}

// RetryDue retries bounded durable work for the configured sink.
func (service *Service) RetryDue(ctx context.Context, limit int32) (int, error) {
	if service.sink == nil || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("alert_delivery_retry_rejected")
	}
	deliveries, err := service.store.DueDeliveries(ctx, service.now(), limit)
	if err != nil {
		return 0, fmt.Errorf("alert_delivery_state_failed")
	}
	delivered := 0
	for _, pending := range deliveries {
		if pending.SinkName != service.sink.Name() {
			return delivered, fmt.Errorf("alert_delivery_sink_mismatch")
		}
		deliveryErr := service.sink.Deliver(ctx, pending.Alert)
		reason := ""
		if deliveryErr != nil {
			reason = "sink_unavailable"
		}
		if err = service.store.CompleteDelivery(ctx, pending.ID, deliveryErr == nil, reason, service.now()); err != nil {
			return delivered, fmt.Errorf("alert_delivery_state_failed")
		}
		if deliveryErr == nil {
			delivered++
		}
	}
	return delivered, nil
}

// Sink delivers only the sanitized alert contract.
type Sink interface {
	Name() string
	Deliver(context.Context, Alert) error
}

// FailCloser irreversibly locks new decision acceptance for this process.
type FailCloser interface{ Lock(string) }

// Service synchronously locks critical faults before attempting persistence.
type Service struct {
	store Store
	sink  Sink
	gate  FailCloser
	now   func() time.Time
}

// NewService constructs the synchronous alert/fail-closed boundary.
func NewService(store Store, sink Sink, gate FailCloser, now func() time.Time) (*Service, error) {
	if store == nil || gate == nil {
		return nil, fmt.Errorf("alert_service_invalid")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, sink: sink, gate: gate, now: now}, nil
}

// Trigger validates, fails closed when required, durably upserts, and then
// attempts the configured external delivery while retaining retry state.
func (service *Service) Trigger(ctx context.Context, fault Fault) (Alert, error) {
	if err := validateFault(fault); err != nil {
		return Alert{}, err
	}
	if slices.Contains(failClosedReasons, fault.Reason) {
		service.gate.Lock(string(fault.Reason))
	}
	dedup := digest(string(fault.Severity), string(fault.Reason), fault.Component)
	alert := Alert{
		ID:               "alert_" + digest(dedup, fault.OccurredAt.Format(time.RFC3339Nano))[:24],
		DeduplicationKey: dedup, Severity: fault.Severity, Reason: fault.Reason,
		Component: fault.Component, CorrelationID: fault.CorrelationID,
		CreatedAt: fault.OccurredAt, LastSeenAt: fault.OccurredAt, Occurrences: 1, Revision: 1,
	}
	stored, err := service.store.Upsert(ctx, alert)
	if err != nil {
		return Alert{}, fmt.Errorf("alert_persistence_failed")
	}
	if service.sink == nil {
		return stored, nil
	}
	deliveryID, err := service.store.PrepareDelivery(ctx, stored, service.sink.Name(), service.now())
	if err != nil {
		return stored, fmt.Errorf("alert_delivery_state_failed")
	}
	deliveryErr := service.sink.Deliver(ctx, stored)
	reason := ""
	if deliveryErr != nil {
		reason = "sink_unavailable"
	}
	if err := service.store.CompleteDelivery(ctx, deliveryID, deliveryErr == nil, reason, service.now()); err != nil {
		return stored, fmt.Errorf("alert_delivery_state_failed")
	}
	if deliveryErr != nil {
		return stored, fmt.Errorf("alert_delivery_failed")
	}
	return stored, nil
}

// Acknowledge records one closed operator acknowledgement reason.
func (service *Service) Acknowledge(ctx context.Context, alertID, actor string, reason AcknowledgementReason) (Alert, error) {
	if !safeIdentity(alertID, 128) || !safeIdentity(actor, 128) || !slices.Contains([]AcknowledgementReason{
		AcknowledgementInvestigated, AcknowledgementFalsePositive, AcknowledgementRecovery, AcknowledgementAcceptedRisk,
	}, reason) {
		return Alert{}, fmt.Errorf("alert_acknowledgement_rejected")
	}
	return service.store.Acknowledge(ctx, alertID, actor, string(reason), service.now())
}

func validateFault(fault Fault) error {
	if !slices.Contains([]Severity{SeverityInfo, SeverityWarning, SeverityCritical}, fault.Severity) ||
		!slices.Contains(reasons, fault.Reason) || !safeIdentity(fault.Component, 64) ||
		!safeIdentity(fault.CorrelationID, 128) || fault.OccurredAt.IsZero() || fault.OccurredAt.Location() != time.UTC {
		return fmt.Errorf("alert_fault_rejected")
	}
	if slices.Contains(failClosedReasons, fault.Reason) && fault.Severity != SeverityCritical {
		return fmt.Errorf("alert_severity_rejected")
	}
	return nil
}

func safeIdentity(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' && character != ':' && character != '.' {
			return false
		}
	}
	return true
}

func digest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
