package observability

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testMetrics(t *testing.T) *Metrics {
	t.Helper()
	metrics, err := NewMetrics("engine-shadow", MetricCatalog{
		Exchanges: []string{"binance"}, Instruments: []string{"BTCUSDT"},
		Strategies: []string{"trend"}, Modes: []string{"shadow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return metrics
}

func TestMetricsExposeBoundedContract(t *testing.T) {
	metrics := testMetrics(t)
	recordMetricFixtures(t, metrics)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(response.Body)
	encoded := string(body)
	for _, required := range []string{
		"axiom_websocket_messages_total", "axiom_order_book_age_seconds",
		"axiom_event_queue_depth", "axiom_strategy_evaluations_total",
		"axiom_websocket_lag_seconds", "axiom_shadow_fills_total",
		"axiom_virtual_pnl_reporting_units", "axiom_disk_free_bytes",
		`service="engine-shadow"`, `instrument="BTCUSDT"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("missing %q", required)
		}
	}
}

func recordMetricFixtures(t *testing.T, metrics *Metrics) {
	t.Helper()
	dimensions := Dimensions{Exchange: "binance", Instrument: "BTCUSDT", Strategy: "trend", Mode: "shadow"}
	if err := metrics.RecordWebSocketMessage(dimensions); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetBookAge(dimensions, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetQueue("market", 3, true); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveStrategy(dimensions, 2, ReasonRisk, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetDependencyReady("postgres", true); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveWebSocketLag(dimensions, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordShadowFill(dimensions, "filled"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordReconciliation("binance", ReasonReconciliation); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetVirtualPortfolio("shadow", 1_250_000, 25_000); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveDatabase("write", time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetDiskFree("market_data", 1024); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsRejectUnboundedLabels(t *testing.T) {
	metrics := testMetrics(t)
	for name, err := range map[string]error{
		"order identifier": metrics.RecordWebSocketMessage(Dimensions{Exchange: "binance", Instrument: "order_123456789"}),
		"arbitrary reason": metrics.RecordWebSocketEvent(Dimensions{Exchange: "binance", Instrument: "BTCUSDT"}, Reason("raw error text")),
		"arbitrary queue":  metrics.SetQueue("user-42", 1, false),
	} {
		if err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestMetricCatalogRejectsUnsafeAndDuplicateValues(t *testing.T) {
	for _, value := range []string{"https://example.invalid", "token=value", "same same"} {
		_, err := NewMetrics("api", MetricCatalog{Exchanges: []string{value}, Instruments: []string{"BTCUSDT"}, Strategies: []string{"trend"}, Modes: []string{"shadow"}})
		if err == nil {
			t.Fatalf("catalog value %q accepted", value)
		}
	}
}

func TestC6MetricsExposeCompleteBoundedContract(t *testing.T) {
	metrics := testMetrics(t)
	recordC6MetricFixtures(t, metrics)
	assertC6MetricNames(t, metrics)
	assertC6MetricLabelsRejected(t, metrics)
}

func recordC6MetricFixtures(t *testing.T, metrics *Metrics) {
	t.Helper()
	checkMetricCalls(t, map[string]error{
		"order":             metrics.RecordSandboxOrder("binance", "UNKNOWN"),
		"anomaly":           metrics.RecordSandboxOrderAnomaly("binance", "duplicate_create"),
		"unknown":           metrics.SetSandboxUnknown("binance", 1, 2*time.Second),
		"reconciliation":    metrics.SetSandboxReconciliation("binance", 1, 2),
		"arm":               metrics.SetSandboxArms("binance", "active", 1, time.Minute),
		"cap":               metrics.SetSandboxCap("daily", 5_000_000, 45_000_000, 50_000_000),
		"cap rejection":     metrics.RecordSandboxCapRejection("daily"),
		"reset":             metrics.RecordSandboxReset("binance", "OPEN"),
		"engine":            metrics.SetSandboxEngine("binance", true, "reconnect"),
		"recovery":          metrics.ObserveSandboxRecovery("binance", "unknown", time.Second),
		"alert":             metrics.ObserveCriticalAlert(ReasonRisk, 2*time.Second),
		"soak":              metrics.SetC6Soak("smoke", "SMOKE_PASSED", 2*time.Second),
		"failure":           metrics.RecordC6Failure("unresolved_unknown"),
		"recovery incident": metrics.SetC6Recovery("binance", "active", 1),
		"memory":            metrics.SetC6MemoryTrend("run", -1024),
	})
}

func checkMetricCalls(t *testing.T, calls map[string]error) {
	t.Helper()
	for name, err := range calls {
		if err != nil {
			t.Fatalf("%s metric failed: %v", name, err)
		}
	}
}

func assertC6MetricNames(t *testing.T, metrics *Metrics) {
	t.Helper()
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(
		response,
		httptest.NewRequest("GET", "/metrics", nil),
	)
	encoded := response.Body.String()
	for _, required := range []string{
		"axiom_sandbox_orders_total",
		"axiom_sandbox_order_anomalies_total",
		"axiom_sandbox_unknown_orders",
		"axiom_sandbox_reconciliation_items",
		"axiom_sandbox_arms",
		"axiom_sandbox_cap",
		"axiom_sandbox_cap_rejections_total",
		"axiom_sandbox_account_resets_total",
		"axiom_sandbox_engine_ready",
		"axiom_sandbox_engine_events_total",
		"axiom_sandbox_recovery_duration_seconds",
		"axiom_critical_alert_latency_seconds",
		"axiom_c6_soak_state",
		"axiom_c6_soak_duration_seconds",
		"axiom_c6_soak_failures_total",
		"axiom_c6_memory_trend_bytes",
		"axiom_c6_recovery_incidents",
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("missing C6 metric %q", required)
		}
	}
}

func assertC6MetricLabelsRejected(t *testing.T, metrics *Metrics) {
	t.Helper()
	for name, err := range map[string]error{
		"exchange":       metrics.RecordSandboxOrder("order-id-unbounded", "UNKNOWN"),
		"state":          metrics.RecordSandboxOrder("binance", "native-private-payload"),
		"failure":        metrics.RecordC6Failure("raw database error"),
		"memory window":  metrics.SetC6MemoryTrend("run-id-123", 1),
		"recovery state": metrics.SetC6Recovery("binance", "raw-error", 1),
	} {
		if err == nil {
			t.Fatalf("unbounded C6 %s label accepted", name)
		}
	}
}
