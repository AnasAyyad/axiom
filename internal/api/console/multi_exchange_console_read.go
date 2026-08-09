package console

import (
	"net/http"
	"strings"

	"axiom/internal/authentication"
)

func (handler *handler) registerMultiExchangeConsoleReads(mux *http.ServeMux) {
	routes := []struct {
		pattern string
		next    authenticatedHandler
	}{
		{"GET /api/v1/exchanges", handler.exchanges},
		{"GET /api/v1/opportunities", handler.opportunities},
		{"GET /api/v1/opportunities/{id}", handler.opportunity},
		{"GET /api/v1/strategies", handler.strategies},
		{"GET /api/v1/inventory", handler.inventory},
		{"GET /api/v1/rebalancing/recommendations", handler.rebalancing},
		{"GET /api/v1/rebalancing/recommendations/{id}", handler.rebalancingDetail},
		{"GET /api/v1/research/champion-challenger", handler.championChallenger},
		{"GET /api/v1/replays/{id}/faults", handler.replayFaults},
	}
	for _, route := range routes {
		mux.HandleFunc(route.pattern, handler.authorized(route.next, "operations.read"))
	}
}

func (handler *handler) exchanges(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	value, err := handler.options.Read.Exchanges(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) opportunities(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	kind := request.URL.Query().Get("kind")
	if kind != "" && kind != "triangular" && kind != "cross_exchange" {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.Read.Opportunities(
		request.Context(), request.URL.Query().Get("cursor"), limit, kind,
	)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) opportunity(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.Opportunity(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) strategies(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	value, err := handler.options.Read.Strategies(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) inventory(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	filters := InventoryFilters{
		Exchange:  request.URL.Query().Get("exchange"),
		Asset:     request.URL.Query().Get("asset"),
		Strategy:  request.URL.Query().Get("strategy"),
		Portfolio: request.URL.Query().Get("portfolio"),
	}
	if !validMultiExchangeConsoleInventoryFilters(filters) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.Read.Inventory(
		request.Context(), request.URL.Query().Get("cursor"), limit, filters,
	)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) rebalancing(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	value, err := handler.options.Read.Rebalancing(request.Context(), request.URL.Query().Get("cursor"), limit)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) rebalancingDetail(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.RebalancingDetail(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) championChallenger(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	limit, ok := handler.multiExchangeConsolePageSize(writer, request)
	if !ok {
		return
	}
	value, err := handler.options.Read.ChampionChallenger(
		request.Context(), request.URL.Query().Get("cursor"), limit,
	)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) replayFaults(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.readUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Read.ReplayFaults(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) multiExchangeConsolePageSize(writer http.ResponseWriter, request *http.Request) (int, bool) {
	limit, err := pageSize(request)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return 0, false
	}
	if handler.readUnavailable(writer, request) {
		return 0, false
	}
	return limit, true
}

func validMultiExchangeConsoleInventoryFilters(filters InventoryFilters) bool {
	values := []struct {
		value string
		limit int
	}{
		{filters.Exchange, 32}, {filters.Asset, 16},
		{filters.Strategy, 128}, {filters.Portfolio, 192},
	}
	for _, item := range values {
		if len(item.value) > item.limit || strings.ContainsAny(item.value, "\r\n\x00") {
			return false
		}
	}
	return true
}
