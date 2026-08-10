package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/demonstrations"
)

func TestGuidedDemonstrationsOnlyExposeExecutableSyntheticWalkthroughs(t *testing.T) {
	handler, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
	session, _ := ownerConsoleHTTPLogin(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/demonstrations", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("catalogue status=%d body=%s", response.Code, response.Body.String())
	}
	var catalogue generated.GuidedDemonstrationPage
	if err := json.Unmarshal(response.Body.Bytes(), &catalogue); err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Items) != 5 || catalogue.Items[0].Id != demonstrations.TrendFollowingID ||
		!catalogue.Items[0].Synthetic || catalogue.Items[0].StrategyId != "trend-following" ||
		catalogue.Items[1].Id != demonstrations.MeanReversionID ||
		catalogue.Items[1].StrategyId != "mean-reversion" ||
		catalogue.Items[2].Id != demonstrations.RebalancingID ||
		catalogue.Items[2].StrategyId != "inventory-rebalancing" ||
		catalogue.Items[3].Id != demonstrations.TriangularArbitrageID ||
		catalogue.Items[3].StrategyId != "triangular-arbitrage" ||
		catalogue.Items[4].Id != demonstrations.CrossExchangeArbitrageID ||
		catalogue.Items[4].StrategyId != "cross-exchange-arbitrage" {
		t.Fatalf("catalogue=%+v", catalogue)
	}
}

func TestGuidedDemonstrationReturnsCanonicalPipelineEvidence(t *testing.T) {
	handler, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
	session, _ := ownerConsoleHTTPLogin(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/demonstrations/"+demonstrations.TrendFollowingID, nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("result status=%d body=%s", response.Code, response.Body.String())
	}
	var result generated.GuidedDemonstrationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Synthetic || result.ResultHash == "" || result.ConfigurationHash == "" ||
		result.Accepted.Orders == "[]" || result.Accepted.ExecutionEvents == "[]" ||
		result.Rejected.Orders != "[]" || result.Metrics == "" {
		t.Fatalf("result=%+v", result)
	}

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/demonstrations/not-installed", nil)
	missing.AddCookie(session)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
}

func TestGuidedAdvisoryDemonstrationReturnsEvidenceWithoutOrders(t *testing.T) {
	for _, demonstration := range []struct {
		name, id, acceptedEvidence, rejectedEvidence string
	}{
		{"rebalancing", demonstrations.RebalancingID, "natural_reverse_arbitrage", "route_unavailable"},
	} {
		t.Run(demonstration.name, func(t *testing.T) {
			handler, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
			session, _ := ownerConsoleHTTPLogin(t, handler)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/demonstrations/"+demonstration.id, nil)
			request.AddCookie(session)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("result status=%d body=%s", response.Code, response.Body.String())
			}
			var result generated.GuidedDemonstrationResult
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Synthetic || !result.AdvisoryOnly || result.AdvisoryEvidence == nil ||
				!strings.Contains(*result.AdvisoryEvidence, demonstration.acceptedEvidence) ||
				!strings.Contains(*result.AdvisoryEvidence, demonstration.rejectedEvidence) ||
				result.Accepted.Orders != "[]" || result.Accepted.ExecutionEvents != "[]" ||
				result.Rejected.Orders != "[]" || result.Rejected.ExecutionEvents != "[]" {
				t.Fatalf("advisory result=%+v", result)
			}
		})
	}
}

func TestGuidedMultilegDemonstrationsReturnCanonicalPipelineEvidence(t *testing.T) {
	for _, demonstration := range []struct {
		name, id, execution, rejection string
	}{
		{"triangular", demonstrations.TriangularArbitrageID, "full_success", "fee_capacity_rejected"},
		{"cross exchange", demonstrations.CrossExchangeArbitrageID, "both_filled", "restoration_cost_rejected"},
	} {
		t.Run(demonstration.name, func(t *testing.T) {
			handler, _ := ownerConsoleHTTPTestHandler(t, []string{"operations.read"})
			session, _ := ownerConsoleHTTPLogin(t, handler)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/demonstrations/"+demonstration.id, nil)
			request.AddCookie(session)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("result status=%d body=%s", response.Code, response.Body.String())
			}
			var result generated.GuidedDemonstrationResult
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Synthetic || result.AdvisoryOnly || result.AdvisoryEvidence != nil ||
				result.Accepted.Orders == "[]" || result.Accepted.ExecutionEvents == "[]" ||
				result.Rejected.Orders != "[]" || result.Rejected.ExecutionEvents != "[]" ||
				!strings.Contains(result.Accepted.Decision, "approve") ||
				!strings.Contains(result.Accepted.Balances, "transactions") ||
				!strings.Contains(result.Accepted.ExecutionEvents, demonstration.execution) ||
				!strings.Contains(result.Rejected.Decision, demonstration.rejection) {
				t.Fatalf("multi-leg result=%+v", result)
			}
		})
	}
}
