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
