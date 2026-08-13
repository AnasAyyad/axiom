package console

import (
	"encoding/json"
	"errors"
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/backtest"
	"axiom/internal/demonstrations"
)

var errInvalidDemonstrationPayload = errors.New("demonstration_payload_invalid")

// guidedDemonstrations returns only synthetic walkthroughs installed by this
// build. It does not represent a durable run catalogue or external capability.
func (handler *handler) guidedDemonstrations(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	scenarios := demonstrations.Catalogue()
	response := generated.GuidedDemonstrationPage{Items: make([]generated.GuidedDemonstration, 0, len(scenarios))}
	for _, scenario := range scenarios {
		response.Items = append(response.Items, generated.GuidedDemonstration{
			Id: scenario.ID, Title: scenario.Title, Description: scenario.Description,
			StrategyId: scenario.StrategyID, StrategyVersion: scenario.StrategyVersion,
			Synthetic: true, ExpectedOutcomes: append([]string(nil), scenario.ExpectedOutcomes...),
		})
	}
	handler.writeJSON(writer, http.StatusOK, response)
}

// guidedDemonstration executes one read-only synthetic walkthrough. It has no
// storage, account, network, credential, or exchange-order side effect.
func (handler *handler) guidedDemonstration(
	writer http.ResponseWriter,
	request *http.Request,
	_ authentication.Principal,
) {
	result, err := demonstrations.Run(request.Context(), request.PathValue("id"))
	if err != nil {
		if errors.Is(err, demonstrations.ErrNotFound) {
			handler.writeError(writer, request, http.StatusNotFound, "demonstration_not_found", "Guided demonstration was not found")
			return
		}
		handler.writeError(writer, request, http.StatusServiceUnavailable, "demonstration_unavailable", "Guided demonstration is unavailable")
		return
	}
	accepted, acceptedErr := guidedDemonstrationEvent(result.Accepted)
	rejected, rejectedErr := guidedDemonstrationEvent(result.Rejected)
	metrics, metricsErr := json.Marshal(result.Metrics)
	advisoryEvidence, advisoryErr := canonicalDemonstrationPayload(result.AdvisoryEvidence)
	if result.AdvisoryEvidence == nil {
		advisoryEvidence, advisoryErr = "", nil
	}
	if acceptedErr != nil || rejectedErr != nil || metricsErr != nil || advisoryErr != nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "demonstration_unavailable", "Guided demonstration is unavailable")
		return
	}
	handler.writeJSON(writer, http.StatusOK, generated.GuidedDemonstrationResult{
		Id: result.ID, StrategyId: result.StrategyID, StrategyVersion: result.StrategyVersion,
		Synthetic: result.Synthetic, ConfigurationHash: result.ConfigurationHash,
		Accepted: accepted, Rejected: rejected, Metrics: string(metrics), ResultHash: result.ResultHash,
		AdvisoryOnly: result.AdvisoryOnly, AdvisoryEvidence: optionalString(advisoryEvidence),
	})
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func guidedDemonstrationEvent(event backtest.EventResult) (generated.GuidedDemonstrationEvent, error) {
	decision, decisionErr := canonicalDemonstrationPayload(event.Decision)
	orders, ordersErr := canonicalDemonstrationPayload(event.Orders)
	executions, executionsErr := canonicalDemonstrationPayload(event.ExecutionEvents)
	balances, balancesErr := canonicalDemonstrationPayload(event.Balances)
	if decisionErr != nil || ordersErr != nil || executionsErr != nil || balancesErr != nil {
		return generated.GuidedDemonstrationEvent{}, errInvalidDemonstrationPayload
	}
	return generated.GuidedDemonstrationEvent{Ordinal: event.Ordinal, Decision: decision,
		Orders: orders, ExecutionEvents: executions, Balances: balances}, nil
}

func canonicalDemonstrationPayload(value json.RawMessage) (string, error) {
	if !json.Valid(value) {
		return "", errInvalidDemonstrationPayload
	}
	return string(value), nil
}
