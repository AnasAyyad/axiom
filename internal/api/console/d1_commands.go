package console

import (
	"net/http"

	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) d1RevisionCommand(kind, action string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		var body generated.RevisionCommandRequest
		if !handler.decode(writer, request, &body) {
			return
		}
		command, ok := handler.d1BaseCommand(writer, request, kind,
			request.PathValue("id"), action, "", body.Reason, body.ExpectedRevision)
		if ok {
			handler.executeD1(writer, request, principal, command)
		}
	}
}

func (handler *handler) createD1Report(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.ReportRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.ReportType.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "report", string(body.ReportType),
		"create", "QUEUED", body.Reason, body.ExpectedRevision)
	if ok {
		command.Payload["report_type"] = string(body.ReportType)
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) createD1Export(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.ExportRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.Format.Valid() || !body.ResourceType.Valid() ||
		!validSandboxReason(body.Reason) || body.ResourceId == "" ||
		!validD1Filter(body.ResourceId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.d1CommandUnavailable(writer, request) {
		return
	}
	if _, err := positiveD1Revision(body.ExpectedRevision); err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	value, err := handler.options.D1Commands.CreateD1Export(request.Context(), principal, key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, value)
}

func (handler *handler) getD1Export(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	if handler.d1ReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.D1Read.D1Export(request.Context(), principal, request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) holdD1Export(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.ArtifactHoldRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.HoldType.Valid() || body.ReferenceId == "" || !validD1Filter(body.ReferenceId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "artifact_hold",
		request.PathValue("id"), string(body.HoldType), "held", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["hold_type"], command.Payload["reference_id"] = string(body.HoldType), body.ReferenceId
	command.Authorization, ok = handler.d1Authorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeArtifactHold,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) transitionD1Incident(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.IncidentTransitionRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.State.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "incident",
		request.PathValue("id"), "transition", string(body.State), body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) activateD1Configuration(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.ConfigurationActivationRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if body.ConfigurationId == "" || !validD1Filter(body.ConfigurationId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "configuration_activation",
		body.ConfigurationId, "activate", "active", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Authorization, ok = handler.d1Authorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeConfigurationActivation,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) controlD1Lab(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	action := request.PathValue("action")
	if !strings.Contains(" pause resume cancel reproduce ", " "+action+" ") {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	var body generated.RevisionCommandRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "lab_run",
		request.PathValue("id"), action, "", body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) startD1Qualification(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.QualificationStartRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSHA256(body.ConfigurationHash) || !validD1SourceSHA(body.SourceSha) ||
		body.QualificationId == "" || !validD1Filter(body.QualificationId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.d1BaseCommand(writer, request, "qualification",
		body.QualificationId, "start", "PREFLIGHT", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["source_sha"] = body.SourceSha
	command.Payload["configuration_hash"] = body.ConfigurationHash
	if body.ImageDigest != nil {
		command.Payload["image_digest"] = *body.ImageDigest
	}
	if body.ServerIdentity != nil {
		command.Payload["server_identity"] = *body.ServerIdentity
	}
	command.Authorization, ok = handler.d1Authorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeQualificationStart,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func (handler *handler) changeD1Roles(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.RoleChangeRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if len(body.Roles) == 0 || len(body.Roles) > 4 {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	roles := make([]string, 0, len(body.Roles))
	for _, role := range body.Roles {
		if !role.Valid() {
			handler.writeServiceError(writer, request, ErrInvalidRequest)
			return
		}
		roles = append(roles, string(role))
	}
	command, ok := handler.d1BaseCommand(writer, request, "role_change",
		request.PathValue("id"), "replace", "active", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["roles"] = roles
	command.Authorization, ok = handler.d1Authorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeRoleChange,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeD1(writer, request, principal, command)
	}
}

func validD1SourceSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
