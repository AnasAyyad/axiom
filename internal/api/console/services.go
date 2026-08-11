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

// WorkflowBlocker turns a fail-closed domain precondition into an
// owner-actionable response without exposing storage IDs, credentials, or raw
// exchange payloads.
type WorkflowBlocker struct {
	Cause                                                                       error
	Code, Summary, Detail, Impact, SuggestedAction, CurrentState, RequiredState string
	Prerequisites                                                               []string
}

// Error returns the stable reason code for a blocked owner workflow.
func (blocker *WorkflowBlocker) Error() string {
	if blocker == nil || blocker.Code == "" {
		return "workflow_blocked"
	}
	return blocker.Code
}

// Unwrap exposes the underlying precondition error for errors.Is checks.
func (blocker *WorkflowBlocker) Unwrap() error {
	if blocker == nil || blocker.Cause == nil {
		return ErrPrecondition
	}
	return blocker.Cause
}

// NewWorkflowBlocker records the exact safe remediation associated with a
// fail-closed workflow state.
func NewWorkflowBlocker(code, summary, detail, impact, action, current, required string, prerequisites ...string) error {
	return &WorkflowBlocker{Cause: ErrPrecondition, Code: code, Summary: summary, Detail: detail,
		Impact: impact, SuggestedAction: action, CurrentState: current, RequiredState: required,
		Prerequisites: append([]string(nil), prerequisites...)}
}

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

// RunReadService exposes the semantic owner-facing projection over durable
// research and shadow records. It deliberately never exposes configuration,
// portfolio, dataset, or model identifiers as browser inputs.
type RunReadService interface {
	Runs(context.Context) (generated.RunPage, error)
	Run(context.Context, string) (generated.RunResource, error)
	RunOutputs(context.Context, string, string) (generated.RunOutputPage, error)
	RunPortfolio(context.Context, string) (generated.RunPortfolioProjection, error)
	RunRisk(context.Context, string) (generated.RunRiskProjection, error)
	RunEvidence(context.Context, string) (generated.RunEvidence, error)
}

// RunCommandService creates a run only after it resolves the owner-facing
// semantic selection to immutable server-side inputs.
type RunCommandService interface {
	CreateRun(context.Context, authentication.Principal, string, generated.RunCreateRequest) (generated.RunResource, error)
	ControlRun(context.Context, authentication.Principal, string, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
}

// DataCatalogueReadService exposes only server-registered immutable dataset
// evidence. It intentionally has no browser upload or raw storage path.
type DataCatalogueReadService interface {
	DataCatalogue(context.Context) (generated.DataCataloguePage, error)
}

// EvaluationCampaignService owns the server-resolved long-running evaluation
// workflow. Its API never accepts a dataset, strategy configuration, model,
// portfolio, or exchange credential from the browser.
type EvaluationCampaignService interface {
	EvaluationCampaigns(context.Context) (generated.EvaluationCampaignPage, error)
	EvaluationCampaign(context.Context, string) (generated.EvaluationCampaign, error)
	CreateEvaluationCampaign(context.Context, authentication.Principal, string, generated.EvaluationCampaignCreateRequest) (generated.EvaluationCampaign, error)
	CancelEvaluationCampaign(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	EvaluationCampaignEvents(context.Context, string) (generated.EvaluationCampaignEventPage, error)
	EvaluationCampaignReport(context.Context, string) (generated.EvaluationCampaignReport, error)
	CreateDataAudit(context.Context, authentication.Principal, string) (generated.DataAudit, error)
	DataAudit(context.Context, string) (generated.DataAudit, error)
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

// SandboxReadService exposes only redacted authoritative sandbox runtime projections.
type SandboxReadService interface {
	SandboxOverview(context.Context) (generated.SandboxOverview, error)
	SandboxOrders(context.Context, string, int, string, string) (generated.SandboxOrderPage, error)
	SandboxReconciliations(context.Context, string, int, string) (generated.SandboxReconciliationPage, error)
	SandboxQualification(context.Context) (generated.SandboxQualificationStatus, error)
}

// SandboxCommandService persists sandbox qualification controls through existing sandbox runtime state
// machines. It owns no exchange client and cannot perform network I/O.
type SandboxCommandService interface {
	CreateSandboxStrategySession(context.Context, authentication.Principal, string, generated.SandboxStrategySessionCreateRequest) (generated.CommandAccepted, error)
	CreateSandboxArm(context.Context, authentication.Principal, string, string, generated.SandboxArmRequest, authentication.ConsumedAuthorization) (generated.SandboxArm, error)
	StartSandboxStrategySession(context.Context, authentication.Principal, string, string, generated.SandboxStrategySessionStartRequest, authentication.ConsumedAuthorization) (generated.CommandAccepted, error)
	StopSandboxStrategySession(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	RevokeSandboxArm(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	UnlockSandboxAccount(context.Context, authentication.Principal, string, string, generated.SandboxUnlockRequest, authentication.ConsumedAuthorization) (generated.CommandAccepted, error)
	CreateSandboxTestOrder(context.Context, authentication.Principal, string, generated.SandboxTestOrderRequest) (generated.CommandAccepted, error)
	QueueSandboxOrderCommand(context.Context, authentication.Principal, string, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
	QueueSandboxAccountReconciliation(context.Context, authentication.Principal, string, string, generated.RevisionCommandRequest) (generated.CommandAccepted, error)
}

// OwnerControlListQuery is a validated, bounded, deterministic collection request.
type OwnerControlListQuery struct {
	Cursor   string
	PageSize int
	From     *time.Time
	To       *time.Time
	Filters  map[string]string
}

// OwnerControlActivityQuery carries only stable documented activity filters.
type OwnerControlActivityQuery struct {
	OwnerControlListQuery
	View, Strategy, Instrument, Exchange, Side, Outcome, Reason, Mode, CorrelationID string
}

// OwnerControlCommand is the closed internal command envelope. Payload contains only
// handler-validated, non-secret values from generated request models.
type OwnerControlCommand struct {
	Kind, TargetID, Action, State, IdempotencyKey, Reason string
	ExpectedRevision                                      int64
	Payload                                               map[string]any
	Authorization                                         *authentication.ConsumedAuthorization
}

// OwnerControlReadService owns redacted owner control snapshots and authorized artifact reads.
type OwnerControlReadService interface {
	OwnerControlResources(context.Context, string, OwnerControlListQuery) (generated.OwnerControlResourcePage, error)
	OwnerControlResource(context.Context, string, string) (generated.OwnerControlResource, error)
	OwnerControlActivity(context.Context, OwnerControlActivityQuery) (generated.ActivityPage, error)
	OwnerControlActivityDetail(context.Context, string) (generated.ActivityResource, error)
	OwnerControlExport(context.Context, authentication.Principal, string) (generated.ExportArtifact, error)
}

// OwnerControlCommandService persists the closed owner control command set and export artifacts.
type OwnerControlCommandService interface {
	ExecuteOwnerControl(context.Context, authentication.Principal, OwnerControlCommand) (generated.CommandAccepted, error)
	CreateOwnerControlExport(context.Context, authentication.Principal, string, generated.ExportRequest) (generated.ExportArtifact, error)
}

// OperationalEvidenceReadService exposes provenance-complete operational evidence without raw
// route secrets or unrestricted log payloads.
type OperationalEvidenceReadService interface {
	OperationalEvidenceReport(context.Context, string) (generated.ReportResource, error)
	OperationalEvidenceReportSchedules(context.Context, OwnerControlListQuery) (generated.ReportSchedulePage, error)
	OperationalEvidenceAlert(context.Context, string) (generated.AlertDetail, error)
	OperationalEvidenceAlertRoutes(context.Context) (generated.AlertRoutePage, error)
	OperationalEvidenceAuditVerification(context.Context) (generated.AuditVerification, error)
}
