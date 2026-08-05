package console

import (
	"net/http"

	"axiom/internal/authentication"
)

// runs returns the single owner-facing list for every durable run that the
// current runtime can actually project. Creation remains on the existing
// specialised endpoints until each strategy has a shared materializer.
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
