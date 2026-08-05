package console

import (
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
	seenTrend, seenRebalancing := false, false
	for _, choice := range catalogue.Choices {
		if choice.StrategyId == "trend-following" && choice.StrategyVersion == "trend-following@1.0.0" {
			seenTrend = true
		}
		if choice.StrategyId == "inventory-rebalancing" && !choice.OrderCapable {
			seenRebalancing = true
		}
		if choice.Mode == "live" {
			t.Fatal("catalogue exposed forbidden live mode")
		}
	}
	if !seenTrend || !seenRebalancing {
		t.Fatalf("catalogue required strategies trend=%t rebalancing=%t", seenTrend, seenRebalancing)
	}
}
