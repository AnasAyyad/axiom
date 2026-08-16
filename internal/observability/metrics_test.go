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
		"axiom_evaluation_stage", "axiom_evaluation_valid_time_seconds",
		"axiom_evaluation_recording_bytes", "axiom_evaluation_data_freshness_seconds",
		"axiom_evaluation_members", "axiom_evaluation_financial_reporting_units",
		"axiom_evaluation_order_funnel", "axiom_evaluation_failure",
		"axiom_operational_readiness_decode_book_p99_milliseconds",
		"axiom_operational_readiness_strategy_risk_p99_milliseconds",
		"axiom_operational_readiness_resync_p95_milliseconds",
		"axiom_operational_readiness_telemetry_observed_unixtime",
		`service="engine-shadow"`, `instrument="BTCUSDT"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("missing %q", required)
		}
	}
}

func recordMetricFixtures(t *testing.T, metrics *Metrics) {
	t.Helper()
	recordBaseMetricFixtures(t, metrics)
	recordEvaluationMetricFixtures(t, metrics)
}

func recordBaseMetricFixtures(t *testing.T, metrics *Metrics) {
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
	if err := metrics.SetOperationalReadinessCollectorLatency(
		9*time.Millisecond, 14*time.Second, time.Now().UTC(),
	); err != nil {
		t.Fatal(err)
	}
	if err := metrics.ObserveOperationalReadinessStrategyRisk(20*time.Millisecond, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func recordEvaluationMetricFixtures(t *testing.T, metrics *Metrics) {
	t.Helper()
	metrics.ResetEvaluationProjection()
	if err := metrics.SetEvaluationCampaign("COMBINED_SHADOW", "RUNNING", 259_200, 60, 1024, 2048, 512); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetEvaluationFeed("binance", "BTCUSDT", time.Second, true); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetEvaluationMembers("trend", "shadow", "RUNNING", 1); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetEvaluationFinancial("trend", "net", 1_000_000); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetEvaluationFunnel("trend", "filled", 1); err != nil {
		t.Fatal(err)
	}
	if err := metrics.SetEvaluationFailure("COMBINED_SHADOW", ReasonRisk); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsRejectUnboundedLabels(t *testing.T) {
	metrics := testMetrics(t)
	for name, err := range map[string]error{
		"order identifier":  metrics.RecordWebSocketMessage(Dimensions{Exchange: "binance", Instrument: "order_123456789"}),
		"arbitrary reason":  metrics.RecordWebSocketEvent(Dimensions{Exchange: "binance", Instrument: "BTCUSDT"}, Reason("raw error text")),
		"arbitrary queue":   metrics.SetQueue("user-42", 1, false),
		"campaign id stage": metrics.SetEvaluationCampaign("campaign-123", "RUNNING", 0, 0, 0, 1, 0),
		"arbitrary member":  metrics.SetEvaluationMembers("strategy-user-42", "shadow", "RUNNING", 1),
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

func TestSandboxQualificationMetricsExposeCompleteBoundedContract(t *testing.T) {
	metrics := testMetrics(t)
	recordSandboxQualificationMetricFixtures(t, metrics)
	assertSandboxQualificationMetricNames(t, metrics)
	assertSandboxQualificationMetricLabelsRejected(t, metrics)
}

func recordSandboxQualificationMetricFixtures(t *testing.T, metrics *Metrics) {
	t.Helper()
	checkMetricCalls(t, map[string]error{
		"order":          metrics.RecordSandboxOrder("binance", "UNKNOWN"),
		"anomaly":        metrics.RecordSandboxOrderAnomaly("binance", "duplicate_create"),
		"unknown":        metrics.SetSandboxUnknown("binance", 1, 2*time.Second),
		"reconciliation": metrics.SetSandboxReconciliation("binance", 1, 2),
		"arm":            metrics.SetSandboxArms("binance", "active", 1, time.Minute),
		"cap":            metrics.SetSandboxCap("daily", 5_000_000, 45_000_000, 50_000_000),
		"cap rejection":  metrics.RecordSandboxCapRejection("daily"),
		"reset":          metrics.RecordSandboxReset("binance", "OPEN"),
		"engine":         metrics.SetSandboxEngine("binance", true, "reconnect"),
		"recovery":       metrics.ObserveSandboxRecovery("binance", "unknown", time.Second),
		"alert":          metrics.ObserveCriticalAlert(ReasonRisk, 2*time.Second),
		"soak":           metrics.SetSandboxQualificationSoak("smoke", "SMOKE_PASSED", 2*time.Second),
		"failure":        metrics.RecordSandboxQualificationFailure("unresolved_unknown"),
		"memory":         metrics.SetSandboxQualificationMemoryTrend("run", -1024),
		"recovery incident": metrics.SetSandboxQualificationRecovery(
			"binance", "active", 1,
		),
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

func assertSandboxQualificationMetricNames(t *testing.T, metrics *Metrics) {
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
		"axiom_sandbox_qualification_soak_state",
		"axiom_sandbox_qualification_soak_duration_seconds",
		"axiom_sandbox_qualification_soak_failures_total",
		"axiom_sandbox_qualification_memory_trend_bytes",
		"axiom_sandbox_qualification_recovery_incidents",
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("missing sandbox qualification metric %q", required)
		}
	}
}

func assertSandboxQualificationMetricLabelsRejected(t *testing.T, metrics *Metrics) {
	t.Helper()
	for name, err := range map[string]error{
		"exchange":      metrics.RecordSandboxOrder("order-id-unbounded", "UNKNOWN"),
		"state":         metrics.RecordSandboxOrder("binance", "native-private-payload"),
		"failure":       metrics.RecordSandboxQualificationFailure("raw database error"),
		"memory window": metrics.SetSandboxQualificationMemoryTrend("run-id-123", 1),
		"recovery state": metrics.SetSandboxQualificationRecovery(
			"binance", "raw-error", 1,
		),
	} {
		if err == nil {
			t.Fatalf("unbounded sandbox qualification %s label accepted", name)
		}
	}
}
