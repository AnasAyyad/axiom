package observability

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var evaluationStages = []string{"HISTORICAL_IMPORT", "EXISTING_DATA_AUDIT", "RECORDER_ROTATION",
	"RECORDER_QUALIFICATION", "BACKTEST_MATRIX", "REPLAY_MATRIX", "CANDIDATE_SELECTION",
	"COMBINED_SHADOW", "FINAL_REPORT", "NONE"}
var evaluationStates = []string{"PENDING", "QUEUED", "RUNNING", "PAUSED_RECOVERABLE", "COMPLETED",
	"SUCCEEDED", "FAILED", "EXCLUDED", "BLOCKED", "CANCELED", "PARTIAL"}

// ResetEvaluationProjection clears only bounded evaluation gauges before one
// authoritative database snapshot is published. Counters elsewhere are not
// reset.
func (metrics *Metrics) ResetEvaluationProjection() {
	metrics.evaluationStage.Reset()
	metrics.evaluationValidTime.Reset()
	metrics.evaluationRecording.Reset()
	metrics.evaluationFeedFreshness.Reset()
	metrics.evaluationFeedHealthy.Reset()
	metrics.evaluationMembers.Reset()
	metrics.evaluationFinancial.Reset()
	metrics.evaluationFunnel.Reset()
	metrics.evaluationFailure.Reset()
}

// SetEvaluationCampaign records low-cardinality campaign stage and valid-time
// progress without labeling individual campaign IDs.
func (metrics *Metrics) SetEvaluationCampaign(stage, state string, validRecording,
	validShadow, recorded, limit, reserve int64) error {
	if !slices.Contains(evaluationStages, stage) || !slices.Contains(evaluationStates, state) ||
		validRecording < 0 || validShadow < 0 || recorded < 0 || limit <= 0 || reserve < 0 || recorded > limit {
		return fmt.Errorf("metric_value_rejected:evaluation_campaign")
	}
	metrics.evaluationStage.WithLabelValues(stage, state).Set(1)
	metrics.evaluationValidTime.WithLabelValues("recording").Set(float64(validRecording))
	metrics.evaluationValidTime.WithLabelValues("shadow").Set(float64(validShadow))
	metrics.evaluationRecording.WithLabelValues("recorded").Set(float64(recorded))
	metrics.evaluationRecording.WithLabelValues("limit").Set(float64(limit))
	metrics.evaluationRecording.WithLabelValues("shadow_reserved").Set(float64(reserve))
	return nil
}

// SetEvaluationFeed records fixed-universe feed freshness and health.
func (metrics *Metrics) SetEvaluationFeed(exchange, instrument string, age time.Duration, healthy bool) error {
	if !slices.Contains(metrics.catalog.Exchanges, exchange) || !slices.Contains(metrics.catalog.Instruments, instrument) || age < 0 {
		return fmt.Errorf("metric_value_rejected:evaluation_feed")
	}
	value := 0.0
	if healthy {
		value = 1
	}
	metrics.evaluationFeedFreshness.WithLabelValues(exchange, instrument).Set(age.Seconds())
	metrics.evaluationFeedHealthy.WithLabelValues(exchange, instrument).Set(value)
	return nil
}

// SetEvaluationMembers records bounded strategy-member state counts.
func (metrics *Metrics) SetEvaluationMembers(strategy, mode, state string, count int) error {
	if !slices.Contains(metrics.catalog.Strategies, strategy) || !slices.Contains(metrics.catalog.Modes, mode) ||
		!slices.Contains(evaluationStates, state) || count < 0 {
		return fmt.Errorf("metric_value_rejected:evaluation_members")
	}
	metrics.evaluationMembers.WithLabelValues(strategy, mode, state).Set(float64(count))
	return nil
}

// SetEvaluationFinancial records aggregate simulated ledger results and costs.
func (metrics *Metrics) SetEvaluationFinancial(strategy, measure string, microunits int64) error {
	if !slices.Contains(metrics.catalog.Strategies, strategy) || !slices.Contains([]string{
		"net", "gross_profit", "gross_loss", "fees", "spread", "slippage", "latency", "recovery",
	}, measure) {
		return fmt.Errorf("metric_value_rejected:evaluation_financial")
	}
	metrics.evaluationFinancial.WithLabelValues(strategy, measure).Set(float64(microunits) / 1_000_000)
	return nil
}

// SetEvaluationFunnel records bounded opportunity-to-fill totals.
func (metrics *Metrics) SetEvaluationFunnel(strategy, measure string, count int64) error {
	if !slices.Contains(metrics.catalog.Strategies, strategy) || !slices.Contains([]string{
		"opportunities", "accepted", "orders", "filled", "partial", "missed", "canceled", "expired", "rejected",
	}, measure) || count < 0 {
		return fmt.Errorf("metric_value_rejected:evaluation_funnel")
	}
	metrics.evaluationFunnel.WithLabelValues(strategy, measure).Set(float64(count))
	return nil
}

// SetEvaluationFailure records one stable reason without dynamic identifiers.
func (metrics *Metrics) SetEvaluationFailure(stage string, reason Reason) error {
	if !slices.Contains(evaluationStages, stage) {
		return fmt.Errorf("metric_label_rejected:evaluation_stage")
	}
	if err := validateReason(reason); err != nil {
		return err
	}
	metrics.evaluationFailure.WithLabelValues(stage, string(reason)).Set(1)
	return nil
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
