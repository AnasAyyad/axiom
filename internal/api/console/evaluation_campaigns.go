package console

import (
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) evaluationCampaigns(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.EvaluationCampaigns(request.Context())
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) createEvaluationCampaign(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	var body generated.EvaluationCampaignCreateRequest
	if !handler.decode(writer, request, &body) || !body.Preset.Valid() {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.EvaluationCampaigns.CreateEvaluationCampaign(request.Context(), principal, key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) evaluationCampaign(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.EvaluationCampaign(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) cancelEvaluationCampaign(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	var body generated.RevisionCommandRequest
	if !handler.decode(writer, request, &body) || !validRevisionCommand(body) {
		handler.writeServiceError(writer, request, ErrInvalidRequest)
		return
	}
	value, err := handler.options.EvaluationCampaigns.CancelEvaluationCampaign(request.Context(), principal, request.PathValue("id"), key, body)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) evaluationCampaignEvents(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.EvaluationCampaignEvents(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) evaluationCampaignReport(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.EvaluationCampaignReport(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}

func (handler *handler) createDataAudit(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	key, ok := handler.idempotencyKey(writer, request)
	if !ok {
		return
	}
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.CreateDataAudit(request.Context(), principal, key)
	if err != nil {
		handler.writeServiceError(writer, request, err)
		return
	}
	handler.writeJSON(writer, http.StatusAccepted, value)
}

func (handler *handler) dataAudit(writer http.ResponseWriter, request *http.Request, _ authentication.Principal) {
	if handler.options.EvaluationCampaigns == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "evaluation_campaign_unavailable", "Strategy evaluation service unavailable")
		return
	}
	value, err := handler.options.EvaluationCampaigns.DataAudit(request.Context(), request.PathValue("id"))
	handler.writeRead(writer, request, value, err)
}
