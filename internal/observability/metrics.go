package observability

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
}

// NewMetrics builds an isolated registry with process collectors and the
// complete A5 metric contract. Catalog values are copied and validated once.
func NewMetrics(service string, catalog MetricCatalog) (*Metrics, error) {
	if service == "" || len(service) > 64 {
		return nil, fmt.Errorf("metrics_service_rejected")
	}
	catalog = cloneCatalog(catalog)
	for name, values := range map[string][]string{
		"exchange": catalog.Exchanges, "instrument": catalog.Instruments,
		"strategy": catalog.Strategies, "mode": catalog.Modes,
	} {
		if err := validateCatalog(name, values); err != nil {
			return nil, err
		}
	}
	labels := prometheus.Labels{"service": service}
	metrics := &Metrics{registry: prometheus.NewRegistry(), catalog: catalog}
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

	metrics.register()
	return metrics, nil
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
	)
}

// Handler returns the OpenMetrics-compatible scrape endpoint.
func (metrics *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// RecordWebSocketMessage increments a validated market stream count.
func (metrics *Metrics) RecordWebSocketMessage(d Dimensions) error {
	if err := metrics.validateDimensions(d, false, true); err != nil {
		return err
	}
	metrics.wsMessages.WithLabelValues(d.Exchange, d.Instrument).Inc()
	return nil
}

// RecordWebSocketEvent increments one closed WebSocket health reason.
func (metrics *Metrics) RecordWebSocketEvent(d Dimensions, reason Reason) error {
	if err := metrics.validateDimensions(d, false, true); err != nil {
		return err
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	metrics.wsFailures.WithLabelValues(d.Exchange, d.Instrument, string(reason)).Inc()
	return nil
}

// SetBookAge publishes nonnegative order-book generation age.
func (metrics *Metrics) SetBookAge(d Dimensions, age time.Duration) error {
	if err := metrics.validateDimensions(d, false, true); err != nil {
		return err
	}
	if age < 0 {
		return fmt.Errorf("metric_value_rejected:book_age")
	}
	metrics.bookAge.WithLabelValues(d.Exchange, d.Instrument).Set(age.Seconds())
	return nil
}

// SetQueue publishes one bounded queue depth and optional drop fact.
func (metrics *Metrics) SetQueue(queue string, depth int, dropped bool) error {
	if !slices.Contains([]string{"market", "persistence", "strategy", "alerts", "jobs"}, queue) || depth < 0 {
		return fmt.Errorf("metric_label_rejected:queue")
	}
	metrics.queueDepth.WithLabelValues(queue).Set(float64(depth))
	if dropped {
		metrics.queueDropped.WithLabelValues(queue, string(ReasonQueueFull)).Inc()
	}
	return nil
}

// ObserveStrategy records a complete strategy/risk evaluation.
func (metrics *Metrics) ObserveStrategy(d Dimensions, candidates int, rejected Reason, duration time.Duration) error {
	if err := metrics.validateDimensions(d, true, false); err != nil {
		return err
	}
	if candidates < 0 || duration < 0 {
		return fmt.Errorf("metric_value_rejected:strategy")
	}
	if rejected != "" {
		if err := validateReason(rejected); err != nil {
			return err
		}
	}
	metrics.strategyRuns.WithLabelValues(d.Strategy, d.Mode).Inc()
	metrics.strategyCandidates.WithLabelValues(d.Strategy, d.Mode).Set(float64(candidates))
	metrics.riskDuration.WithLabelValues(d.Strategy, d.Mode).Observe(duration.Seconds())
	if rejected != "" {
		metrics.strategyRejected.WithLabelValues(d.Strategy, d.Mode, string(rejected)).Inc()
	}
	return nil
}

// ObserveWebSocketLag records nonnegative public stream lag.
func (metrics *Metrics) ObserveWebSocketLag(d Dimensions, lag time.Duration) error {
	if err := metrics.validateDimensions(d, false, true); err != nil {
		return err
	}
	if lag < 0 {
		return fmt.Errorf("metric_value_rejected:websocket_lag")
	}
	metrics.wsLag.WithLabelValues(d.Exchange, d.Instrument).Observe(lag.Seconds())
	return nil
}

// RecordShadowFill increments a credential-free simulated fill outcome.
func (metrics *Metrics) RecordShadowFill(d Dimensions, state string) error {
	if err := metrics.validateDimensions(d, false, true); err != nil {
		return err
	}
	if !slices.Contains([]string{"filled", "partial", "rejected"}, state) {
		return fmt.Errorf("metric_label_rejected:fill_state")
	}
	metrics.shadowFills.WithLabelValues(d.Exchange, d.Instrument, state).Inc()
	return nil
}

// RecordReconciliation increments a virtual reconciliation mismatch.
func (metrics *Metrics) RecordReconciliation(exchange string, reason Reason) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) {
		return fmt.Errorf("metric_label_rejected:exchange")
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	metrics.reconciliation.WithLabelValues(exchange, string(reason)).Inc()
	return nil
}

// RecordJournalFailure increments an exact-journal failure reason.
func (metrics *Metrics) RecordJournalFailure(reason Reason) error {
	if err := validateReason(reason); err != nil {
		return err
	}
	metrics.journalFailures.WithLabelValues(string(reason)).Inc()
	return nil
}

// SetVirtualPortfolio accepts fixed microunits/parts-per-million so financial
// authority never depends on binary floating point. Conversion occurs only at
// the Prometheus presentation boundary.
func (metrics *Metrics) SetVirtualPortfolio(mode string, pnlMicrounits int64, drawdownPPM uint32) error {
	if !slices.Contains(metrics.catalog.Modes, mode) || drawdownPPM > 1_000_000 {
		return fmt.Errorf("metric_value_rejected:portfolio")
	}
	metrics.virtualPnL.WithLabelValues(mode).Set(float64(pnlMicrounits) / 1_000_000)
	metrics.virtualDrawdown.WithLabelValues(mode).Set(float64(drawdownPPM) / 1_000_000)
	return nil
}

// ObserveDatabase records a bounded database operation outcome.
func (metrics *Metrics) ObserveDatabase(operation string, duration time.Duration, failed bool) error {
	if !slices.Contains([]string{"connect", "ping", "read", "write", "transaction", "migration"}, operation) || duration < 0 {
		return fmt.Errorf("metric_label_rejected:database_operation")
	}
	metrics.databaseDuration.WithLabelValues(operation).Observe(duration.Seconds())
	if failed {
		metrics.databaseFailures.WithLabelValues(operation, string(ReasonPersistence)).Inc()
	}
	return nil
}

// SetDiskFree publishes free bytes for one configured storage class.
func (metrics *Metrics) SetDiskFree(storage string, bytes uint64) error {
	if !slices.Contains([]string{"market_data", "postgres", "backups", "prometheus"}, storage) {
		return fmt.Errorf("metric_label_rejected:storage")
	}
	metrics.diskFreeBytes.WithLabelValues(storage).Set(float64(bytes))
	return nil
}

// ObserveSimulation records a credential-free execution simulation duration.
func (metrics *Metrics) ObserveSimulation(mode string, duration time.Duration) error {
	if !slices.Contains(metrics.catalog.Modes, mode) || duration < 0 {
		return fmt.Errorf("metric_label_rejected:mode")
	}
	metrics.simulationDuration.WithLabelValues(mode).Observe(duration.Seconds())
	return nil
}

// ObserveREST records an allowlisted public REST operation.
func (metrics *Metrics) ObserveREST(exchange, operation string, duration time.Duration, failed bool) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) || !slices.Contains([]string{"metadata", "time", "candles", "trades", "depth"}, operation) || duration < 0 {
		return fmt.Errorf("metric_label_rejected:rest")
	}
	metrics.restDuration.WithLabelValues(exchange, operation).Observe(duration.Seconds())
	if failed {
		metrics.restFailures.WithLabelValues(exchange, operation).Inc()
	}
	return nil
}

// SetDependencyReady publishes one closed dependency health value.
func (metrics *Metrics) SetDependencyReady(dependency string, ready bool) error {
	if !slices.Contains([]string{"postgres", "disk", "clock", "fencing", "books", "queues"}, dependency) {
		return fmt.Errorf("metric_label_rejected:dependency")
	}
	value := 0.0
	if ready {
		value = 1
	}
	metrics.ready.WithLabelValues(dependency).Set(value)
	return nil
}

// SetOpenAlerts publishes a durable in-app alert count.
func (metrics *Metrics) SetOpenAlerts(severity string, reason Reason, count int) error {
	if !slices.Contains([]string{"info", "warning", "critical"}, severity) || count < 0 {
		return fmt.Errorf("metric_label_rejected:severity")
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	metrics.alerts.WithLabelValues(severity, string(reason)).Set(float64(count))
	return nil
}

func (metrics *Metrics) validateDimensions(d Dimensions, strategy, market bool) error {
	if strategy && (!slices.Contains(metrics.catalog.Strategies, d.Strategy) || !slices.Contains(metrics.catalog.Modes, d.Mode)) {
		return fmt.Errorf("metric_label_rejected:strategy")
	}
	if market && (!slices.Contains(metrics.catalog.Exchanges, d.Exchange) || !slices.Contains(metrics.catalog.Instruments, d.Instrument)) {
		return fmt.Errorf("metric_label_rejected:market")
	}
	return nil
}

func validateReason(reason Reason) error {
	if !slices.Contains(boundedReasons, reason) {
		return fmt.Errorf("metric_label_rejected:reason")
	}
	return nil
}

func validateCatalog(name string, values []string) error {
	if len(values) == 0 || len(values) > 256 {
		return fmt.Errorf("metric_catalog_rejected:%s", name)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || len(value) > 64 {
			return fmt.Errorf("metric_catalog_rejected:%s", name)
		}
		for _, character := range value {
			if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
				return fmt.Errorf("metric_catalog_rejected:%s", name)
			}
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("metric_catalog_rejected:%s", name)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func cloneCatalog(source MetricCatalog) MetricCatalog {
	return MetricCatalog{Exchanges: slices.Clone(source.Exchanges), Instruments: slices.Clone(source.Instruments), Strategies: slices.Clone(source.Strategies), Modes: slices.Clone(source.Modes)}
}

func counter(name, help string, variable []string, labels prometheus.Labels) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: labels}, variable)
}

func gauge(name, help string, variable []string, labels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels}, variable)
}

func histogram(name, help string, variable []string, labels prometheus.Labels) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, ConstLabels: labels, Buckets: prometheus.DefBuckets}, variable)
}
