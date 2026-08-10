package console

import (
	"net/http"

	"strings"
	"time"

	"axiom/internal/authentication"
)

func (handler *handler) registerOwnerControl(mux *http.ServeMux) {
	handler.registerOwnerControlReadRoutes(mux)
	handler.registerOwnerControlMutationRoutes(mux)

	mux.HandleFunc("POST /api/v1/authorizations",
		handler.authorizedMutation(handler.createHighRiskAuthorization, ""))
}

func (handler *handler) registerOwnerControlReadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/assets",
		handler.authorized(handler.ownerControlList("assets", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/{id}",
		handler.authorized(handler.ownerControlDetail("strategy"), "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/{id}/versions",
		handler.authorized(handler.ownerControlList("strategy_versions", func(request *http.Request) map[string]string {
			return map[string]string{"strategy_id": request.PathValue("id")}
		}), "operations.read"))
	mux.HandleFunc("GET /api/v1/risk/controls",
		handler.authorized(handler.ownerControlList("risk_controls", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/activity",
		handler.authorized(handler.ownerControlActivity, "activity.read"))
	mux.HandleFunc("GET /api/v1/activity/{id}",
		handler.authorized(handler.ownerControlActivityDetail, "activity.read"))
	mux.HandleFunc("GET /api/v1/orders",
		handler.authorized(handler.ownerControlList("orders", nil), "activity.read"))
	mux.HandleFunc("GET /api/v1/orders/{id}",
		handler.authorized(handler.ownerControlDetail("order"), "activity.read"))
	mux.HandleFunc("GET /api/v1/fills",
		handler.authorized(handler.ownerControlList("fills", nil), "activity.read"))
	mux.HandleFunc("GET /api/v1/alerts",
		handler.authorized(handler.ownerControlList("alerts", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/reports",
		handler.authorized(handler.ownerControlList("reports", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/exports/{id}",
		handler.authorized(handler.getOwnerControlExport, "artifacts.read"))
	mux.HandleFunc("GET /api/v1/configuration-revisions",
		handler.authorized(handler.ownerControlList("configuration_revisions", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/lab-runs",
		handler.authorized(handler.ownerControlList("lab_runs", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/qualifications",
		handler.authorized(handler.ownerControlList("qualifications", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/commands/{id}",
		handler.authorized(handler.ownerControlDetail("command"), "operations.read"))
}

func (handler *handler) registerOwnerControlMutationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/strategies/{id}/configuration",
		handler.authorizedMutation(handler.configureOwnerControlStrategy, "configuration.admin"))
	mux.HandleFunc("POST /api/v1/strategies/{id}/runtime",
		handler.authorizedMutation(handler.controlOwnerControlStrategyRuntime, "operations.control"))
	mux.HandleFunc("POST /api/v1/risk/controls/{scope}/{id}",
		handler.authorizedMutation(handler.controlOwnerControlRisk, "operations.control"))
	mux.HandleFunc("POST /api/v1/alerts/{id}/acknowledge",
		handler.authorizedMutation(handler.ownerControlRevisionCommand("alert", "acknowledge"), "alert.write"))
	mux.HandleFunc("POST /api/v1/reports",
		handler.authorizedMutation(handler.createOwnerControlReport, "research.control"))
	mux.HandleFunc("POST /api/v1/exports",
		handler.authorizedMutation(handler.createOwnerControlExport, "artifacts.read"))
	mux.HandleFunc("DELETE /api/v1/exports/{id}",
		handler.authorizedMutation(handler.ownerControlRevisionCommand("export", "delete"), "artifacts.manage"))
	mux.HandleFunc("POST /api/v1/exports/{id}/holds",
		handler.authorizedMutation(handler.holdOwnerControlExport, "artifacts.manage"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/transitions",
		handler.authorizedMutation(handler.transitionOwnerControlIncident, "incident.write"))
	mux.HandleFunc("POST /api/v1/configuration-revisions",
		handler.authorizedMutation(handler.activateOwnerControlConfiguration, "configuration.admin"))
	mux.HandleFunc("POST /api/v1/lab-runs/{id}/{action}",
		handler.authorizedMutation(handler.controlOwnerControlLab, "research.control"))
	mux.HandleFunc("POST /api/v1/qualifications",
		handler.authorizedMutation(handler.startOwnerControlQualification, "qualification.start"))
	mux.HandleFunc("POST /api/v1/qualifications/{id}/abort",
		handler.authorizedMutation(handler.ownerControlRevisionCommand("qualification", "abort"), "qualification.monitor"))
}

func (handler *handler) ownerControlReadUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.OwnerControlRead != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"owner_control_projection_unavailable", "Control-plane projection unavailable")
	return true
}

func (handler *handler) ownerControlCommandUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.OwnerControlCommands != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"owner_control_command_unavailable", "Control-plane command service unavailable")
	return true
}

func (handler *handler) ownerControlList(
	kind string,
	additional func(*http.Request) map[string]string,
) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
		query, err := ownerControlListQuery(request)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		if additional != nil {
			for key, value := range additional(request) {
				if !validOwnerControlFilter(value) {
					handler.writeServiceError(writer, request, ErrInvalidRequest)
					return
				}
				query.Filters[key] = value
			}
		}
		if handler.ownerControlReadUnavailable(writer, request) {
			return
		}
		value, err := handler.options.OwnerControlRead.OwnerControlResources(request.Context(), kind, query)
		handler.writeRead(writer, request, value, err)
	}
}

func ownerControlListQuery(request *http.Request) (OwnerControlListQuery, error) {
	limit, err := pageSize(request)
	if err != nil {
		return OwnerControlListQuery{}, ErrInvalidRequest
	}
	from, err := ownerControlOptionalTime(request.URL.Query().Get("from"))
	if err != nil {
		return OwnerControlListQuery{}, err
	}
	to, err := ownerControlOptionalTime(request.URL.Query().Get("to"))
	if err != nil || (from != nil && to != nil && from.After(*to)) {
		return OwnerControlListQuery{}, ErrInvalidRequest
	}
	state := request.URL.Query().Get("state")
	if !validOwnerControlFilter(state) {
		return OwnerControlListQuery{}, ErrInvalidRequest
	}
	return OwnerControlListQuery{Cursor: request.URL.Query().Get("cursor"), PageSize: limit,
		From: from, To: to, Filters: map[string]string{"state": state}}, nil
}

func ownerControlOptionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() == nil {
		return nil, ErrInvalidRequest
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func validOwnerControlFilter(value string) bool {
	return len(value) <= 160 && !strings.ContainsAny(value, "\r\n\x00")
}

func (handler *handler) ownerControlDetail(kind string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
		if handler.ownerControlReadUnavailable(writer, request) {
			return
		}
		value, err := handler.options.OwnerControlRead.OwnerControlResource(request.Context(), kind, request.PathValue("id"))
		handler.writeRead(writer, request, value, err)
	}
}

func (handler *handler) ownerControlActivity(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	query, err := ownerControlListQuery(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	values := request.URL.Query()
	activity := OwnerControlActivityQuery{OwnerControlListQuery: query, View: values.Get("view"),
		Strategy: values.Get("strategy"), Instrument: values.Get("instrument"),
		Exchange: values.Get("exchange"), Side: values.Get("side"),
		Outcome: values.Get("outcome"), Reason: values.Get("reason"),
		Mode: values.Get("mode"), CorrelationID: values.Get("correlation_id")}
	if !validOwnerControlActivityQuery(activity) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.ownerControlReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OwnerControlRead.OwnerControlActivity(request.Context(), activity)
	handler.writeRead(writer, request, value, err)
}

func validOwnerControlActivityQuery(query OwnerControlActivityQuery) bool {
	for _, value := range []string{query.View, query.Strategy, query.Instrument, query.Exchange,
		query.Side, query.Outcome, query.Reason, query.Mode, query.CorrelationID} {
		if !validOwnerControlFilter(value) {
			return false
		}
	}
	return (query.View == "" || query.View == "decisions_orders" || query.View == "system_events") &&
		(query.Side == "" || query.Side == "buy" || query.Side == "sell") &&
		(query.Mode == "" || strings.Contains(" backtest replay paper shadow testnet demo ", " "+query.Mode+" "))
}

func (handler *handler) ownerControlActivityDetail(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.ownerControlReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OwnerControlRead.OwnerControlActivityDetail(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
