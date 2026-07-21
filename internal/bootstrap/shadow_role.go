package bootstrap

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	postgresstore "axiom/internal/storage/postgres"
)

type shadowRuntimeStore interface {
	Claim(context.Context) (postgresstore.A11ShadowClaim, bool, error)
	Renew(context.Context, string) error
	Posture(context.Context, string) (postgresstore.A11ShadowPosture, error)
	Activate(context.Context, string) error
	Pause(context.Context, string) error
	CompleteStop(context.Context, string) error
	Fail(context.Context, string, string) error
}

type shadowSession interface {
	Run(context.Context) error
	SetEntriesEnabled(bool)
	FlushAvailable(context.Context) error
	Flush(context.Context) error
	Checkpoint(context.Context) error
}

type shadowSessionFactory func(context.Context, postgresstore.A11ShadowClaim) (shadowSession, error)

type shadowRoleWork struct {
	store     shadowRuntimeStore
	factory   shadowSessionFactory
	preflight func(context.Context) error
	interval  time.Duration
	ready     atomic.Bool
}

func newShadowRoleWork(store shadowRuntimeStore, factory shadowSessionFactory,
	interval time.Duration) (*shadowRoleWork, error) {
	if store == nil || factory == nil || interval <= 0 || interval > time.Minute {
		return nil, roleError("shadow_role_configuration_invalid")
	}
	return &shadowRoleWork{store: store, factory: factory, interval: interval}, nil
}

// Run continuously consumes durable shadow sessions until shutdown.
func (work *shadowRoleWork) Run(ctx context.Context, logger *slog.Logger) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	preflightComplete := work.preflight == nil
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if !preflightComplete {
				if err := work.preflight(ctx); err != nil {
					work.ready.Store(false)
					logger.Warn("shadow startup recovery pending", "event_code", "shadow_recovery_pending", "cause", err)
					timer.Reset(work.interval)
					continue
				}
				preflightComplete = true
			}
			claim, found, err := work.store.Claim(ctx)
			if err != nil {
				work.ready.Store(false)
				logger.Warn("shadow claim failed", "event_code", "shadow_claim_failed", "cause", err)
				timer.Reset(work.interval)
				continue
			}
			work.ready.Store(true)
			if found {
				work.runClaim(ctx, claim, logger)
			}
			timer.Reset(work.interval)
		}
	}
}

func (work *shadowRoleWork) runClaim(ctx context.Context, claim postgresstore.A11ShadowClaim, logger *slog.Logger) {
	session, err := work.factory(ctx, claim)
	if err != nil {
		_ = work.store.Fail(ctx, claim.ID, "shadow_composition_failed")
		return
	}
	sessionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- session.Run(sessionContext) }()
	ticker := time.NewTicker(work.interval)
	defer ticker.Stop()
	for {
		select {
		case err = <-result:
			if err != nil && ctx.Err() == nil {
				logger.Warn("shadow runtime failed", "event_code", "shadow_runtime_failed", "cause", err)
				flushContext, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = session.Flush(flushContext)
				flushCancel()
				_ = work.store.Fail(ctx, claim.ID, "shadow_runtime_failed")
			} else if ctx.Err() == nil {
				_ = work.store.Fail(ctx, claim.ID, "shadow_runtime_stopped")
			}
			return
		case <-ticker.C:
			if work.controlClaim(ctx, claim.ID, session, cancel) {
				<-result
				work.finishClaim(claim.ID, session, logger)
				return
			}
		case <-ctx.Done():
			session.SetEntriesEnabled(false)
			cancel()
			<-result
			flushContext, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = session.Flush(flushContext)
			flushCancel()
			return
		}
	}
}

func (work *shadowRoleWork) controlClaim(ctx context.Context, id string, session shadowSession,
	cancel context.CancelFunc) bool {
	if err := work.store.Renew(ctx, id); err != nil {
		session.SetEntriesEnabled(false)
		cancel()
		return true
	}
	posture, err := work.store.Posture(ctx, id)
	if err != nil {
		session.SetEntriesEnabled(false)
		return false
	}
	switch {
	case posture.State == "CANCEL_REQUESTED":
		session.SetEntriesEnabled(false)
		cancel()
		return true
	case posture.State == "PAUSED" && posture.RiskState == "NORMAL":
		if work.store.Activate(ctx, id) == nil {
			session.SetEntriesEnabled(true)
		}
	case posture.State == "RUNNING" && posture.RiskState != "NORMAL":
		session.SetEntriesEnabled(false)
		_ = work.store.Pause(ctx, id)
	case posture.State != "RUNNING":
		session.SetEntriesEnabled(false)
	}
	return false
}

func (work *shadowRoleWork) finishClaim(id string, session shadowSession, logger *slog.Logger) {
	flushContext, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer flushCancel()
	if err := session.Flush(flushContext); err != nil {
		logger.Warn("shadow stop flush failed", "event_code", "shadow_stop_flush_failed", "cause", err)
		_ = work.store.Fail(flushContext, id, "shadow_stop_failed")
		return
	}
	if err := session.Checkpoint(flushContext); err != nil {
		logger.Warn("shadow stop checkpoint failed", "event_code", "shadow_stop_checkpoint_failed", "cause", err)
		_ = work.store.Fail(flushContext, id, "shadow_stop_failed")
		return
	}
	if err := work.store.CompleteStop(flushContext, id); err != nil {
		logger.Warn("shadow stop completion failed", "event_code", "shadow_stop_completion_failed", "cause", err)
		_ = work.store.Fail(flushContext, id, "shadow_stop_failed")
	}
}

// Ready requires one successful authoritative durable-session poll.
func (work *shadowRoleWork) Ready() bool { return work != nil && work.ready.Load() }
