package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"axiom/internal/api/console"
	"axiom/internal/api/health"
	"axiom/internal/buildinfo"
)

type healthyDependency struct{}

func (healthyDependency) Ping(context.Context) error { return nil }

func TestRouterAppliesSecurityHeaders(t *testing.T) {
	handler := NewRouter(health.Options{
		Role: "api", Build: buildinfo.Current(), Dependency: healthyDependency{},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "style-src 'self'") ||
		!strings.Contains(policy, "script-src 'self'") ||
		strings.Contains(policy, "script-src 'self' 'unsafe-inline'") || strings.Contains(policy, "unsafe-eval") {
		t.Fatalf("unexpected content security policy %q", policy)
	}
}

func TestPanicRecoveryReturnsOnlyStableRedactedError(t *testing.T) {
	secret := "do-not-expose-secret-or-path-/run/secrets/example"
	handler := recoverPanics(securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(secret)
	})))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	request.Header.Set("X-Correlation-ID", "correlation-a11")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), secret) ||
		strings.Contains(response.Body.String(), "/run/secrets") || !strings.Contains(response.Body.String(), `"correlation_id":"correlation-a11"`) {
		t.Fatalf("panic response = %d %q", response.Code, response.Body.String())
	}
}

func TestA11RouterRegistersEveryRequiredMethodAndPath(t *testing.T) {
	handler := NewRouter(health.Options{
		Role: "api", Build: buildinfo.Current(), Dependency: healthyDependency{},
	}, console.Options{AllowedOrigins: []string{"http://localhost:4173"}})
	routes := []struct{ method, path string }{
		{http.MethodGet, "/health/live"},
		{http.MethodGet, "/health/ready"},
		{http.MethodPost, "/api/v1/session/login"},
		{http.MethodPost, "/api/v1/session/logout"},
		{http.MethodGet, "/api/v1/session/me"},
		{http.MethodGet, "/api/v1/system/status"},
		{http.MethodGet, "/api/v1/exchanges/binance/health"},
		{http.MethodGet, "/api/v1/exchanges/binance/instruments"},
		{http.MethodGet, "/api/v1/portfolios"},
		{http.MethodGet, "/api/v1/portfolios/portfolio-a11"},
		{http.MethodGet, "/api/v1/portfolios/portfolio-a11/journal"},
		{http.MethodGet, "/api/v1/risk/status"},
		{http.MethodPost, "/api/v1/risk/pause"},
		{http.MethodPost, "/api/v1/risk/resume"},
		{http.MethodGet, "/api/v1/strategies/trend"},
		{http.MethodGet, "/api/v1/strategies/trend/decisions"},
		{http.MethodPost, "/api/v1/backtests"},
		{http.MethodGet, "/api/v1/backtests/backtest-a11"},
		{http.MethodPost, "/api/v1/replays"},
		{http.MethodGet, "/api/v1/replays/replay-a11"},
		{http.MethodPost, "/api/v1/replays/replay-a11/pause"},
		{http.MethodPost, "/api/v1/replays/replay-a11/resume"},
		{http.MethodPost, "/api/v1/replays/replay-a11/step"},
		{http.MethodPost, "/api/v1/shadow-sessions"},
		{http.MethodPost, "/api/v1/shadow-sessions/shadow-a11/stop"},
		{http.MethodGet, "/api/v1/shadow-sessions/shadow-a11"},
		{http.MethodGet, "/api/v1/incidents"},
		{http.MethodGet, "/api/v1/incidents/incident-a11"},
		{http.MethodGet, "/api/v1/audit-events"},
		{http.MethodGet, "/api/v1/stream"},
	}
	for _, route := range routes {
		request := httptest.NewRequest(route.method, route.path, nil)
		request.Header.Set("Origin", "http://localhost:4173")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == http.StatusNotFound || response.Code == http.StatusMethodNotAllowed {
			t.Errorf("required route %s %s returned %d", route.method, route.path, response.Code)
		}
	}
}

func TestOperationalRouterExposesMetrics(t *testing.T) {
	handler := NewOperationalRouter(health.Options{
		Role: "api", Build: buildinfo.Current(), Dependency: healthyDependency{},
	}, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("metric 1\n")) }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || response.Body.String() != "metric 1\n" {
		t.Fatalf("metrics response = %d %q", response.Code, response.Body.String())
	}
}
