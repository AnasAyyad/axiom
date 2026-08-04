package console

import (
	"context"
	"errors"
	"net/http"
	"time"

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
	Shadows(context.Context, string, int, string) (generated.ShadowSessionPage, error)
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

// SandboxReadService exposes only redacted authoritative V1C projections.
type SandboxReadService interface {
	SandboxOverview(context.Context) (generated.SandboxOverview, error)
	SandboxOrders(context.Context, string, int, string, string) (generated.SandboxOrderPage, error)
	SandboxReconciliations(context.Context, string, int, string) (generated.SandboxReconciliationPage, error)
	C6Qualification(context.Context) (generated.C6QualificationStatus, error)
}

// SandboxCommandService persists C6 controls through existing V1C state
// machines. It owns no exchange client and cannot perform network I/O.
type SandboxCommandService interface {
	CreateSandboxArm(context.Context, authentication.Principal, string, string, generated.SandboxArmRequest, authentication.ConsumedAuthorization) (generated.SandboxArm, error)
	RevokeSandboxArm(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	UnlockSandboxAccount(context.Context, authentication.Principal, string, string, generated.SandboxUnlockRequest, authentication.ConsumedAuthorization) (generated.CommandAccepted, error)
	CreateSandboxTestOrder(context.Context, authentication.Principal, string, generated.SandboxTestOrderRequest) (generated.CommandAccepted, error)
	QueueSandboxOrderCommand(context.Context, authentication.Principal, string, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	QueueSandboxAccountReconciliation(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
}

// D1ListQuery is a validated, bounded, deterministic collection request.
type D1ListQuery struct {
	Cursor   string
	PageSize int
	From     *time.Time
	To       *time.Time
	Filters  map[string]string
}

// D1ActivityQuery carries only stable documented activity filters.
type D1ActivityQuery struct {
	D1ListQuery
	View, Strategy, Instrument, Exchange, Side, Outcome, Reason, Mode, CorrelationID string
}

// D1Command is the closed internal command envelope. Payload contains only
// handler-validated, non-secret values from generated request models.
type D1Command struct {
	Kind, TargetID, Action, State, IdempotencyKey, Reason string
	ExpectedRevision                                      int64
	Payload                                               map[string]any
	Authorization                                         *authentication.ConsumedAuthorization
}

// D1ReadService owns redacted D1 snapshots and authorized artifact reads.
type D1ReadService interface {
	D1Resources(context.Context, string, D1ListQuery) (generated.D1ResourcePage, error)
	D1Resource(context.Context, string, string) (generated.D1Resource, error)
	D1Activity(context.Context, D1ActivityQuery) (generated.ActivityPage, error)
	D1ActivityDetail(context.Context, string) (generated.ActivityResource, error)
	D1Export(context.Context, authentication.Principal, string) (generated.ExportArtifact, error)
}

// D1CommandService persists the closed D1 command set and export artifacts.
type D1CommandService interface {
	ExecuteD1(context.Context, authentication.Principal, D1Command) (generated.CommandAccepted, error)
	CreateD1Export(context.Context, authentication.Principal, string, generated.ExportRequest) (generated.ExportArtifact, error)
}

// D4ReadService exposes provenance-complete operational evidence without raw
// route secrets or unrestricted log payloads.
type D4ReadService interface {
	D4Report(context.Context, string) (generated.ReportResource, error)
	D4ReportSchedules(context.Context, D1ListQuery) (generated.ReportSchedulePage, error)
	D4Alert(context.Context, string) (generated.AlertDetail, error)
	D4AlertRoutes(context.Context) (generated.AlertRoutePage, error)
	D4AuditVerification(context.Context) (generated.AuditVerification, error)
}
