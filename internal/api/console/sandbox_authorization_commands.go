package console

import (
	"net/http"
	"strconv"
	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) consumeSandboxAuthorization(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
	token string,
	purpose authentication.AuthorizationPurpose,
	reason string,
) (authentication.ConsumedAuthorization, bool) {
	if handler.sandboxAuthorizationUnavailable(writer, request) {
		return authentication.ConsumedAuthorization{}, false
	}
	consumed, err := handler.options.SandboxAuthorizations.Consume(
		request.Context(),
		principal,
		token,
		purpose,
	)
	if err != nil {
		handler.writeError(
			writer,
			request,
			http.StatusForbidden,
			"authorization_invalid",
			"One-use authorization is invalid or expired",
		)
		return authentication.ConsumedAuthorization{}, false
	}
	if consumed.SourceHash != authentication.AuthorizationBindingHash(
		sandboxRequestSource(request),
	) || consumed.ReasonHash != authentication.AuthorizationBindingHash(reason) {
		handler.writeError(
			writer,
			request,
			http.StatusForbidden,
			"authorization_binding_mismatch",
			"One-use authorization does not match this request",
		)
		return authentication.ConsumedAuthorization{}, false
	}
	return consumed, true
}

func (handler *handler) sandboxAuthorizationUnavailable(
	writer http.ResponseWriter,
	request *http.Request,
) bool {
	if handler.options.SandboxAuthorizations != nil {
		return false
	}
	handler.writeError(
		writer,
		request,
		http.StatusServiceUnavailable,
		"sandbox_authorization_unavailable",
		"High-risk authorization unavailable",
	)
	return true
}

func (handler *handler) revokeSandboxArm(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, body, ok := handler.sandboxRevisionCommand(writer, request)
	if !ok || handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxCommands.RevokeSandboxArm(
		request.Context(),
		principal,
		request.PathValue("id"),
		key,
		body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) unlockSandboxAccount(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.SandboxUnlockRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSandboxReason(body.Reason) ||
		!validPositiveRevision(body.ExpectedRevision) ||
		body.ReconciliationId == "" {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	consumed, ok := handler.consumeSandboxAuthorization(
		writer,
		request,
		principal,
		body.AuthorizationToken,
		authentication.PurposeRiskUnlock,
		body.Reason,
	)
	if !ok {
		return
	}
	value, err := handler.options.SandboxCommands.UnlockSandboxAccount(
		request.Context(),
		principal,
		request.PathValue("id"),
		key,
		body,
		consumed,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) createSandboxTestOrder(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.SandboxTestOrderRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSandboxReason(body.Reason) ||
		!validPositiveRevision(body.ExpectedRevision) ||
		body.Side != generated.SandboxTestOrderRequestSide("buy") ||
		!body.Exchange.Valid() || !body.Instrument.Valid() || !body.Style.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxCommands.CreateSandboxTestOrder(
		request.Context(),
		principal,
		key,
		body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) sandboxOrderCommand(action string) authenticatedHandler {
	return func(
		writer http.ResponseWriter,
		request *http.Request,
		principal authentication.Principal,
	) {
		key, body, ok := handler.sandboxRevisionCommand(writer, request)
		if !ok || handler.sandboxCommandUnavailable(writer, request) {
			return
		}
		value, err := handler.options.SandboxCommands.QueueSandboxOrderCommand(
			request.Context(),
			principal,
			request.PathValue("id"),
			action,
			key,
			body,
		)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		handler.writeJSON(writer, http.StatusAccepted, value)
	}
}

func (handler *handler) reconcileSandboxAccount(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, body, ok := handler.sandboxRevisionCommand(writer, request)
	if !ok || handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxCommands.QueueSandboxAccountReconciliation(
		request.Context(),
		principal,
		request.PathValue("id"),
		key,
		body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) sandboxRevisionCommand(
	writer http.ResponseWriter,
	request *http.Request,
) (string, generated.RevisionCommandRequest, bool) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return "", generated.RevisionCommandRequest{}, false
	}
	var body generated.RevisionCommandRequest
	if !handler.decode(writer, request, &body) ||
		!validRevisionCommand(body) {
		return "", generated.RevisionCommandRequest{}, false
	}
	return key, body, true
}

func validPositiveRevision(value string) bool {
	revision, err := strconv.ParseInt(value, 10, 64)
	return err == nil && revision > 0
}

func validSandboxReason(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 8 && len(value) <= 500 &&
		!strings.ContainsAny(value, "\r\n\x00")
}
