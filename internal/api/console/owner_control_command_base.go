package console

import (
	"net/http"
	"strconv"
	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) createHighRiskAuthorization(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.HighRiskAuthorizationRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.Purpose.Valid() || !validSandboxReason(body.Reason) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	revision, err := positiveOwnerControlRevision(body.ExpectedRevision)
	if err != nil {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.sandboxAuthorizationUnavailable(writer, request) {
		return
	}
	purpose := authentication.AuthorizationPurpose(body.Purpose)
	grant, err := handler.options.SandboxAuthorizations.ReauthenticateForRevision(
		request.Context(), principal, body.Password, body.Totp, purpose, revision,
		sandboxRequestSource(request), body.Reason,
	)
	if err != nil {
		handler.writeError(writer, request, http.StatusForbidden,
			"recent_reauthentication_failed", "Password and TOTP verification failed")
		return
	}
	handler.writeJSON(writer, http.StatusCreated, generated.HighRiskAuthorizationGrant{
		Token: grant.Token, Purpose: generated.HighRiskAuthorizationGrantPurpose(grant.Purpose),
		TargetRevision: body.ExpectedRevision, ExpiresAt: grant.ExpiresAt,
	})
}

func positiveOwnerControlRevision(value generated.Revision) (int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, ErrInvalidRequest
	}
	return revision, nil
}

func (handler *handler) ownerControlBaseCommand(
	writer http.ResponseWriter,
	request *http.Request,
	kind, target, action, state, reason string,
	expected generated.Revision,
) (OwnerControlCommand, bool) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return OwnerControlCommand{}, false
	}
	revision, err := positiveOwnerControlRevision(expected)
	if err != nil || !validSandboxReason(reason) || target == "" ||
		!validOwnerControlFilter(target) || !validOwnerControlFilter(state) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return OwnerControlCommand{}, false
	}
	if handler.ownerControlCommandUnavailable(writer, request) {
		return OwnerControlCommand{}, false
	}
	return OwnerControlCommand{Kind: kind, TargetID: target, Action: action, State: state,
		IdempotencyKey: key, Reason: reason, ExpectedRevision: revision,
		Payload: make(map[string]any)}, true
}

func (handler *handler) ownerControlAuthorization(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
	token string,
	purpose authentication.AuthorizationPurpose,
	reason string,
	revision int64,
) (*authentication.ConsumedAuthorization, bool) {
	consumed, ok := handler.consumeSandboxAuthorization(
		writer, request, principal, token, purpose, reason,
	)
	if !ok || consumed.TargetRevision == nil || *consumed.TargetRevision != revision {
		if ok {
			handler.writeError(writer, request, http.StatusForbidden,
				"authorization_revision_mismatch", "One-use authorization does not match this revision")
		}
		return nil, false
	}
	return &consumed, true
}

func (handler *handler) executeOwnerControl(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
	command OwnerControlCommand,
) {
	value, err := handler.options.OwnerControlCommands.ExecuteOwnerControl(request.Context(), principal, command)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) configureOwnerControlStrategy(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.StrategyConfigurationRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.State.Valid() || body.ConfigurationId == "" || !validOwnerControlFilter(body.ConfigurationId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "strategy_configuration",
		request.PathValue("id"), "configure", string(body.State), body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["configuration_id"] = body.ConfigurationId
	command.Authorization, ok = handler.ownerControlAuthorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeStrategyConfiguration,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) controlOwnerControlStrategyRuntime(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.RuntimeControlRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.State.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "strategy_runtime",
		request.PathValue("id"), "runtime", string(body.State), body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) controlOwnerControlRisk(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.RiskControlRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.State.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	scope := request.PathValue("scope")
	if !strings.Contains(" global strategy instrument exchange new_entries ", " "+scope+" ") {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "risk_control",
		scope+":"+request.PathValue("id"), scope, string(body.State), body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	if body.State == generated.RiskControlRequestStateNormal {
		if body.AuthorizationToken == nil {
			handler.writeError(writer, request, http.StatusForbidden,
				"authorization_required", "One-use authorization is required")
			return
		}
		command.Authorization, ok = handler.ownerControlAuthorization(writer, request, principal,
			*body.AuthorizationToken, authentication.PurposeRiskControl,
			body.Reason, command.ExpectedRevision)
	}
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}
