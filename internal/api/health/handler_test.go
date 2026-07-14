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
		Lifecycle: func() generated.SystemStatusLifecycleState { return generated.SystemStatusLifecycleStateREADYPAUSED },
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

func TestDetailedHealthRequiresOpaqueBearerWithoutLeakingIt(t *testing.T) {
	bearer := healthBearerFixture()
	authorize, err := NewBearerAuthorizer(bearer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, Options{Role: "api", Build: buildinfo.Current(), Dependency: dependency{}, Authorize: authorize})

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Header.Set("Authorization", "Bearer "+bearer+"-wrong")
	mux.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized || strings.Contains(unauthorized.Body.String(), bearer) {
		t.Fatalf("unsafe unauthorized response: %d %q", unauthorized.Code, unauthorized.Body.String())
	}

	authorized := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Header.Set("Authorization", "Bearer "+bearer)
	mux.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"name":"postgres"`) ||
		!strings.Contains(authorized.Body.String(), `"real_trading_enabled":false`) {
		t.Fatalf("detailed health response: %d %s", authorized.Code, authorized.Body.String())
	}
}

func TestDetailedHealthRedactsDependencyFailure(t *testing.T) {
	bearer := healthBearerFixture()
	authorize, err := NewBearerAuthorizer(bearer)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, Options{Role: "api", Dependency: dependency{errors.New("database secret detail")}, Authorize: authorize})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Header.Set("Authorization", "Bearer "+bearer)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "secret detail") ||
		!strings.Contains(response.Body.String(), "required_dependency_unavailable") {
		t.Fatalf("unsafe dependency response: %d %s", response.Code, response.Body.String())
	}
}

func healthBearerFixture() string {
	return strings.Repeat("test-only-", 4)
}
