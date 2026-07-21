package console

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) registerCommands(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/risk/pause", handler.authorizedMutation(handler.riskCommand("pause"), "commands.write"))
	mux.HandleFunc("POST /api/v1/risk/resume", handler.authorizedMutation(handler.riskCommand("resume"), "commands.write"))
	mux.HandleFunc("POST /api/v1/backtests", handler.authorizedMutation(handler.createJob("backtest"), "commands.write"))
	mux.HandleFunc("POST /api/v1/replays", handler.authorizedMutation(handler.createJob("replay"), "commands.write"))
	for _, action := range []string{"pause", "resume", "step"} {
		mux.HandleFunc("POST /api/v1/replays/{id}/"+action, handler.authorizedMutation(handler.controlReplay(action), "commands.write"))
	}
	mux.HandleFunc("POST /api/v1/shadow-sessions", handler.authorizedMutation(handler.createShadow, "commands.write"))
	mux.HandleFunc("POST /api/v1/shadow-sessions/{id}/stop", handler.authorizedMutation(handler.stopShadow, "commands.write"))
}

func (handler *handler) idempotencyKey(writer http.ResponseWriter, request *http.Request) (string, bool) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 || !allIdempotencyCharacters(key) {
		handler.writeError(writer, request, http.StatusBadRequest, "idempotency_key_invalid", "A stable idempotency key is required")
		return "", false
	}
	return key, true
}

func allIdempotencyCharacters(value string) bool {
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && !strings.ContainsRune("._:-", character) {
			return false
		}
	}
	return true
}

func validRevisionCommand(value generated.RevisionCommandRequest) bool {
	if len(strings.TrimSpace(value.Reason)) < 8 || len(value.Reason) > 500 {
		return false
	}
	revision, err := strconv.ParseInt(value.ExpectedRevision, 10, 64)
	return err == nil && revision > 0
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func validOfflineRequest(value generated.OfflineJobRequest) bool {
	return value.StrategyVersion.Valid() && value.ConfigurationId != "" && value.DatasetId != "" &&
		value.ResearchGenerationId != "" && validSHA256(value.RootSeedHash)
}

func validReplayRequest(value generated.ReplayJobRequest) bool {
	if !value.StrategyVersion.Valid() || value.ConfigurationId == "" || value.DatasetId == "" ||
		value.ResearchGenerationId == "" || !validSHA256(value.RootSeedHash) {
		return false
	}
	return value.Speed == nil || value.Speed.Valid()
}

func (handler *handler) commandUnavailable(writer http.ResponseWriter, request *http.Request) bool {
	if handler.options.Commands != nil {
		return false
	}
	handler.writeError(writer, request, http.StatusServiceUnavailable, "command_service_unavailable", "Durable command service unavailable")
	return true
}

func (handler *handler) riskCommand(action string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		key, ok := handler.idempotencyKey(writer, request)
		if !ok {
			return
		}
		var body generated.RevisionCommandRequest
		if !handler.decode(writer, request, &body) {
			return
		}
		if !validRevisionCommand(body) {
			handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
			return
		}
		if handler.commandUnavailable(writer, request) {
			return
		}
		if action == "resume" && handler.options.Authentication.RequireRecentReauthentication(principal) != nil {
			handler.writeError(writer, request, http.StatusForbidden, "recent_reauthentication_required", "Recent authentication is required")
			return
		}
		value, err := handler.options.Commands.RiskCommand(request.Context(), principal, action, key, body)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		handler.writeJSON(writer, http.StatusAccepted, value)
	}
}

func (handler *handler) createJob(kind string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		key, ok := handler.idempotencyKey(writer, request)
		if !ok {
			return
		}
		if handler.commandUnavailable(writer, request) {
			return
		}
		if kind == "backtest" {
			var body generated.OfflineJobRequest
			if !handler.decode(writer, request, &body) {
				return
			}
			if !validOfflineRequest(body) {
				handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
				return
			}
			value, err := handler.options.Commands.CreateJob(request.Context(), principal, kind, key, body)
			if err != nil {
				handler.writeServiceError(writer, request, err)
				return
			}
			handler.writeJSON(writer, http.StatusAccepted, value)
			return
		}
		var body generated.ReplayJobRequest
		if !handler.decode(writer, request, &body) {
			return
		}
		if !validReplayRequest(body) {
			handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
			return
		}
		value, err := handler.options.Commands.CreateJob(request.Context(), principal, kind, key, body)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		handler.writeJSON(writer, http.StatusAccepted, value)
	}
}

func (handler *handler) controlReplay(action string) authenticatedHandler {
	return func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		key, ok := handler.idempotencyKey(writer, request)
		if !ok {
			return
		}
		var body generated.RevisionCommandRequest
		if !handler.decode(writer, request, &body) {
			return
		}
		if !validRevisionCommand(body) {
			handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
			return
		}
		if handler.commandUnavailable(writer, request) {
			return
		}
		value, err := handler.options.Commands.ControlJob(request.Context(), principal, request.PathValue("id"), action, key, body)
		if err != nil {
			handler.writeServiceError(writer, request, err)
			return
		}
		handler.writeJSON(writer, http.StatusAccepted, value)
	}
}

func (handler *handler) createShadow(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.ShadowSessionRequest
	if !handler.decode(writer, request, &body) || handler.commandUnavailable(writer, request) {
		return
	}
	if !body.StrategyVersion.Valid() || body.ConfigurationId == "" || body.PortfolioId == "" {
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
		return
	}
	value, err := handler.options.Commands.CreateShadow(request.Context(), principal, key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) stopShadow(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	var body generated.RevisionCommandRequest
	if !handler.decode(writer, request, &body) {
		return
	}
	if !validRevisionCommand(body) {
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
		return
	}
	if handler.commandUnavailable(writer, request) {
		return
	}
	value, err := handler.options.Commands.StopShadow(request.Context(), principal, request.PathValue("id"), key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) stream(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	if !handler.validEventStreamOrigin(request) {
		handler.writeError(writer, request, http.StatusForbidden, "origin_invalid", "Request origin rejected")
		return
	}
	if handler.options.Streams == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "stream_unavailable", "Live stream unavailable")
		return
	}
	// The API server has a finite write timeout for ordinary responses. SSE is a
	// long-lived response, so remove only this response's deadline while keeping
	// the global safety limit for every other route.
	if err := http.NewResponseController(writer).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		handler.writeServiceError(writer, request, err)
		return
	}
	if err := handler.options.Streams.Serve(writer, request, principal); err != nil {
		handler.writeServiceError(writer, request, err)
	}
}
