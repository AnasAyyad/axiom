package console

import (
	"net/http"

	"axiom/internal/authentication"
)

func (handler *handler) registerD4(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/reports/{id}",
		handler.authorized(handler.d4Report, "operations.read"))
	mux.HandleFunc("GET /api/v1/report-schedules",
		handler.authorized(handler.d4ReportSchedules, "operations.read"))
	mux.HandleFunc("GET /api/v1/alerts/{id}",
		handler.authorized(handler.d4Alert, "operations.read"))
	mux.HandleFunc("GET /api/v1/alert-routes",
		handler.authorized(handler.d4AlertRoutes, "operations.read"))
	mux.HandleFunc("GET /api/v1/audit-verification",
		handler.authorized(handler.d4AuditVerification, "operations.read"))
	mux.HandleFunc("POST /api/v1/incidents",
		handler.authorizedMutation(handler.createD4Incident, "incident.write"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/updates",
		handler.authorizedMutation(handler.updateD4Incident, "incident.write"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/evidence-bundles",
		handler.authorizedMutation(handler.createD4IncidentBundle, "artifacts.read"))
	mux.HandleFunc("POST /api/v1/report-schedules",
		handler.authorizedMutation(handler.createD4ReportSchedule, "research.control"))
	mux.HandleFunc("POST /api/v1/report-schedules/{id}/transitions",
		handler.authorizedMutation(handler.transitionD4ReportSchedule, "research.control"))
	mux.HandleFunc("POST /api/v1/alerts/{id}/escalate",
		handler.authorizedMutation(handler.escalateD4Alert, "alert.write"))
	mux.HandleFunc("POST /api/v1/alert-routes/{id}/test",
		handler.authorizedMutation(handler.testD4AlertRoute, "alert.write"))
}

func (handler *handler) d4Unavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.D4Read != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"d4_projection_unavailable", "Operational evidence projection unavailable")
	return true
}

func (handler *handler) d4Report(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.d4Unavailable(writer, request) {
		return
	}
	value, err := handler.options.D4Read.D4Report(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) d4ReportSchedules(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	query, err := d1ListQuery(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.d4Unavailable(writer, request) {
		return
	}
	value, err := handler.options.D4Read.D4ReportSchedules(request.Context(), query)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) d4Alert(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.d4Unavailable(writer, request) {
		return
	}
	value, err := handler.options.D4Read.D4Alert(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) d4AlertRoutes(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.d4Unavailable(writer, request) {
		return
	}
	value, err := handler.options.D4Read.D4AlertRoutes(request.Context())
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) d4AuditVerification(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.d4Unavailable(writer, request) {
		return
	}
	value, err := handler.options.D4Read.D4AuditVerification(request.Context())
	handler.writeRead(writer, request, value, err)
}
