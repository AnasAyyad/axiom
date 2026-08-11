package observability

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// MetricCatalog is the complete bounded label vocabulary for one process.
// Runtime identities and arbitrary text must never be added to this catalog.
type MetricCatalog struct {
	Exchanges   []string
	Instruments []string
	Strategies  []string
	Modes       []string
}

// Dimensions identifies one market/strategy metric using configured values.
type Dimensions struct {
	Exchange   string
	Instrument string
	Strategy   string
	Mode       string
}

// Reason is a closed operational reason-code label.
type Reason string

// Bounded reason codes. Never use error text, IDs, URLs, or file paths here.
const (
	ReasonDecode         Reason = "decode"
	ReasonSequenceGap    Reason = "sequence_gap"
	ReasonReconnect      Reason = "reconnect"
	ReasonQueueFull      Reason = "queue_full"
	ReasonStaleBook      Reason = "stale_book"
	ReasonClockDrift     Reason = "clock_drift"
	ReasonPersistence    Reason = "persistence"
	ReasonFencingLease   Reason = "fencing_lease"
	ReasonDiskPressure   Reason = "disk_pressure"
	ReasonReconciliation Reason = "reconciliation"
	ReasonJournal        Reason = "journal"
	ReasonRisk           Reason = "risk"
	ReasonUnsupported    Reason = "unsupported"
)

var boundedReasons = []Reason{
	ReasonDecode, ReasonSequenceGap, ReasonReconnect, ReasonQueueFull,
	ReasonStaleBook, ReasonClockDrift, ReasonPersistence, ReasonFencingLease,
	ReasonDiskPressure, ReasonReconciliation, ReasonJournal, ReasonRisk,
	ReasonUnsupported,
}

// Metrics owns the private Prometheus collectors for one service. Vector
// collectors are deliberately not exposed, so every label crosses validation.
type Metrics struct {
	registry *prometheus.Registry
	catalog  MetricCatalog

	wsMessages         *prometheus.CounterVec
	wsFailures         *prometheus.CounterVec
	bookAge            *prometheus.GaugeVec
	queueDepth         *prometheus.GaugeVec
	queueDropped       *prometheus.CounterVec
	strategyRuns       *prometheus.CounterVec
	strategyCandidates *prometheus.GaugeVec
	strategyRejected   *prometheus.CounterVec
	riskDuration       *prometheus.HistogramVec
	simulationDuration *prometheus.HistogramVec
	restDuration       *prometheus.HistogramVec
	restFailures       *prometheus.CounterVec
	wsLag              *prometheus.HistogramVec
	shadowFills        *prometheus.CounterVec
	reconciliation     *prometheus.CounterVec
	journalFailures    *prometheus.CounterVec
	virtualPnL         *prometheus.GaugeVec
	virtualDrawdown    *prometheus.GaugeVec
	databaseDuration   *prometheus.HistogramVec
	databaseFailures   *prometheus.CounterVec
	diskFreeBytes      *prometheus.GaugeVec
	alerts             *prometheus.GaugeVec
	ready              *prometheus.GaugeVec

	sandboxOrders                    *prometheus.CounterVec
	sandboxAnomalies                 *prometheus.CounterVec
	sandboxUnknown                   *prometheus.GaugeVec
	sandboxReconciliation            *prometheus.GaugeVec
	sandboxArms                      *prometheus.GaugeVec
	sandboxCapUsage                  *prometheus.GaugeVec
	sandboxCapRejections             *prometheus.CounterVec
	sandboxResets                    *prometheus.CounterVec
	sandboxEngineReady               *prometheus.GaugeVec
	sandboxEngineEvents              *prometheus.CounterVec
	sandboxRecovery                  *prometheus.HistogramVec
	criticalAlertLatency             *prometheus.HistogramVec
	sandboxQualificationSoakState    *prometheus.GaugeVec
	sandboxQualificationSoakDuration *prometheus.GaugeVec
	sandboxQualificationSoakFailures *prometheus.CounterVec
	sandboxQualificationMemoryTrend  *prometheus.GaugeVec

	evaluationStage         *prometheus.GaugeVec
	evaluationValidTime     *prometheus.GaugeVec
	evaluationRecording     *prometheus.GaugeVec
	evaluationFeedFreshness *prometheus.GaugeVec
	evaluationFeedHealthy   *prometheus.GaugeVec
	evaluationMembers       *prometheus.GaugeVec
	evaluationFinancial     *prometheus.GaugeVec
	evaluationFunnel        *prometheus.GaugeVec
	evaluationFailure       *prometheus.GaugeVec
}

// NewMetrics builds an isolated registry with process collectors and the
// complete observability metric contract. Catalog values are copied and validated once.
func NewMetrics(service string, catalog MetricCatalog) (*Metrics, error) {
	if service == "" || len(service) > 64 {
		return nil, fmt.Errorf("metrics_service_rejected")
	}
	catalog = cloneCatalog(catalog)
	if err := validateMetricCatalog(catalog); err != nil {
		return nil, err
	}
	labels := prometheus.Labels{"service": service}
	metrics := &Metrics{registry: prometheus.NewRegistry(), catalog: catalog}
	initializeObservabilityMetrics(metrics, labels)
	initializeSandboxQualificationMetrics(metrics, labels)
	initializeEvaluationMetrics(metrics, labels)
	metrics.register()
	return metrics, nil
}

func validateMetricCatalog(catalog MetricCatalog) error {
	for name, values := range map[string][]string{
		"exchange": catalog.Exchanges, "instrument": catalog.Instruments,
		"strategy": catalog.Strategies, "mode": catalog.Modes,
	} {
		if err := validateCatalog(name, values); err != nil {
			return err
		}
	}
	return nil
}

func initializeObservabilityMetrics(metrics *Metrics, labels prometheus.Labels) {
	metrics.wsMessages = counter("axiom_websocket_messages_total", "Validated WebSocket messages.", []string{"exchange", "instrument"}, labels)
	metrics.wsFailures = counter("axiom_websocket_events_total", "WebSocket health events by bounded reason.", []string{"exchange", "instrument", "reason"}, labels)
	metrics.bookAge = gauge("axiom_order_book_age_seconds", "Age of the active order-book generation.", []string{"exchange", "instrument"}, labels)
	metrics.queueDepth = gauge("axiom_event_queue_depth", "Current bounded event queue depth.", []string{"queue"}, labels)
	metrics.queueDropped = counter("axiom_event_queue_dropped_total", "Events dropped by a bounded queue.", []string{"queue", "reason"}, labels)
	metrics.strategyRuns = counter("axiom_strategy_evaluations_total", "Strategy evaluations.", []string{"strategy", "mode"}, labels)
	metrics.strategyCandidates = gauge("axiom_strategy_candidates", "Current strategy candidate count.", []string{"strategy", "mode"}, labels)
	metrics.strategyRejected = counter("axiom_strategy_rejections_total", "Strategy candidate rejections.", []string{"strategy", "mode", "reason"}, labels)
	metrics.riskDuration = histogram("axiom_risk_check_duration_seconds", "Risk-check duration.", []string{"strategy", "mode"}, labels)
	metrics.simulationDuration = histogram("axiom_execution_simulation_duration_seconds", "Simulation duration.", []string{"mode"}, labels)
	metrics.restDuration = histogram("axiom_exchange_rest_duration_seconds", "Public REST request duration.", []string{"exchange", "operation"}, labels)
	metrics.restFailures = counter("axiom_exchange_rest_failures_total", "Public REST failures by bounded operation.", []string{"exchange", "operation"}, labels)
	metrics.wsLag = histogram("axiom_websocket_lag_seconds", "WebSocket exchange-to-receipt lag.", []string{"exchange", "instrument"}, labels)
	metrics.shadowFills = counter("axiom_shadow_fills_total", "Shadow fills by bounded state.", []string{"exchange", "instrument", "state"}, labels)
	metrics.reconciliation = counter("axiom_reconciliation_mismatches_total", "Reconciliation mismatches.", []string{"exchange", "reason"}, labels)
	metrics.journalFailures = counter("axiom_journal_failures_total", "Journal failures by bounded reason.", []string{"reason"}, labels)
	metrics.virtualPnL = gauge("axiom_virtual_pnl_reporting_units", "Virtual portfolio P&L in fixed reporting units.", []string{"mode"}, labels)
	metrics.virtualDrawdown = gauge("axiom_virtual_drawdown_ratio", "Virtual portfolio drawdown ratio.", []string{"mode"}, labels)
	metrics.databaseDuration = histogram("axiom_database_operation_duration_seconds", "Database operation duration.", []string{"operation"}, labels)
	metrics.databaseFailures = counter("axiom_database_failures_total", "Database failures by bounded operation.", []string{"operation", "reason"}, labels)
	metrics.diskFreeBytes = gauge("axiom_disk_free_bytes", "Free bytes on a configured storage class.", []string{"storage"}, labels)
	metrics.alerts = gauge("axiom_alerts_open", "Open in-app alerts.", []string{"severity", "reason"}, labels)
	metrics.ready = gauge("axiom_dependency_ready", "Dependency readiness state (1 ready, 0 unavailable).", []string{"dependency"}, labels)
}

func initializeSandboxQualificationMetrics(metrics *Metrics, labels prometheus.Labels) {
	metrics.sandboxOrders = counter("axiom_sandbox_orders_total", "Test/demo order state observations.", []string{"exchange", "state"}, labels)
	metrics.sandboxAnomalies = counter("axiom_sandbox_order_anomalies_total", "Safety-critical test/demo order anomalies.", []string{"exchange", "kind"}, labels)
	metrics.sandboxUnknown = gauge("axiom_sandbox_unknown_orders", "Unknown order count and oldest age.", []string{"exchange", "measure"}, labels)
	metrics.sandboxReconciliation = gauge("axiom_sandbox_reconciliation_items", "Current reconciliation mismatch and suspense items.", []string{"exchange", "kind"}, labels)
	metrics.sandboxArms = gauge("axiom_sandbox_arms", "Current arm count and nearest expiry.", []string{"exchange", "state", "measure"}, labels)
	metrics.sandboxCapUsage = gauge("axiom_sandbox_cap", "Exact sandbox cap values at the presentation boundary.", []string{"scope", "measure"}, labels)
	metrics.sandboxCapRejections = counter("axiom_sandbox_cap_rejections_total", "Sandbox admission rejections by fixed cap.", []string{"scope"}, labels)
	metrics.sandboxResets = counter("axiom_sandbox_account_resets_total", "Account epoch reset incidents.", []string{"exchange", "state"}, labels)
	metrics.sandboxEngineReady = gauge("axiom_sandbox_engine_ready", "Credential-owning engine readiness.", []string{"exchange"}, labels)
	metrics.sandboxEngineEvents = counter("axiom_sandbox_engine_events_total", "Engine reconnect and restart observations.", []string{"exchange", "kind"}, labels)
	metrics.sandboxRecovery = histogram("axiom_sandbox_recovery_duration_seconds", "Sandbox reconciliation and recovery duration.", []string{"exchange", "operation"}, labels)
	metrics.criticalAlertLatency = histogram("axiom_critical_alert_latency_seconds", "Critical in-app alert creation latency.", []string{"reason"}, labels)
	metrics.sandboxQualificationSoakState = gauge("axiom_sandbox_qualification_soak_state", "sandbox qualification soak state and terminal verdict.", []string{"mode", "state"}, labels)
	metrics.sandboxQualificationSoakDuration = gauge("axiom_sandbox_qualification_soak_duration_seconds", "Observed sandbox qualification duration.", []string{"mode"}, labels)
	metrics.sandboxQualificationSoakFailures = counter("axiom_sandbox_qualification_soak_failures_total", "Sandbox qualification failures by closed reason.", []string{"reason"}, labels)
	metrics.sandboxQualificationMemoryTrend = gauge("axiom_sandbox_qualification_memory_trend_bytes", "Bounded resident-memory trend after warm-up.", []string{"window"}, labels)
}

func initializeEvaluationMetrics(metrics *Metrics, labels prometheus.Labels) {
	metrics.evaluationStage = gauge("axiom_evaluation_stage", "Current automated evaluation stage state.", []string{"stage", "state"}, labels)
	metrics.evaluationValidTime = gauge("axiom_evaluation_valid_time_seconds", "Accumulated valid evidence time.", []string{"phase"}, labels)
	metrics.evaluationRecording = gauge("axiom_evaluation_recording_bytes", "Campaign recording bytes, hard limit, and protected reserve.", []string{"measure"}, labels)
	metrics.evaluationFeedFreshness = gauge("axiom_evaluation_data_freshness_seconds", "Age of the latest campaign recorder event.", []string{"exchange", "instrument"}, labels)
	metrics.evaluationFeedHealthy = gauge("axiom_evaluation_feed_healthy", "Campaign feed eligibility (1 healthy, 0 paused).", []string{"exchange", "instrument"}, labels)
	metrics.evaluationMembers = gauge("axiom_evaluation_members", "Evaluation matrix and shadow members by bounded state.", []string{"strategy", "mode", "state"}, labels)
	metrics.evaluationFinancial = gauge("axiom_evaluation_financial_reporting_units", "Evaluation P and L and cost attribution in fixed reporting units.", []string{"strategy", "measure"}, labels)
	metrics.evaluationFunnel = gauge("axiom_evaluation_order_funnel", "Opportunity through simulated-fill funnel counts.", []string{"strategy", "measure"}, labels)
	metrics.evaluationFailure = gauge("axiom_evaluation_failure", "Active shared evaluation failure by bounded stage and reason.", []string{"stage", "reason"}, labels)
}

func (metrics *Metrics) register() {
	metrics.registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.wsMessages, metrics.wsFailures, metrics.bookAge, metrics.queueDepth,
		metrics.queueDropped, metrics.strategyRuns, metrics.strategyCandidates,
		metrics.strategyRejected, metrics.riskDuration, metrics.simulationDuration,
		metrics.restDuration, metrics.restFailures, metrics.wsLag, metrics.shadowFills, metrics.reconciliation,
		metrics.journalFailures, metrics.virtualPnL, metrics.virtualDrawdown,
		metrics.databaseDuration, metrics.databaseFailures, metrics.alerts, metrics.ready,
		metrics.diskFreeBytes,
		metrics.sandboxOrders, metrics.sandboxAnomalies, metrics.sandboxUnknown,
		metrics.sandboxReconciliation, metrics.sandboxArms,
		metrics.sandboxCapUsage, metrics.sandboxCapRejections,
		metrics.sandboxResets, metrics.sandboxEngineReady,
		metrics.sandboxEngineEvents, metrics.sandboxRecovery,
		metrics.criticalAlertLatency, metrics.sandboxQualificationSoakState,
		metrics.sandboxQualificationSoakDuration, metrics.sandboxQualificationSoakFailures, metrics.sandboxQualificationMemoryTrend,
		metrics.evaluationStage, metrics.evaluationValidTime, metrics.evaluationRecording,
		metrics.evaluationFeedFreshness, metrics.evaluationFeedHealthy, metrics.evaluationMembers,
		metrics.evaluationFinancial, metrics.evaluationFunnel, metrics.evaluationFailure,
	)
}
