package observability

import (
	"fmt"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
)

func (metrics *Metrics) validateDimensions(
	d Dimensions,
	strategy, market bool,
) error {
	if strategy && (!slices.Contains(metrics.catalog.Strategies, d.Strategy) ||
		!slices.Contains(metrics.catalog.Modes, d.Mode)) {
		return fmt.Errorf("metric_label_rejected:strategy")
	}
	if market && (!slices.Contains(metrics.catalog.Exchanges, d.Exchange) ||
		!slices.Contains(metrics.catalog.Instruments, d.Instrument)) {
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
			if !(character >= 'a' && character <= 'z') &&
				!(character >= 'A' && character <= 'Z') &&
				!(character >= '0' && character <= '9') &&
				character != '_' && character != '-' {
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
	return MetricCatalog{
		Exchanges:   slices.Clone(source.Exchanges),
		Instruments: slices.Clone(source.Instruments),
		Strategies:  slices.Clone(source.Strategies),
		Modes:       slices.Clone(source.Modes),
	}
}

func counter(
	name, help string,
	variable []string,
	labels prometheus.Labels,
) *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: name, Help: help, ConstLabels: labels},
		variable,
	)
}

func gauge(
	name, help string,
	variable []string,
	labels prometheus.Labels,
) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels},
		variable,
	)
}

func histogram(
	name, help string,
	variable []string,
	labels prometheus.Labels,
) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: name, Help: help, ConstLabels: labels,
			Buckets: prometheus.DefBuckets,
		},
		variable,
	)
}
