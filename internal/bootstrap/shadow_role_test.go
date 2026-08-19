package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	postgresstore "axiom/internal/storage/postgres"
)

func TestShadowRoleActivatesOnlyNormalRiskAndFlushesStop(t *testing.T) {
	store := &shadowStoreStub{postures: []postgresstore.PublicShadowPosture{
		{State: "PAUSED", RiskState: "NORMAL", StoragePressure: "NORMAL"},
		{State: "CANCEL_REQUESTED", RiskState: "PAUSED", StoragePressure: "NORMAL"},
	}}
	session := &shadowSessionStub{}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	claim := postgresstore.PublicShadowClaim{ID: "shadow-owner_console"}
	if work.controlClaim(ctx, claim, session, cancel) != shadowClaimContinue {
		t.Fatal("normal activation terminated session")
	}
	if work.controlClaim(ctx, claim, session, cancel) != shadowClaimStopRequested {
		t.Fatal("stop request did not terminate session")
	}
	work.finishClaim("shadow-owner_console", session, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if store.activations != 1 || store.completions != 1 || !session.flushed || !session.checkpointed || session.entries {
		t.Fatalf("shadow control = %#v %#v", store, session)
	}
}

func TestShadowRoleDoesNotActivatePausedSessionAtHighPressure(t *testing.T) {
	store := &shadowStoreStub{postures: []postgresstore.PublicShadowPosture{
		{State: "PAUSED", RiskState: "NORMAL", StoragePressure: "HIGH"},
	}}
	session := &shadowSessionStub{entries: true}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if work.controlClaim(context.Background(), postgresstore.PublicShadowClaim{ID: "shadow-owner_console"}, session, func() {}) != shadowClaimContinue ||
		store.activations != 0 || session.entries {
		t.Fatalf("high pressure resumed paused session: store=%#v session=%#v", store, session)
	}
}

func TestShadowRoleKeepsRecoveredSessionPaused(t *testing.T) {
	store := &shadowStoreStub{postures: []postgresstore.PublicShadowPosture{
		{State: "PAUSED", RiskState: "NORMAL", StoragePressure: "NORMAL"},
	}}
	session := &shadowSessionStub{}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	claim := postgresstore.PublicShadowClaim{ID: "shadow-recovered", Recovery: true}
	if work.controlClaim(context.Background(), claim, session, func() {}) != shadowClaimContinue || store.activations != 0 || session.entries {
		t.Fatalf("recovered session left paused hold: store=%#v session=%#v", store, session)
	}
}

func TestShadowRoleFailsClosedWhenStopFlushFails(t *testing.T) {
	store := &shadowStoreStub{}
	session := &shadowSessionStub{flushErr: errors.New("qualification flush failure")}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	work.finishClaim("shadow-owner_console", session, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if store.failures != 1 || store.failureReason != "shadow_stop_failed" || session.checkpointed || store.completions != 0 {
		t.Fatalf("failed stop = %#v %#v", store, session)
	}
}

func TestShadowRoleGracefulShutdownCheckpointsBeforeLeaseRelease(t *testing.T) {
	store := &shadowStoreStub{}
	session := &shadowSessionStub{started: make(chan struct{})}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		work.runClaim(ctx, postgresstore.PublicShadowClaim{ID: "shadow-restart", ClaimEpoch: 7},
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	<-session.started
	cancel()
	<-done
	if !session.flushed || !session.checkpointed || store.releases != 1 || store.releaseEpoch != 7 || session.entries {
		t.Fatalf("graceful restart handoff = %#v %#v", store, session)
	}
}

func TestShadowRoleLeaseLossLocksWithoutUnsafeStopFinalization(t *testing.T) {
	store := &shadowStoreStub{renewErr: errors.New("database unavailable")}
	session := &shadowSessionStub{entries: true, started: make(chan struct{})}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return session, nil
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		work.runClaim(context.Background(), postgresstore.PublicShadowClaim{ID: "shadow-lease-loss", ClaimEpoch: 11},
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	<-session.started
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lease-loss shutdown did not complete")
	}
	if session.entries || session.flushed || session.checkpointed || store.completions != 0 ||
		store.failures != 0 || store.releases != 0 {
		t.Fatalf("lease loss attempted an unsafe terminal transition: store=%#v session=%#v", store, session)
	}
}

type shadowStoreStub struct {
	mutex         sync.Mutex
	postures      []postgresstore.PublicShadowPosture
	activations   int
	releases      int
	releaseEpoch  int64
	completions   int
	failures      int
	failureReason string
	renewErr      error
}

func (*shadowStoreStub) Claim(context.Context) (postgresstore.PublicShadowClaim, bool, error) {
	return postgresstore.PublicShadowClaim{}, false, nil
}
func (store *shadowStoreStub) Renew(context.Context, string) error { return store.renewErr }
func (store *shadowStoreStub) Posture(context.Context, string) (postgresstore.PublicShadowPosture, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	value := store.postures[0]
	store.postures = store.postures[1:]
	return value, nil
}
func (store *shadowStoreStub) Activate(context.Context, string) error {
	store.activations++
	return nil
}
func (*shadowStoreStub) Pause(context.Context, string) error { return nil }
func (store *shadowStoreStub) ReleaseForRestart(_ context.Context, _ string, epoch int64) error {
	store.releases++
	store.releaseEpoch = epoch
	return nil
}
func (store *shadowStoreStub) CompleteStop(context.Context, string) error {
	store.completions++
	return nil
}
func (store *shadowStoreStub) Fail(_ context.Context, _ string, reason string) error {
	store.failures++
	store.failureReason = reason
	return nil
}

type shadowSessionStub struct {
	entries      bool
	flushed      bool
	checkpointed bool
	flushErr     error
	started      chan struct{}
}

func (session *shadowSessionStub) Run(ctx context.Context) error {
	if session.started != nil {
		close(session.started)
	}
	<-ctx.Done()
	return nil
}
func (session *shadowSessionStub) SetEntriesEnabled(enabled bool) { session.entries = enabled }
func (*shadowSessionStub) FlushAvailable(context.Context) error   { return nil }
func (session *shadowSessionStub) Flush(context.Context) error {
	session.flushed = true
	return session.flushErr
}
func (session *shadowSessionStub) Checkpoint(context.Context) error {
	session.checkpointed = true
	return nil
}
