package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
}
