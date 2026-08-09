package console

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) createOperationalEvidenceIncident(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	var body generated.IncidentCreateRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	target := operationalEvidenceStableTarget("incident", principal.UserID, request.Header.Get("Idempotency-Key"))
	if !body.Severity.Valid() || !validOwnerControlFilter(body.ReasonCode) ||
		!validOwnerControlFilter(body.OwnerUserId) || body.ReasonCode == "" || body.OwnerUserId == "" {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "incident_create", target,
		"create", "open", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload["severity"], command.Payload["reason_code"] = string(body.Severity), body.ReasonCode
	command.Payload["owner_user_id"] = body.OwnerUserId
	handler.executeOwnerControl(writer, request, principal, command)
}

func (handler *handler) updateOperationalEvidenceIncident(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	var body generated.IncidentUpdateRequest
	if !handler.decode(writer, request, &body) || !body.Action.Valid() ||
		!validOperationalEvidenceIncidentUpdate(body) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "incident_update",
		request.PathValue("id"), string(body.Action), "", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload = operationalEvidenceIncidentPayload(body)
	handler.executeOwnerControl(writer, request, principal, command)
}

func validOperationalEvidenceIncidentUpdate(body generated.IncidentUpdateRequest) bool {
	safe := func(value *string) bool { return value == nil || validOwnerControlFilter(*value) }
	if !safe(body.OwnerUserId) || !safe(body.ReferenceId) || !safe(body.DatasetId) ||
		!safe(body.SourceIdentity) || body.Note != nil && len(*body.Note) > 2000 {
		return false
	}
	switch string(body.Action) {
	case "assign_owner":
		return body.OwnerUserId != nil
	case "add_remediation":
		return body.Note != nil && len(*body.Note) >= 3
	case "link_alert", "link_activity":
		return body.ReferenceId != nil
	case "link_replay":
		return body.DatasetId != nil && body.FirstOrdinal != nil && body.LastOrdinal != nil && body.SourceIdentity != nil
	default:
		return false
	}
}

func operationalEvidenceIncidentPayload(body generated.IncidentUpdateRequest) map[string]any {
	payload := make(map[string]any)
	for key, value := range map[string]*string{"owner_user_id": body.OwnerUserId,
		"note": body.Note, "reference_id": body.ReferenceId, "dataset_id": body.DatasetId,
		"source_identity": body.SourceIdentity} {
		if value != nil {
			payload[key] = *value
		}
	}
	if body.FirstOrdinal != nil {
		payload["first_ordinal"] = string(*body.FirstOrdinal)
	}
	if body.LastOrdinal != nil {
		payload["last_ordinal"] = string(*body.LastOrdinal)
	}
	return payload
}

func (handler *handler) createOperationalEvidenceReportSchedule(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	var body generated.ReportScheduleRequest
	if !handler.decode(writer, request, &body) || !body.ReportType.Valid() || !body.Frequency.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	target := operationalEvidenceStableTarget("report-schedule", principal.UserID, request.Header.Get("Idempotency-Key"))
	command, ok := handler.ownerControlBaseCommand(writer, request, "report_schedule", target,
		"create", "active", body.Reason, body.ExpectedRevision)
	if !ok {
		return
	}
	command.Payload = map[string]any{"report_type": string(body.ReportType),
		"frequency": string(body.Frequency), "minute_utc": body.MinuteUtc,
		"hour_utc": body.HourUtc, "weekday_utc": body.WeekdayUtc}
	handler.executeOwnerControl(writer, request, principal, command)
}

func (handler *handler) transitionOperationalEvidenceReportSchedule(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	var body generated.ReportScheduleTransitionRequest
	if !handler.decode(writer, request, &body) || !body.State.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "report_schedule",
		request.PathValue("id"), "transition", string(body.State), body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) escalateOperationalEvidenceAlert(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	handler.ownerControlRevisionCommand("alert", "escalate")(writer, request, principal)
}

func (handler *handler) testOperationalEvidenceAlertRoute(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	var body generated.AlertTestRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	command, ok := handler.ownerControlBaseCommand(writer, request, "alert_test", request.PathValue("id"),
		"test", "pending", body.Reason, body.ExpectedRevision)
	if ok {
		handler.executeOwnerControl(writer, request, principal, command)
	}
}

func (handler *handler) createOperationalEvidenceIncidentBundle(
	writer http.ResponseWriter, request *http.Request, principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.EvidenceBundleRequest
	if !handler.decode(writer, request, &body) || !body.Format.Valid() ||
		!validSandboxReason(body.Reason) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	if handler.ownerControlCommandUnavailable(writer, request) {
		return
	}
	export := generated.ExportRequest{ExpectedRevision: body.ExpectedRevision,
		Format: generated.ExportRequestFormat(body.Format), Reason: body.Reason,
		ResourceId: request.PathValue("id"), ResourceType: generated.ExportRequestResourceTypeIncident}
	value, err := handler.options.OwnerControlCommands.CreateOwnerControlExport(request.Context(), principal, key, export)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, value)
}

func operationalEvidenceStableTarget(prefix, actor, key string) string {
	digest := sha256.Sum256([]byte(prefix + "\x00" + actor + "\x00" + key))
	return prefix + "-" + hex.EncodeToString(digest[:12])
}
