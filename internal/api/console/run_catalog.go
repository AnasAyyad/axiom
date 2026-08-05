package console

import (
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/runs"
)

// runCatalog returns only semantic combinations that this build has approved.
// A client never constructs a run from guessed database IDs or model hashes.
func (handler *handler) runCatalog(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	registry := handler.options.RunRegistry
	if registry == nil {
		var err error
		registry, err = runs.DefaultRegistry()
		if err != nil {
			handler.writeError(writer, request, http.StatusServiceUnavailable, "run_catalogue_unavailable", "Run choices are unavailable")
			return
		}
	}
	choices, blocker := registry.Catalogue(runs.Selection{})
	response := generated.RunCatalog{Choices: make([]generated.RunChoice, 0, len(choices))}
	for _, choice := range choices {
		response.Choices = append(response.Choices, generated.RunChoice{
			StrategyId: choice.StrategyID, StrategyName: choice.StrategyName, StrategyVersion: choice.StrategyVersion,
			Mode: generated.RunChoiceMode(choice.Mode), Instrument: choice.Instrument, Cadence: choice.Cadence,
			Warmup: choice.Warmup, OrderCapable: choice.OrderCapable, Exchanges: runCatalogExchanges(choice.Exchanges),
		})
	}
	if blocker != nil {
		response.Blocker = &generated.RunBlocker{Code: blocker.Code, Summary: blocker.Summary,
			Detail: blocker.Detail, SuggestedAction: blocker.SuggestedAction}
	}
	handler.writeJSON(writer, http.StatusOK, response)
}

func runCatalogExchanges(exchanges []runs.Exchange) []generated.RunChoiceExchanges {
	result := make([]generated.RunChoiceExchanges, 0, len(exchanges))
	for _, exchange := range exchanges {
		result = append(result, generated.RunChoiceExchanges(exchange))
	}
	return result
}
