package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/demonstrations"
)

func TestGuidedDemonstrationsOnlyExposeExecutableSyntheticWalkthroughs(t *testing.T) {
	handler, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, _ := a11HTTPLogin(t, handler)
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
	if len(catalogue.Items) != 2 || catalogue.Items[0].Id != demonstrations.TrendFollowingID ||
		!catalogue.Items[0].Synthetic || catalogue.Items[0].StrategyId != "trend-following" ||
		catalogue.Items[1].Id != demonstrations.MeanReversionID ||
		catalogue.Items[1].StrategyId != "mean-reversion" {
		t.Fatalf("catalogue=%+v", catalogue)
	}
}

func TestGuidedDemonstrationReturnsCanonicalPipelineEvidence(t *testing.T) {
	handler, _ := a11HTTPTestHandler(t, []string{"operations.read"})
	session, _ := a11HTTPLogin(t, handler)
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
