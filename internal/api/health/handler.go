package health

import (
	"context"
	"encoding/json"
	"net/http"
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
}

// Register adds all A1 health and system-information routes to mux.
func Register(mux *http.ServeMux, options Options) {
	mux.HandleFunc("/health/live", getOnly(liveness(options)))
	mux.HandleFunc("/health/ready", getOnly(readiness(options)))
	mux.HandleFunc("/api/v1/system/version", getOnly(version(options)))
	mux.HandleFunc("/api/v1/system/build", getOnly(build(options)))
	mux.HandleFunc("/api/v1/system/status", getOnly(status(options)))
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
		state := generated.STARTING
		if options.Lifecycle != nil {
			state = options.Lifecycle()
		}
		writeJSON(writer, http.StatusOK, generated.SystemStatus{
			Release: generated.V1A, Phase: generated.SystemStatusPhaseA1,
			Role: options.Role, LifecycleState: state,
			StrategyActivation: generated.Unavailable, RealTradingEnabled: generated.False,
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
