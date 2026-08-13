package console

import (
	"net/http"
	"strings"

	"axiom/internal/authentication"
)

func (handler *handler) registerSandbox(mux *http.ServeMux) {
	handler.registerSandboxReads(mux)
	handler.registerSandboxCommands(mux)
}

func (handler *handler) registerSandboxReads(mux *http.ServeMux) {
	mux.HandleFunc(
		"GET /api/v1/sandbox/overview",
		handler.authorized(handler.sandboxOverview, authentication.PermissionSandboxRead),
	)
	mux.HandleFunc(
		"GET /api/v1/sandbox/orders",
		handler.authorized(handler.sandboxOrders, authentication.PermissionSandboxRead),
	)
	mux.HandleFunc(
		"GET /api/v1/sandbox/reconciliations",
		handler.authorized(handler.sandboxReconciliations, authentication.PermissionSandboxRead),
	)
	mux.HandleFunc(
		"GET /api/v1/sandbox/qualification",
		handler.authorized(handler.sandboxQualification, authentication.PermissionSandboxRead),
	)
}

func (handler *handler) registerSandboxCommands(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST /api/v1/sandbox/authorizations",
		handler.authorizedMutation(handler.createSandboxAuthorization, ""),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/sessions/{id}/arms",
		handler.authorizedMutation(handler.createSandboxArm, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/strategy-sessions",
		handler.authorizedMutation(handler.createSandboxStrategySession, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/strategy-sessions/{id}/start",
		handler.authorizedMutation(handler.startSandboxStrategySession, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/strategy-sessions/{id}/stop",
		handler.authorizedMutation(handler.stopSandboxStrategySession, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/arms/{id}/revoke",
		handler.authorizedMutation(handler.revokeSandboxArm, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/accounts/{id}/unlock",
		handler.authorizedMutation(handler.unlockSandboxAccount, authentication.PermissionSandboxAdmin),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/orders",
		handler.authorizedMutation(handler.createSandboxTestOrder, authentication.PermissionSandboxArm),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/orders/{id}/cancel",
		handler.authorizedMutation(handler.sandboxOrderCommand("cancel"), authentication.PermissionSandboxCancel),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/orders/{id}/query",
		handler.authorizedMutation(handler.sandboxOrderCommand("query"), authentication.PermissionSandboxCancel),
	)
	mux.HandleFunc(
		"POST /api/v1/sandbox/accounts/{id}/reconcile",
		handler.authorizedMutation(handler.reconcileSandboxAccount, authentication.PermissionSandboxAdmin),
	)
}

func (handler *handler) sandboxReadUnavailable(
	writer http.ResponseWriter,
	request *http.Request,
) bool {
	if handler.options.SandboxRead != nil {
		return false
	}
	handler.writeError(
		writer,
		request,
		http.StatusServiceUnavailable,
		"sandbox_projection_unavailable",
		"Sandbox projection unavailable",
	)
	return true
}

func (handler *handler) sandboxCommandUnavailable(
	writer http.ResponseWriter,
	request *http.Request,
) bool {
	if handler.options.SandboxCommands != nil {
		return false
	}
	handler.writeError(
		writer,
		request,
		http.StatusServiceUnavailable,
		"sandbox_command_unavailable",
		"Sandbox command service unavailable",
	)
	return true
}

func (handler *handler) sandboxOverview(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.sandboxReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxRead.SandboxOverview(request.Context())
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) sandboxOrders(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	limit, err := pageSize(request)
	exchange := request.URL.Query().Get("exchange")
	state := request.URL.Query().Get("state")
	if err != nil || !validSandboxExchangeFilter(exchange) ||
		len(state) > 40 || strings.ContainsAny(state, "\r\n\x00") {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxRead.SandboxOrders(
		request.Context(),
		request.URL.Query().Get("cursor"),
		limit,
		exchange,
		state,
	)
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) sandboxReconciliations(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	limit, err := pageSize(request)
	exchange := request.URL.Query().Get("exchange")
	if err != nil || !validSandboxExchangeFilter(exchange) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxRead.SandboxReconciliations(
		request.Context(),
		request.URL.Query().Get("cursor"),
		limit,
		exchange,
	)
	handler.writeRead(writer, request, value, err)
}

func validSandboxExchangeFilter(value string) bool {
	return value == "" || value == "binance" || value == "bybit"
}

func (handler *handler) sandboxQualification(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	if handler.sandboxReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxRead.SandboxQualification(request.Context())
	handler.writeRead(writer, request, value, err)
}
