package console

import (
	"errors"
	"net/http"

	"axiom/internal/authentication"
)

func (handler *handler) registerReads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/status", handler.authorized(handler.systemStatus, "operations.read"))
	mux.HandleFunc("GET /api/v1/exchanges/binance/health", handler.authorized(handler.binanceHealth, "operations.read"))
	mux.HandleFunc("GET /api/v1/exchanges/binance/instruments", handler.authorized(handler.instruments, "operations.read"))
	mux.HandleFunc("GET /api/v1/portfolios", handler.authorized(handler.portfolios, "operations.read"))
	mux.HandleFunc("GET /api/v1/portfolios/{id}", handler.authorized(handler.portfolio, "operations.read"))
	mux.HandleFunc("GET /api/v1/portfolios/{id}/journal", handler.authorized(handler.journal, "operations.read"))
	mux.HandleFunc("GET /api/v1/risk/status", handler.authorized(handler.risk, "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/trend", handler.authorized(handler.trend, "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/trend/decisions", handler.authorized(handler.trendDecisions, "operations.read"))
	mux.HandleFunc("GET /api/v1/backtests/{id}", handler.authorized(handler.job, "operations.read"))
	mux.HandleFunc("GET /api/v1/replays/{id}", handler.authorized(handler.job, "operations.read"))
	mux.HandleFunc("GET /api/v1/shadow-sessions/{id}", handler.authorized(handler.shadow, "operations.read"))
	mux.HandleFunc("GET /api/v1/incidents", handler.authorized(handler.incidents, "operations.read"))
	mux.HandleFunc("GET /api/v1/incidents/{id}", handler.authorized(handler.incident, "operations.read"))
	mux.HandleFunc("GET /api/v1/audit-events", handler.authorized(handler.audit, "operations.read"))
}

func (handler *handler) readUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.Read != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable, "projection_unavailable", "Authoritative projection unavailable")
	return true
}

func (handler *handler) systemStatus(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.SystemStatus(request.Context())
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) binanceHealth(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.BinanceHealth(request.Context())
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) instruments(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Instruments(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) portfolios(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Portfolios(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) portfolio(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Portfolio(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) journal(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Journal(request.Context(), request.PathValue("id"), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) risk(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Risk(request.Context())
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) trend(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Trend(request.Context())
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) trendDecisions(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.TrendDecisions(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) job(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Job(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) shadow(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Shadow(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) incidents(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Incidents(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) incident(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	raw := authentication.RequirePermission(principal, "incident.raw") == nil
	value, err := handler.options.Read.Incident(request.Context(), request.PathValue("id"), raw)
	handler.writeRead(writer, request, value, err)
}
func (handler *handler) audit(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.readUnavailable(writer, request) {
		return
	}
	raw := authentication.RequirePermission(principal, "audit.raw") == nil
	value, err := handler.options.Read.Audit(request.Context(), request.URL.Query().Get("cursor"), limit, raw)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) writeRead(writer http.ResponseWriter, request *http.Request, value any, err error) {
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusOK, value)
}

func (handler *handler) writeServiceError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		handler.writeError(writer, request, http.StatusNotFound, "not_found", "Resource not found")
	case errors.Is(err, ErrIdempotencyConflict):
		handler.writeError(writer, request, http.StatusConflict, "idempotency_conflict", "Idempotency key was already used with a different payload")
	case errors.Is(err, ErrConflict):
		handler.writeError(writer, request, http.StatusConflict, "conflict", "Request conflicts with current state")
	case errors.Is(err, ErrPrecondition):
		handler.writeError(writer, request, http.StatusPreconditionFailed, "precondition_failed", "Safety prerequisites are not satisfied")
	case errors.Is(err, ErrQuota):
		handler.writeError(writer, request, http.StatusTooManyRequests, "quota_exceeded", "Durable job quota exceeded")
	case errors.Is(err, ErrInvalidRequest):
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request is invalid")
	case errors.Is(err, ErrCursorExpired):
		handler.writeError(writer, request, http.StatusGone, "cursor_expired", "Resume cursor expired; refresh the authoritative snapshot")
	default:
		handler.writeError(writer, request, http.StatusServiceUnavailable, "service_unavailable", "Service temporarily unavailable")
	}
}
