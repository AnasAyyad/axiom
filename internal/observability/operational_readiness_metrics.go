package observability

import (
	"fmt"
	"sort"
	"time"
)

const operationalReadinessStrategyWindowSize = 4096

// SetOperationalReadinessCollectorLatency publishes collector latency computed
// from the real bounded collector statistics. Missing observations are never
// published, allowing the external observer to fail closed on absent metrics.
func (metrics *Metrics) SetOperationalReadinessCollectorLatency(
	decodeBookP99 time.Duration,
	resyncP95 time.Duration,
	observedAt time.Time,
) error {
	if decodeBookP99 < 0 || resyncP95 < 0 || observedAt.IsZero() {
		return fmt.Errorf("metric_value_rejected:operational_readiness_collector")
	}
	metrics.operationalReadinessDecodeBookP99.Set(float64(decodeBookP99) / float64(time.Millisecond))
	metrics.operationalReadinessResyncP95.Set(float64(resyncP95) / float64(time.Millisecond))
	metrics.operationalReadinessObservedAt.WithLabelValues("collector").Set(float64(observedAt.UTC().Unix()))
	return nil
}

// ObserveOperationalReadinessStrategyRisk records one real strategy-through-
// risk duration and publishes a bounded rolling p99 for the external observer.
func (metrics *Metrics) ObserveOperationalReadinessStrategyRisk(duration time.Duration, observedAt time.Time) error {
	if duration < 0 || observedAt.IsZero() {
		return fmt.Errorf("metric_value_rejected:operational_readiness_strategy")
	}
	metrics.operationalReadinessMutex.Lock()
	if len(metrics.operationalReadinessStrategyWindow) < operationalReadinessStrategyWindowSize {
		metrics.operationalReadinessStrategyWindow = append(metrics.operationalReadinessStrategyWindow, duration)
	} else {
		metrics.operationalReadinessStrategyWindow[metrics.operationalReadinessStrategyNext] = duration
		metrics.operationalReadinessStrategyNext =
			(metrics.operationalReadinessStrategyNext + 1) % operationalReadinessStrategyWindowSize
	}
	window := append([]time.Duration(nil), metrics.operationalReadinessStrategyWindow...)
	metrics.operationalReadinessMutex.Unlock()
	sort.Slice(window, func(left, right int) bool { return window[left] < window[right] })
	index := (99*len(window) + 99) / 100
	if index > 0 {
		index--
	}
	metrics.operationalReadinessStrategyRiskP99.Set(float64(window[index]) / float64(time.Millisecond))
	metrics.operationalReadinessObservedAt.WithLabelValues("strategy_risk").Set(float64(observedAt.UTC().Unix()))
	return nil
}
