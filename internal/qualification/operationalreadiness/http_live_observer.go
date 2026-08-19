package operationalReadiness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var requiredRuntimeMetricRoles = []string{
	"api", "engine-shadow", "recorder", "backtest-worker",
	"binance-sandbox-engine", "bybit-sandbox-engine",
}

type RuntimeMetricTarget struct {
	Role             string
	URL              string
	MemoryLimitBytes uint64
}

type RuntimeHealthTarget struct {
	Role string
	URL  string
}

// HTTPRuntimeTelemetrySource reads only credential-free health and Prometheus
// endpoints on the private deployment network.
type HTTPRuntimeTelemetrySource struct {
	Client        *http.Client
	MetricTargets []RuntimeMetricTarget
	HealthTargets []RuntimeHealthTarget
}

func (source HTTPRuntimeTelemetrySource) Observe(ctx context.Context, observedAt time.Time) (RuntimeTelemetry, error) {
	if observedAt.IsZero() || validateRuntimeTargets(source.MetricTargets, source.HealthTargets) != nil {
		return RuntimeTelemetry{}, sourceFailure("runtime", "configuration", "", "configuration_invalid", false)
	}
	client := source.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	result := RuntimeTelemetry{ObservedAt: observedAt.UTC(), AllDeclaredLoadHealthy: true}
	for _, target := range source.HealthTargets {
		body, err := readBoundedHTTP(ctx, client, target.URL)
		if err != nil {
			return RuntimeTelemetry{}, sourceFailure("runtime", "health", target.Role, "endpoint_unavailable", true)
		}
		if isApplicationRuntimeRole(target.Role) && !strings.Contains(string(body), `"status":"ready"`) {
			return RuntimeTelemetry{}, sourceFailure("runtime", "health", target.Role, "not_ready", true)
		}
	}
	var collectorObservedAt, strategyObservedAt time.Time
	for _, target := range source.MetricTargets {
		body, err := readBoundedHTTP(ctx, client, target.URL)
		if err != nil {
			return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "endpoint_unavailable", true)
		}
		memory, found := prometheusMetric(body, "process_resident_memory_bytes", "")
		heap, heapFound := prometheusMetric(body, "go_memstats_heap_alloc_bytes", "")
		if !found || !heapFound {
			return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "required_metric_missing", true)
		}
		memoryBytes, err := boundedMetricUint64(memory)
		heapBytes, heapErr := boundedMetricUint64(heap)
		if err != nil || math.MaxUint64-result.ResidentMemoryBytes < memoryBytes ||
			heapErr != nil || math.MaxUint64-result.MemoryLimitBytes < target.MemoryLimitBytes {
			return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "metric_value_invalid", false)
		}
		result.ResidentMemoryBytes += memoryBytes
		result.MemoryLimitBytes += target.MemoryLimitBytes
		if memoryBytes > target.MemoryLimitBytes {
			result.AllDeclaredLoadHealthy = false
		}
		result.ServiceMemory = append(result.ServiceMemory, ServiceMemory{Role: target.Role,
			ResidentMemoryBytes: memoryBytes, HeapAllocBytes: heapBytes, MemoryLimitBytes: target.MemoryLimitBytes})
		switch target.Role {
		case "recorder":
			decode, decodeFound := prometheusMetric(body, "axiom_operational_readiness_decode_book_p99_milliseconds", "")
			resync, resyncFound := prometheusMetric(body, "axiom_operational_readiness_resync_p95_milliseconds", "")
			marker, markerFound := prometheusMetric(body, "axiom_operational_readiness_telemetry_observed_unixtime", `component="collector"`)
			if !decodeFound || !resyncFound || !markerFound {
				return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "collector_metric_missing", true)
			}
			if result.DecodeBookP99Millis, err = boundedMetricUint64(decode); err != nil {
				return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "collector_metric_invalid", false)
			}
			if result.ResyncP95Millis, err = boundedMetricUint64(resync); err != nil {
				return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "collector_metric_invalid", false)
			}
			collectorObservedAt = metricTime(marker)
		case "engine-shadow":
			strategy, strategyFound := prometheusMetric(body, "axiom_operational_readiness_strategy_risk_p99_milliseconds", "")
			marker, markerFound := prometheusMetric(body, "axiom_operational_readiness_telemetry_observed_unixtime", `component="strategy_risk"`)
			if !strategyFound || !markerFound {
				return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "strategy_metric_missing", true)
			}
			if result.StrategyRiskP99Millis, err = boundedMetricUint64(strategy); err != nil {
				return RuntimeTelemetry{}, sourceFailure("runtime", "metrics", target.Role, "strategy_metric_invalid", false)
			}
			strategyObservedAt = metricTime(marker)
		}
	}
	if !freshSource(collectorObservedAt, observedAt.UTC(), 30*time.Second) {
		return RuntimeTelemetry{}, sourceFailure("runtime", "freshness", "recorder", "collector_marker_stale", true)
	}
	if !freshSource(strategyObservedAt, observedAt.UTC(), 30*time.Second) {
		return RuntimeTelemetry{}, sourceFailure("runtime", "freshness", "engine-shadow", "strategy_marker_stale", true)
	}
	return result, nil
}

func validateRuntimeTargets(metrics []RuntimeMetricTarget, health []RuntimeHealthTarget) error {
	if len(metrics) != len(requiredRuntimeMetricRoles) || len(health) < len(requiredRuntimeMetricRoles) {
		return fmt.Errorf("runtime_target_set_invalid")
	}
	wanted := make(map[string]bool, len(requiredRuntimeMetricRoles))
	for _, role := range requiredRuntimeMetricRoles {
		wanted[role] = true
	}
	seenMetrics := make(map[string]bool, len(metrics))
	for _, target := range metrics {
		if !wanted[target.Role] || seenMetrics[target.Role] || target.MemoryLimitBytes == 0 || validateObserverURL(target.URL) != nil {
			return fmt.Errorf("runtime_metric_target_invalid")
		}
		seenMetrics[target.Role] = true
	}
	seenHealth := make(map[string]bool, len(health))
	for _, target := range health {
		if target.Role == "" || seenHealth[target.Role] || validateObserverURL(target.URL) != nil {
			return fmt.Errorf("runtime_health_target_invalid")
		}
		seenHealth[target.Role] = true
	}
	for role := range wanted {
		if !seenHealth[role] {
			return fmt.Errorf("runtime_health_target_incomplete")
		}
	}
	return nil
}

func validateObserverURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("observer_url_invalid")
	}
	return nil
}

func readBoundedHTTP(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("observer_http_status_invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || len(body) == 0 || len(body) >= 2<<20 {
		return nil, fmt.Errorf("observer_http_body_invalid")
	}
	return body, nil
}

func prometheusMetric(body []byte, name, requiredLabel string) (float64, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") ||
			!(strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"{")) ||
			(requiredLabel != "" && !strings.Contains(line, requiredLabel)) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		return value, err == nil
	}
	return 0, false
}

func boundedMetricUint64(value float64) (uint64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxUint64 {
		return 0, fmt.Errorf("metric_value_invalid")
	}
	return uint64(math.Ceil(value)), nil
}

func metricTime(value float64) time.Time {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > math.MaxInt64 {
		return time.Time{}
	}
	return time.Unix(int64(value), 0).UTC()
}

func isApplicationRuntimeRole(role string) bool {
	for _, candidate := range requiredRuntimeMetricRoles {
		if role == candidate {
			return true
		}
	}
	return false
}
