package console

import (
	"net/http"
	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

// runs returns the single owner-facing list for every durable run that the
// current runtime can actually project. Creation is resolved through the
// semantic run command service, which selects only an installed shared
// materializer for the requested workflow.
func (handler *handler) runs(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.options.Runs == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_projection_unavailable", "Durable run projection unavailable")
		return
	}
	value, err := handler.options.Runs.Runs(request.Context())
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) createRun(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	if handler.options.RunCommands == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_command_unavailable", "Durable run creation unavailable")
		return
	}
	var body generated.RunCreateRequest
	if !handler.decode(writer, request, &body) || !validRunCreateRequest(body) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.RunCommands.CreateRun(request.Context(), principal, key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func validRunCreateRequest(body generated.RunCreateRequest) bool {
	if !body.Mode.Valid() || !body.Preset.Valid() || len(body.Exchanges) == 0 || len(body.Exchanges) > 2 ||
		len(body.StrategyId) < 3 || len(body.StrategyId) > 64 || len(body.StrategyVersion) < 3 ||
		len(body.StrategyVersion) > 128 || len(body.Instrument) < 3 || len(body.Instrument) > 32 {
		return false
	}
	seen := make(map[generated.RunCreateRequestExchanges]struct{}, len(body.Exchanges))
	for _, exchange := range body.Exchanges {
		if !exchange.Valid() {
			return false
		}
		if _, duplicate := seen[exchange]; duplicate {
			return false
		}
		seen[exchange] = struct{}{}
	}
	for _, character := range body.StrategyId {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return !strings.ContainsAny(body.Instrument, "\r\n\x00")
}

func (handler *handler) controlRun(action string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		key, ok := handler.idempotencyKey(writer, request)
		if !ok {
			return
		}
		if handler.options.RunCommands == nil {
			handler.writeError(writer, request, http.StatusServiceUnavailable,
				"run_command_unavailable", "Durable run controls unavailable")
			return
		}
		var body generated.RevisionCommandRequest
		if !handler.decode(writer, request, &body) || !validRevisionCommand(body) {
			handler.writeServiceError(writer, request, ErrInvalidRequest)
			return
		}
		value, err := handler.options.RunCommands.ControlRun(request.Context(), principal,
			request.PathValue("id"), action, key, body)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		handler.writeJSON(writer, http.StatusAccepted, value)
	}
}

func (handler *handler) run(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.options.Runs == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_projection_unavailable", "Durable run projection unavailable")
		return
	}
	value, err := handler.options.Runs.Run(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) runOutputs(kind string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
		if handler.options.Runs == nil {
			handler.writeError(writer, request, http.StatusServiceUnavailable,
				"run_projection_unavailable", "Durable run projection unavailable")
			return
		}
		value, err := handler.options.Runs.RunOutputs(request.Context(), request.PathValue("id"), kind)
		handler.writeRead(writer, request, value, err)
	}
}

func (handler *handler) runPortfolio(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.Runs == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_projection_unavailable", "Durable run projection unavailable")
		return
	}
	value, err := handler.options.Runs.RunPortfolio(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) runRisk(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.Runs == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_projection_unavailable", "Durable run projection unavailable")
		return
	}
	value, err := handler.options.Runs.RunRisk(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) runEvidence(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.Runs == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"run_projection_unavailable", "Durable run projection unavailable")
		return
	}
	value, err := handler.options.Runs.RunEvidence(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) dataCatalogue(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.options.DataCatalogue == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable,
			"data_catalogue_unavailable", "Protected data catalogue unavailable")
		return
	}
	value, err := handler.options.DataCatalogue.DataCatalogue(request.Context())
	handler.writeRead(writer, request, value, err)
}
