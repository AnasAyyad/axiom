package console

import (
	"net/http"

	"strings"
	"time"

	"axiom/internal/authentication"
)

func (handler *handler) registerD1(mux *http.ServeMux) {
	handler.registerD1ReadRoutes(mux)
	handler.registerD1MutationRoutes(mux)

	mux.HandleFunc("POST /api/v1/authorizations",
		handler.authorizedMutation(handler.createHighRiskAuthorization, ""))
}

func (handler *handler) registerD1ReadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/assets",
		handler.authorized(handler.d1List("assets", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/{id}",
		handler.authorized(handler.d1Detail("strategy"), "operations.read"))
	mux.HandleFunc("GET /api/v1/strategies/{id}/versions",
		handler.authorized(handler.d1List("strategy_versions", func(request *http.Request) map[string]string {
			return map[string]string{"strategy_id": request.PathValue("id")}
		}), "operations.read"))
	mux.HandleFunc("GET /api/v1/risk/controls",
		handler.authorized(handler.d1List("risk_controls", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/activity",
		handler.authorized(handler.d1Activity, "activity.read"))
	mux.HandleFunc("GET /api/v1/activity/{id}",
		handler.authorized(handler.d1ActivityDetail, "activity.read"))
	mux.HandleFunc("GET /api/v1/orders",
		handler.authorized(handler.d1List("orders", nil), "activity.read"))
	mux.HandleFunc("GET /api/v1/orders/{id}",
		handler.authorized(handler.d1Detail("order"), "activity.read"))
	mux.HandleFunc("GET /api/v1/fills",
		handler.authorized(handler.d1List("fills", nil), "activity.read"))
	mux.HandleFunc("GET /api/v1/alerts",
		handler.authorized(handler.d1List("alerts", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/reports",
		handler.authorized(handler.d1List("reports", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/exports/{id}",
		handler.authorized(handler.getD1Export, "artifacts.read"))
	mux.HandleFunc("GET /api/v1/configuration-revisions",
		handler.authorized(handler.d1List("configuration_revisions", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/lab-runs",
		handler.authorized(handler.d1List("lab_runs", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/qualifications",
		handler.authorized(handler.d1List("qualifications", nil), "operations.read"))
	mux.HandleFunc("GET /api/v1/commands/{id}",
		handler.authorized(handler.d1Detail("command"), "operations.read"))
}

func (handler *handler) registerD1MutationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/strategies/{id}/configuration",
		handler.authorizedMutation(handler.configureD1Strategy, "configuration.admin"))
	mux.HandleFunc("POST /api/v1/strategies/{id}/runtime",
		handler.authorizedMutation(handler.controlD1StrategyRuntime, "operations.control"))
	mux.HandleFunc("POST /api/v1/risk/controls/{scope}/{id}",
		handler.authorizedMutation(handler.controlD1Risk, "operations.control"))
	mux.HandleFunc("POST /api/v1/alerts/{id}/acknowledge",
		handler.authorizedMutation(handler.d1RevisionCommand("alert", "acknowledge"), "alert.write"))
	mux.HandleFunc("POST /api/v1/reports",
		handler.authorizedMutation(handler.createD1Report, "research.control"))
	mux.HandleFunc("POST /api/v1/exports",
		handler.authorizedMutation(handler.createD1Export, "artifacts.read"))
	mux.HandleFunc("DELETE /api/v1/exports/{id}",
		handler.authorizedMutation(handler.d1RevisionCommand("export", "delete"), "artifacts.manage"))
	mux.HandleFunc("POST /api/v1/exports/{id}/holds",
		handler.authorizedMutation(handler.holdD1Export, "artifacts.manage"))
	mux.HandleFunc("POST /api/v1/incidents/{id}/transitions",
		handler.authorizedMutation(handler.transitionD1Incident, "incident.write"))
	mux.HandleFunc("POST /api/v1/configuration-revisions",
		handler.authorizedMutation(handler.activateD1Configuration, "configuration.admin"))
	mux.HandleFunc("POST /api/v1/lab-runs/{id}/{action}",
		handler.authorizedMutation(handler.controlD1Lab, "research.control"))
	mux.HandleFunc("POST /api/v1/qualifications",
		handler.authorizedMutation(handler.startD1Qualification, "qualification.start"))
	mux.HandleFunc("POST /api/v1/qualifications/{id}/abort",
		handler.authorizedMutation(handler.d1RevisionCommand("qualification", "abort"), "qualification.monitor"))
}

func (handler *handler) d1ReadUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.D1Read != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"d1_projection_unavailable", "Control-plane projection unavailable")
	return true
}

func (handler *handler) d1CommandUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.D1Commands != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable,
		"d1_command_unavailable", "Control-plane command service unavailable")
	return true
}

func (handler *handler) d1List(
	kind string,
	additional func(*http.Request) map[string]string,
) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
		query, err := d1ListQuery(request)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		if additional != nil {
			for key, value := range additional(request) {
				if !validD1Filter(value) {
					handler.writeServiceError(writer, request, ErrInvalidRequest)
					return
				}
				query.Filters[key] = value
			}
		}
		if handler.d1ReadUnavailable(writer, request) {
			return
		}
		value, err := handler.options.D1Read.D1Resources(request.Context(), kind, query)
		handler.writeRead(writer, request, value, err)
	}
}

func d1ListQuery(request *http.Request) (D1ListQuery, error) {
	limit, err := pageSize(request)
	if err != nil {
		return D1ListQuery{}, ErrInvalidRequest
	}
	from, err := d1OptionalTime(request.URL.Query().Get("from"))
	if err != nil {
		return D1ListQuery{}, err
	}
	to, err := d1OptionalTime(request.URL.Query().Get("to"))
	if err != nil || (from != nil && to != nil && from.After(*to)) {
		return D1ListQuery{}, ErrInvalidRequest
	}
	state := request.URL.Query().Get("state")
	if !validD1Filter(state) {
		return D1ListQuery{}, ErrInvalidRequest
	}
	return D1ListQuery{Cursor: request.URL.Query().Get("cursor"), PageSize: limit,
		From: from, To: to, Filters: map[string]string{"state": state}}, nil
}

func d1OptionalTime(value string) (*time.Time, error) {
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

func validD1Filter(value string) bool {
	return len(value) <= 160 && !strings.ContainsAny(value, "\r\n\x00")
}

func (handler *handler) d1Detail(kind string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
		if handler.d1ReadUnavailable(writer, request) {
			return
		}
		value, err := handler.options.D1Read.D1Resource(request.Context(), kind, request.PathValue("id"))
		handler.writeRead(writer, request, value, err)
	}
}

func (handler *handler) d1Activity(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	query, err := d1ListQuery(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	values := request.URL.Query()
	activity := D1ActivityQuery{D1ListQuery: query, View: values.Get("view"),
		Strategy: values.Get("strategy"), Instrument: values.Get("instrument"),
		Exchange: values.Get("exchange"), Side: values.Get("side"),
		Outcome: values.Get("outcome"), Reason: values.Get("reason"),
		Mode: values.Get("mode"), CorrelationID: values.Get("correlation_id")}
	if !validD1ActivityQuery(activity) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.d1ReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.D1Read.D1Activity(request.Context(), activity)
	handler.writeRead(writer, request, value, err)
}

func validD1ActivityQuery(query D1ActivityQuery) bool {
	for _, value := range []string{query.View, query.Strategy, query.Instrument, query.Exchange,
		query.Side, query.Outcome, query.Reason, query.Mode, query.CorrelationID} {
		if !validD1Filter(value) {
			return false
		}
	}
	return (query.View == "" || query.View == "decisions_orders" || query.View == "system_events") &&
		(query.Side == "" || query.Side == "buy" || query.Side == "sell") &&
		(query.Mode == "" || strings.Contains(" backtest replay paper shadow testnet demo ", " "+query.Mode+" "))
}

func (handler *handler) d1ActivityDetail(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.d1ReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.D1Read.D1ActivityDetail(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
