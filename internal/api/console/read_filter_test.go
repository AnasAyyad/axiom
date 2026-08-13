package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

type ownerConsoleFilterReadStub struct {
	ReadService
	incidentState    string
	auditEvent       string
	auditRaw         bool
	jobID            string
	eventOrdinal     string
	incidentRaw      bool
	opportunityKind  string
	inventoryFilters InventoryFilters
}

func (stub *ownerConsoleFilterReadStub) Opportunities(
	_ context.Context, _ string, _ int, kind string,
) (generated.OpportunityPage, error) {
	stub.opportunityKind = kind
	return generated.OpportunityPage{Items: []generated.OpportunitySummary{},
		Revision: "0", SnapshotRevision: "0"}, nil
}

func (stub *ownerConsoleFilterReadStub) Inventory(
	_ context.Context, _ string, _ int, filters InventoryFilters,
) (generated.InventoryPage, error) {
	stub.inventoryFilters = filters
	return generated.InventoryPage{Items: []generated.InventoryPosition{},
		Revision: "0", SnapshotRevision: "0", CombinedBalance: false,
		IsolationNotice: "isolated"}, nil
}

func (stub *ownerConsoleFilterReadStub) Job(_ context.Context, id, eventOrdinal string) (generated.JobResource, error) {
	stub.jobID, stub.eventOrdinal = id, eventOrdinal
	return generated.JobResource{Id: id, Kind: generated.JobResourceKind("replay"),
		State: generated.JobResourceState("PAUSED"), ModeLabel: generated.REPLAY, Revision: "1"}, nil
}

func (stub *ownerConsoleFilterReadStub) Incident(_ context.Context, id string, raw bool) (generated.IncidentDetail, error) {
	stub.incidentRaw = raw
	return generated.IncidentDetail{Id: id, ReasonCode: "test", Revision: "1",
		Severity: generated.IncidentDetailSeverity("warning"), State: generated.IncidentDetailState("resolved"),
		Timeline: []generated.TimelineEvent{}}, nil
}

func (stub *ownerConsoleFilterReadStub) Incidents(_ context.Context, _ string, _ int, state string) (generated.IncidentPage, error) {
	stub.incidentState = state
	return generated.IncidentPage{Items: []generated.IncidentSummary{}, Revision: "0"}, nil
}

func (stub *ownerConsoleFilterReadStub) Audit(_ context.Context, _ string, _ int, eventType string, raw bool) (generated.AuditEventPage, error) {
	stub.auditEvent, stub.auditRaw = eventType, raw
	return generated.AuditEventPage{Items: []generated.AuditEvent{}, Revision: "0"}, nil
}

func TestReadFiltersReachAuthoritativeProjection(t *testing.T) {
	stub := &ownerConsoleFilterReadStub{}
	handler := &handler{options: Options{Read: stub}}

	incidentResponse := httptest.NewRecorder()
	handler.incidents(incidentResponse, httptest.NewRequest(http.MethodGet,
		"/api/v1/incidents?state=acknowledged&page_size=25", nil), authentication.Principal{})
	if incidentResponse.Code != http.StatusOK || stub.incidentState != "acknowledged" {
		t.Fatalf("incident filter = %d %q", incidentResponse.Code, stub.incidentState)
	}

	auditResponse := httptest.NewRecorder()
	handler.audit(auditResponse, httptest.NewRequest(http.MethodGet,
		"/api/v1/audit-events?event_type=command_completed&include_detail=true&page_size=25", nil),
		authentication.Principal{UserID: "owner"})
	if auditResponse.Code != http.StatusOK || stub.auditEvent != "command_completed" || !stub.auditRaw {
		t.Fatalf("audit filter = %d %q raw=%t", auditResponse.Code, stub.auditEvent, stub.auditRaw)
	}
}

func TestRawEvidenceRequiresExplicitPermission(t *testing.T) {
	stub := &ownerConsoleFilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-owner_console?include_raw=true", nil)
	request.SetPathValue("id", "incident-owner_console")
	response := httptest.NewRecorder()
	handler.incident(response, request, authentication.Principal{})
	if response.Code != http.StatusForbidden || stub.incidentRaw {
		t.Fatalf("raw incident permission = %d forwarded=%t", response.Code, stub.incidentRaw)
	}

	allowed := httptest.NewRecorder()
	handler.incident(allowed, request, authentication.Principal{UserID: "owner"})
	if allowed.Code != http.StatusOK || !stub.incidentRaw {
		t.Fatalf("authorized raw incident = %d forwarded=%t", allowed.Code, stub.incidentRaw)
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/incident-owner_console?include_raw=1", nil)
	invalidRequest.SetPathValue("id", "incident-owner_console")
	invalid := httptest.NewRecorder()
	handler.incident(invalid, invalidRequest, authentication.Principal{UserID: "owner"})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("noncanonical boolean accepted = %d", invalid.Code)
	}
}

func TestIncidentFilterRejectsUnknownState(t *testing.T) {
	stub := &ownerConsoleFilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	response := httptest.NewRecorder()
	handler.incidents(response, httptest.NewRequest(http.MethodGet,
		"/api/v1/incidents?state=deleted", nil), authentication.Principal{})
	if response.Code != http.StatusBadRequest || stub.incidentState != "" {
		t.Fatalf("unknown state = %d forwarded=%q", response.Code, stub.incidentState)
	}
}

func TestReplayEventOrdinalReachesAuthoritativeProjection(t *testing.T) {
	stub := &ownerConsoleFilterReadStub{}
	handler := &handler{options: Options{Read: stub}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/replays/replay-owner_console?event_ordinal=42", nil)
	request.SetPathValue("id", "replay-owner_console")
	response := httptest.NewRecorder()
	handler.job(response, request, authentication.Principal{})
	if response.Code != http.StatusOK || stub.jobID != "replay-owner_console" || stub.eventOrdinal != "42" {
		t.Fatalf("replay event selection = %d %q/%q", response.Code, stub.jobID, stub.eventOrdinal)
	}

	backtestRequest := httptest.NewRequest(http.MethodGet, "/api/v1/backtests/backtest-owner_console?event_ordinal=42", nil)
	backtestRequest.SetPathValue("id", "backtest-owner_console")
	backtestResponse := httptest.NewRecorder()
	handler.job(backtestResponse, backtestRequest, authentication.Principal{})
	if backtestResponse.Code != http.StatusBadRequest || stub.jobID != "replay-owner_console" {
		t.Fatalf("backtest accepted replay inspection = %d forwarded=%q", backtestResponse.Code, stub.jobID)
	}
}

func TestMultiExchangeConsoleGenericFiltersReachAuthoritativeProjectionAndFailClosed(t *testing.T) {
	stub := &ownerConsoleFilterReadStub{}
	handler := &handler{options: Options{Read: stub}}

	opportunities := httptest.NewRecorder()
	handler.opportunities(opportunities, httptest.NewRequest(http.MethodGet,
		"/api/v1/opportunities?kind=cross_exchange&page_size=25", nil),
		authentication.Principal{})
	if opportunities.Code != http.StatusOK || stub.opportunityKind != "cross_exchange" {
		t.Fatalf("multi-exchange console opportunity filter=%d %q", opportunities.Code, stub.opportunityKind)
	}

	invalid := httptest.NewRecorder()
	handler.opportunities(invalid, httptest.NewRequest(http.MethodGet,
		"/api/v1/opportunities?kind=production&page_size=25", nil),
		authentication.Principal{})
	if invalid.Code != http.StatusBadRequest || stub.opportunityKind != "cross_exchange" {
		t.Fatalf("unsafe multi-exchange console kind forwarded=%d %q", invalid.Code, stub.opportunityKind)
	}

	inventory := httptest.NewRecorder()
	handler.inventory(inventory, httptest.NewRequest(http.MethodGet,
		"/api/v1/inventory?exchange=bybit&asset=BTC&strategy=cross.v1&portfolio=portfolio-1&page_size=25", nil),
		authentication.Principal{})
	if inventory.Code != http.StatusOK ||
		stub.inventoryFilters != (InventoryFilters{Exchange: "bybit", Asset: "BTC",
			Strategy: "cross.v1", Portfolio: "portfolio-1"}) {
		t.Fatalf("multi-exchange console inventory filter=%d %#v", inventory.Code, stub.inventoryFilters)
	}
}
