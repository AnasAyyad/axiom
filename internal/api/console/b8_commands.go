package console

import (
	"net/http"
	"strconv"
	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) registerB8Commands(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/replays/{id}/faults",
		handler.authorizedMutation(handler.scheduleReplayFault, "commands.write"))
	mux.HandleFunc("POST /api/v1/reports/{id}/exports",
		handler.authorizedMutation(handler.exportReport, "commands.write"))
}

func (handler *handler) scheduleReplayFault(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.ReplayFaultRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validB8ReplayFault(body) {
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
		return
	}
	if handler.commandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Commands.ScheduleReplayFault(
		request.Context(), principal, request.PathValue("id"), key, body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, value)
}

func (handler *handler) exportReport(
	writer http.ResponseWriter,
	request *http.Request,
	principal authentication.Principal,
) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.ReportExportRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !body.Format.Valid() {
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
		return
	}
	if handler.commandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Commands.ExportReport(
		request.Context(), principal, request.PathValue("id"), key, body,
	)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusCreated, value)
}

func validB8ReplayFault(value generated.ReplayFaultRequest) bool {
	if !value.Fault.Valid() || len(strings.TrimSpace(value.Reason)) < 8 || len(value.Reason) > 500 {
		return false
	}
	expected, expectedErr := strconv.ParseInt(value.ExpectedRevision, 10, 64)
	ordinal, ordinalErr := strconv.ParseInt(value.Ordinal, 10, 64)
	delay, delayErr := strconv.ParseInt(value.DelayNanos, 10, 64)
	if expectedErr != nil || expected < 0 || ordinalErr != nil || ordinal == 0 ||
		delayErr != nil || delay < 0 {
		return false
	}
	if value.Fault == generated.Latency {
		return delay > 0
	}
	return delay == 0
}
