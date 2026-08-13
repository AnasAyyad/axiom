package console

import (
	"net/http"

	"axiom/internal/authentication"
)

func (handler *handler) registerOperationalEvidence(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/reports/{id}",
		handler.authorized(handler.operationalEvidenceReport, "operations.read"))
	mux.HandleFunc("GET /api/v1/report-schedules",
		handler.authorized(handler.operationalEvidenceReportSchedules, "operations.read"))
	mux.HandleFunc("GET /api/v1/alerts/{id}",
		handler.authorized(handler.operationalEvidenceAlert, "operations.read"))
	mux.HandleFunc("GET /api/v1/alert-routes",
		handler.authorized(handler.operationalEvidenceAlertRoutes, "operations.read"))
	mux.HandleFunc("GET /api/v1/audit-verification",
		handler.authorized(handler.operationalEvidenceAuditVerification, "operations.read"))
	mux.HandleFunc("POST /api/v1/incidents",
		handler.authorizedMutation(handler.createOperationalEvidenceIncident, "incident.write"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/updates",
		handler.authorizedMutation(handler.updateOperationalEvidenceIncident, "incident.write"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/evidence-bundles",
		handler.authorizedMutation(handler.createOperationalEvidenceIncidentBundle, "artifacts.read"))
	mux.HandleFunc("POST /api/v1/report-schedules",
		handler.authorizedMutation(handler.createOperationalEvidenceReportSchedule, "research.control"))
	mux.HandleFunc("POST /api/v1/report-schedules/{id}/transitions",
		handler.authorizedMutation(handler.transitionOperationalEvidenceReportSchedule, "research.control"))
	mux.HandleFunc("POST /api/v1/alerts/{id}/escalate",
		handler.authorizedMutation(handler.escalateOperationalEvidenceAlert, "alert.write"))
	mux.HandleFunc("POST /api/v1/alert-routes/{id}/test",
		handler.authorizedMutation(handler.testOperationalEvidenceAlertRoute, "alert.write"))
}

func (handler *handler) operationalEvidenceUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.OperationalEvidenceRead != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"operational_evidence_projection_unavailable", "Operational evidence projection unavailable")
	return true
}

func (handler *handler) operationalEvidenceReport(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.operationalEvidenceUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OperationalEvidenceRead.OperationalEvidenceReport(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) operationalEvidenceReportSchedules(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	query, err := ownerControlListQuery(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	if handler.operationalEvidenceUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OperationalEvidenceRead.OperationalEvidenceReportSchedules(request.Context(), query)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) operationalEvidenceAlert(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.operationalEvidenceUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OperationalEvidenceRead.OperationalEvidenceAlert(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) operationalEvidenceAlertRoutes(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.operationalEvidenceUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OperationalEvidenceRead.OperationalEvidenceAlertRoutes(request.Context())
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) operationalEvidenceAuditVerification(
	writer http.ResponseWriter, request *http.Request, _ authentication.Principal,
) {
	if handler.operationalEvidenceUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OperationalEvidenceRead.OperationalEvidenceAuditVerification(request.Context())
	handler.writeRead(writer, request, value, err)
}
