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
	Portfolios(context.Context, string, int) (generated.PortfolioPage, error)
	Portfolio(context.Context, string) (generated.PortfolioDetail, error)
	Journal(context.Context, string, string, int) (generated.JournalPage, error)
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
}

// StreamService owns durable SSE resume and connection quotas.
type StreamService interface {
	Serve(http.ResponseWriter, *http.Request, authentication.Principal) error
}
