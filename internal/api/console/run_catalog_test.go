package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"axiom/internal/api/generated"
)

func TestRunCatalogueUsesSemanticStrategiesWithoutRawIdentifiers(t *testing.T) {
	handler, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, _ := a11HTTPLogin(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/run-catalog", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("catalogue status=%d body=%s", response.Code, response.Body.String())
	}
	var catalogue generated.RunCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalogue); err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Choices) == 0 {
		t.Fatal("catalogue has no approved choices")
	}
	seenTrend, seenMeanReversion := false, false
	for _, choice := range catalogue.Choices {
		if choice.StrategyId == "trend-following" && choice.StrategyVersion == "trend-following@1.0.0" {
			seenTrend = true
		}
		if choice.StrategyId == "mean-reversion" && choice.StrategyVersion == "mean-reversion@1.0.0" {
			seenMeanReversion = true
		}
		if choice.Mode == "live" {
			t.Fatal("catalogue exposed forbidden live mode")
		}
	}
	if !seenTrend || !seenMeanReversion {
		t.Fatalf("catalogue required runtimes trend=%t mean-reversion=%t", seenTrend, seenMeanReversion)
	}
}

func TestUnifiedRunCreationRequiresAnAvailableDurableCommandService(t *testing.T) {
	handler, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, csrf := a11HTTPLogin(t, handler)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(`{
"strategy_id":"trend-following","strategy_version":"trend-following@1.0.0","mode":"backtest",
"exchanges":["binance"],"instrument":"BTC/USDT","preset":"latest-qualified-inputs"}`))
	request.Header.Set("Origin", "http://localhost:4173")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "unified-run-create-0001")
	request.Header.Set("X-CSRF-Token", csrf.Value)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable run command status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkflowBlockerIncludesOwnerActionableFields(t *testing.T) {
	handler := &handler{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	response := httptest.NewRecorder()
	blocker := NewWorkflowBlocker("QUALIFIED_INPUTS_UNAVAILABLE", "No qualified inputs are available.",
		"A matching immutable dataset is required.", "No run was created.", "Register protected data.",
		"missing", "qualified", "dataset")
	handler.writeServiceError(response, request, blocker)
	if response.Code != http.StatusPreconditionFailed {
		t.Fatalf("workflow blocker status=%d", response.Code)
	}
	var body generated.Error
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != "QUALIFIED_INPUTS_UNAVAILABLE" ||
		body.Detail == nil || body.Impact == nil || body.SuggestedAction == nil || body.BlockingPrerequisites == nil ||
		body.CorrelationId == "" {
		t.Fatalf("workflow blocker=%+v err=%v", body, err)
	}
}
