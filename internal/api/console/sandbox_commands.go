package console

import (
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) createSandboxAuthorization(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.SandboxAuthorizationRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSandboxReason(body.Reason) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxAuthorizationUnavailable(writer, request) {
		return
	}
	purpose, ok := sandboxAuthorizationPurpose(body.Purpose, principal)
	if !ok {
		handler.writeError(writer, request, http.StatusForbidden, "forbidden", "Permission denied")
		return
	}
	grant, err := handler.options.SandboxAuthorizations.Reauthenticate(
		request.Context(),
		principal,
		body.Password,
		body.Totp,
		purpose,
		sandboxRequestSource(request),
		body.Reason,
	)
	if err != nil {
		handler.writeError(
			writer,
			request,
			http.StatusForbidden,
			"recent_reauthentication_failed",
			"Password and TOTP verification failed",
		)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, generated.SandboxAuthorizationGrant{
		Token:     grant.Token,
		Purpose:   generated.SandboxAuthorizationGrantPurpose(grant.Purpose),
		ExpiresAt: grant.ExpiresAt,
	})
}

func sandboxAuthorizationPurpose(
	value generated.SandboxAuthorizationRequestPurpose,
	principal authentication.Principal,
) (authentication.AuthorizationPurpose, bool) {
	purpose := authentication.AuthorizationPurpose(value)
	permission := authentication.PermissionSandboxAdmin
	if purpose == authentication.PurposeSandboxArm {
		permission = authentication.PermissionSandboxArm
	}
	valid := purpose == authentication.PurposeSandboxArm ||
		purpose == authentication.PurposeRiskUnlock
	return purpose, valid &&
		authentication.RequirePermission(principal, permission) == nil
}

func sandboxRequestSource(request *http.Request) string {
	return loginSourceScope(request.RemoteAddr)
}

func (handler *handler) createSandboxArm(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.SandboxArmRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSandboxReason(body.Reason) ||
		!validPositiveRevision(body.ExpectedRevision) ||
		len(body.AccountIds) < 1 || len(body.AccountIds) > 2 {
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
		authentication.PurposeSandboxArm,
		body.Reason,
	)
	if !ok {
		return
	}
	value, err := handler.options.SandboxCommands.CreateSandboxArm(
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
	handler.writeJSON(writer, http.StatusCreated, value)
}

func (handler *handler) createSandboxStrategySession(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok || handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	var body generated.SandboxStrategySessionCreateRequest
	if !handler.decode(writer, request, &body) ||
		!validSandboxStrategySessionCreateRequest(body) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.SandboxCommands.CreateSandboxStrategySession(
		request.Context(), principal, key, body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func validSandboxStrategySessionCreateRequest(
	body generated.SandboxStrategySessionCreateRequest,
) bool {
	if !body.StrategyId.Valid() || !body.Instrument.Valid() || !body.Preset.Valid() ||
		!validSandboxReason(body.Reason) || len(body.Exchanges) == 0 ||
		len(body.Exchanges) > 2 {
		return false
	}
	seen := make(map[generated.SandboxExchange]struct{}, len(body.Exchanges))
	for _, exchange := range body.Exchanges {
		if !exchange.Valid() {
			return false
		}
		if _, duplicate := seen[exchange]; duplicate {
			return false
		}
		seen[exchange] = struct{}{}
	}
	return true
}

func (handler *handler) startSandboxStrategySession(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.SandboxStrategySessionStartRequest
	if !handler.decode(writer, request, &body) || !validSandboxReason(body.Reason) ||
		!validPositiveRevision(body.ExpectedRevision) || handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	consumed, ok := handler.consumeSandboxAuthorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeSandboxArm, body.Reason)
	if !ok {
		return
	}
	value, err := handler.options.SandboxCommands.StartSandboxStrategySession(
		request.Context(), principal, request.PathValue("id"), key, body, consumed,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) stopSandboxStrategySession(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, body, ok := handler.sandboxRevisionCommand(writer, request)
	if !ok || handler.sandboxCommandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.SandboxCommands.StopSandboxStrategySession(
		request.Context(), principal, request.PathValue("id"), key, body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}
