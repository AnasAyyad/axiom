package console

import (
	"context"
	"errors"
	"net/http"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

// Stable service boundary errors are deliberately free of infrastructure detail.
var (
	ErrNotFound            = errors.New("not_found")
	ErrConflict            = errors.New("conflict")
	ErrIdempotencyConflict = errors.New("idempotency_conflict")
	ErrPrecondition        = errors.New("precondition_failed")
	ErrQuota               = errors.New("quota_exceeded")
	ErrUnavailable         = errors.New("unavailable")
	ErrCursorExpired       = errors.New("cursor_expired")
	ErrInvalidRequest      = errors.New("invalid_request")
)

// ReadService returns authoritative storage projections for the console.
type ReadService interface {
	SystemStatus(context.Context) (generated.SystemStatus, error)
	BinanceHealth(context.Context) (generated.BinanceHealth, error)
	Instruments(context.Context, string, int) (generated.InstrumentPage, error)
	Exchanges(context.Context, string, int) (generated.ExchangePage, error)
	Opportunities(context.Context, string, int, string) (generated.OpportunityPage, error)
	Opportunity(context.Context, string) (generated.OpportunityDetail, error)
	Strategies(context.Context, string, int) (generated.StrategyPage, error)
	Portfolios(context.Context, string, int) (generated.PortfolioPage, error)
	Portfolio(context.Context, string) (generated.PortfolioDetail, error)
	Journal(context.Context, string, string, int) (generated.JournalPage, error)
	Inventory(context.Context, string, int, InventoryFilters) (generated.InventoryPage, error)
	Rebalancing(context.Context, string, int) (generated.RebalancingPage, error)
	RebalancingDetail(context.Context, string) (generated.RebalancingDetail, error)
	ChampionChallenger(context.Context, string, int) (generated.ChampionChallengerPage, error)
	ReplayFaults(context.Context, string) (generated.ReplayFaultPage, error)
	Risk(context.Context) (generated.RiskStatus, error)
	Trend(context.Context) (generated.TrendStatus, error)
	TrendDecisions(context.Context, string, int) (generated.TrendDecisionPage, error)
	Job(context.Context, string, string) (generated.JobResource, error)
	Shadow(context.Context, string) (generated.ShadowSessionResource, error)
	Incidents(context.Context, string, int, string) (generated.IncidentPage, error)
	Incident(context.Context, string, bool) (generated.IncidentDetail, error)
	Audit(context.Context, string, int, string, bool) (generated.AuditEventPage, error)
}

// CommandService persists audited, idempotent commands and durable jobs.
type CommandService interface {
	RiskCommand(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	CreateJob(context.Context, authentication.Principal, string, string, any) (generated.JobResource, error)
	ControlJob(context.Context, authentication.Principal, string, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	CreateShadow(context.Context, authentication.Principal, string, generated.ShadowSessionRequest) (generated.ShadowSessionResource, error)
	StopShadow(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	ScheduleReplayFault(context.Context, authentication.Principal, string, string, generated.ReplayFaultRequest) (generated.ReplayFaultResource, error)
	ExportReport(context.Context, authentication.Principal, string, string, generated.ReportExportRequest) (generated.ReportExportResource, error)
}

// InventoryFilters are exact optional dimensions for isolated virtual inventory.
type InventoryFilters struct {
	Exchange  string
	Asset     string
	Strategy  string
	Portfolio string
}

// StreamService owns durable SSE resume and connection quotas.
type StreamService interface {
	Serve(http.ResponseWriter, *http.Request, authentication.Principal) error
}
