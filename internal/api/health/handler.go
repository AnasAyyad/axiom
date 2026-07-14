package health

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/buildinfo"
)

const readinessTimeout = 2 * time.Second

// Dependency is the narrow readiness contract for a required dependency.
type Dependency interface {
	Ping(context.Context) error
}

// LifecycleState returns the current process lifecycle state.
type LifecycleState func() generated.SystemStatusLifecycleState

// Options are immutable dependencies for the health handlers.
type Options struct {
	Role       string
	Build      buildinfo.Info
	Dependency Dependency
	Lifecycle  LifecycleState
	Authorize  func(*http.Request) bool
}

// NewBearerAuthorizer returns a constant-time authorizer that retains only a
// SHA-256 digest of the file-backed operational token.
func NewBearerAuthorizer(token string) (func(*http.Request) bool, error) {
	if len(token) < 32 || len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
		return nil, fmt.Errorf("health_token_rejected")
	}
	wanted := sha256.Sum256([]byte(token))
	return func(request *http.Request) bool {
		const prefix = "Bearer "
		header := request.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) || len(header)-len(prefix) > 4096 {
			return false
		}
		actual := sha256.Sum256([]byte(strings.TrimPrefix(header, prefix)))
		return subtle.ConstantTimeCompare(actual[:], wanted[:]) == 1
	}, nil
}

// Register adds all A1 health and system-information routes to mux.
func Register(mux *http.ServeMux, options Options) {
	mux.HandleFunc("/health/live", getOnly(liveness(options)))
	mux.HandleFunc("/health/ready", getOnly(readiness(options)))
	mux.HandleFunc("/api/v1/system/version", getOnly(version(options)))
	mux.HandleFunc("/api/v1/system/build", getOnly(build(options)))
	mux.HandleFunc("/api/v1/system/status", getOnly(status(options)))
	mux.HandleFunc("/api/v1/system/health", getOnly(detailed(options)))
}

func detailed(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if options.Authorize == nil || !options.Authorize(request) {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="operational-health"`)
			writer.Header().Set("Cache-Control", "no-store")
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		state := generated.SystemStatusLifecycleStateSTARTING
		if options.Lifecycle != nil {
			state = options.Lifecycle()
		}
		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()
		status, componentStatus := generated.DetailedHealthResponseStatusReady, generated.HealthComponentStatusReady
		statusCode := http.StatusOK
		var reason *generated.HealthComponentReasonCode
		if options.Dependency == nil || options.Dependency.Ping(ctx) != nil {
			status, componentStatus, statusCode = generated.DetailedHealthResponseStatusNotReady, generated.HealthComponentStatusNotReady, http.StatusServiceUnavailable
			value := generated.RequiredDependencyUnavailable
			reason = &value
		}
		writeJSON(writer, statusCode, generated.DetailedHealthResponse{
			Status: status, Role: options.Role, LifecycleState: generated.DetailedHealthResponseLifecycleState(state),
			RealTradingEnabled: generated.DetailedHealthResponseRealTradingEnabled(false),
			Components:         []generated.HealthComponent{{Name: generated.Postgres, Status: componentStatus, ReasonCode: reason}},
		})
	}
}

func liveness(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, generated.HealthResponse{
			Status: generated.Live,
			Role:   options.Role,
			Phase:  generated.HealthResponsePhaseA1,
		})
	}
}

func readiness(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()
		if options.Dependency == nil || options.Dependency.Ping(ctx) != nil {
			reason := "required_dependency_unavailable"
			writeJSON(writer, http.StatusServiceUnavailable, generated.HealthResponse{
				Status: generated.NotReady, Role: options.Role,
				Phase: generated.HealthResponsePhaseA1, ReasonCode: &reason,
			})
			return
		}
		writeJSON(writer, http.StatusOK, generated.HealthResponse{
			Status: generated.Ready, Role: options.Role, Phase: generated.HealthResponsePhaseA1,
		})
	}
}

func version(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, generated.VersionResponse{Version: options.Build.Version})
	}
}

func build(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, generated.BuildInformation{
			Version: options.Build.Version, Commit: options.Build.Commit,
			BuiltAt: options.Build.BuiltAt, GoVersion: options.Build.GoVersion,
			Dirty: options.Build.Dirty,
		})
	}
}

func status(options Options) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		state := generated.SystemStatusLifecycleStateSTARTING
		if options.Lifecycle != nil {
			state = options.Lifecycle()
		}
		writeJSON(writer, http.StatusOK, generated.SystemStatus{
			Release: generated.V1A, Phase: generated.SystemStatusPhaseA1,
			Role: options.Role, LifecycleState: state,
			StrategyActivation: generated.Unavailable, RealTradingEnabled: generated.SystemStatusRealTradingEnabledFalse,
		})
	}
}

func getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method_not_allowed", http.StatusMethodNotAllowed)
			return
		}
		next(writer, request)
	}
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}
