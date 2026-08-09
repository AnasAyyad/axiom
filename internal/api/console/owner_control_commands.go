package console

import (
	"net/http"

	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) ownerControlRevisionCommand(kind, action string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		var body generated.RevisionCommandRequest
		if !handler.decode(writer, request, &body) {
			return
		}
		command, ok := handler.ownerControlBaseCommand(writer, request, kind,
			request.PathValue("id"), action, "", body.Reason, body.ExpectedRevision)
		if ok {
			handler.executeOwnerControl(writer, request, principal, command)
		}
	}
}

func (handler *handler) createOwnerControlReport(
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
	target := operationalEvidenceStableTarget("report", principal.UserID, request.Header.Get("Idempotency-Key"))
	command, ok := handler.ownerControlBaseCommand(writer, request, "report", target,
		"create", "QUEUED", body.Reason, body.ExpectedRevision)
	if ok {
		command.Payload["report_type"] = string(body.ReportType)
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) createOwnerControlExport(
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
		!validOwnerControlFilter(body.ResourceId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.ownerControlCommandUnavailable(writer, request) {
		return
	}
	if _, err := positiveOwnerControlRevision(body.ExpectedRevision); err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	value, err := handler.options.OwnerControlCommands.CreateOwnerControlExport(request.Context(), principal, key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, value)
}

func (handler *handler) getOwnerControlExport(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	if handler.ownerControlReadUnavailable(writer, request) {
		return
	}
	value, err := handler.options.OwnerControlRead.OwnerControlExport(request.Context(), principal, request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) holdOwnerControlExport(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.ArtifactHoldRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.HoldType.Valid() || body.ReferenceId == "" || !validOwnerControlFilter(body.ReferenceId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "artifact_hold",
		request.PathValue("id"), string(body.HoldType), "held", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["hold_type"], command.Payload["reference_id"] = string(body.HoldType), body.ReferenceId
	command.Authorization, ok = handler.ownerControlAuthorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeArtifactHold,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) transitionOwnerControlIncident(
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
	if body.State == generated.IncidentTransitionRequestStateResolved &&
		(body.ResolutionEvidence == nil || len(*body.ResolutionEvidence) < 3) {
		handler.writeServiceError(writer, request, ErrPrecondition)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "incident",
		request.PathValue("id"), "transition", string(body.State), body.Reason, body.ExpectedRevision)
	if ok {
		if body.ResolutionEvidence != nil {
			command.Payload["resolution_evidence"] = *body.ResolutionEvidence
		}
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) activateOwnerControlConfiguration(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.ConfigurationActivationRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if body.ConfigurationId == "" || !validOwnerControlFilter(body.ConfigurationId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "configuration_activation",
		body.ConfigurationId, "activate", "active", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Authorization, ok = handler.ownerControlAuthorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeConfigurationActivation,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) controlOwnerControlLab(
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
	command, ok := handler.ownerControlBaseCommand(writer, request, "lab_run",
		request.PathValue("id"), action, "", body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) startOwnerControlQualification(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	var body generated.QualificationStartRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validSHA256(body.ConfigurationHash) || !validOwnerControlSourceSHA(body.SourceSha) ||
		body.QualificationId == "" || !validOwnerControlFilter(body.QualificationId) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "qualification",
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
	command.Authorization, ok = handler.ownerControlAuthorization(writer, request, principal,
		body.AuthorizationToken, authentication.PurposeQualificationStart,
		body.Reason, command.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func validOwnerControlSourceSHA(value string) bool {
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
