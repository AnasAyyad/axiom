package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/buildinfo"
)

type dependency struct{ err error }

func (item dependency) Ping(context.Context) error { return item.err }

func TestReadinessReflectsDependency(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "ready", want: http.StatusOK},
		{name: "unready", err: errors.New("fixture"), want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux, Options{Role: "api", Build: buildinfo.Current(), Dependency: dependency{test.err}})
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if strings.Contains(response.Body.String(), "fixture") {
				t.Fatal("dependency error leaked")
			}
		})
	}
}

func TestSystemStatusHardCodesSafetyBoundary(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Options{
		Role: "api", Build: buildinfo.Current(), Dependency: dependency{},
		Lifecycle: func() generated.SystemStatusLifecycleState { return generated.READYPAUSED },
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
	if !strings.Contains(response.Body.String(), `"real_trading_enabled":false`) {
		t.Fatalf("unsafe status body: %s", response.Body.String())
	}
}

func TestHealthRejectsMutationMethod(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Options{Role: "api", Build: buildinfo.Current(), Dependency: dependency{}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/health/live", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", response.Code)
	}
}
