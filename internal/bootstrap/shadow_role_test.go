package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	postgresstore "axiom/internal/storage/postgres"
)

func TestShadowRoleActivatesOnlyNormalRiskAndFlushesStop(t *testing.T) {
	store := &shadowStoreStub{postures: []postgresstore.A11ShadowPosture{
		{State: "PAUSED", RiskState: "NORMAL"}, {State: "CANCEL_REQUESTED", RiskState: "PAUSED"},
	}}
	session := &shadowSessionStub{}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.A11ShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if work.controlClaim(ctx, "shadow-a11", session, cancel) {
		t.Fatal("normal activation terminated session")
	}
	if !work.controlClaim(ctx, "shadow-a11", session, cancel) {
		t.Fatal("stop request did not terminate session")
	}
	work.finishClaim("shadow-a11", session, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if store.activations != 1 || store.completions != 1 || !session.flushed || !session.checkpointed || session.entries {
		t.Fatalf("shadow control = %#v %#v", store, session)
	}
}

func TestShadowRoleFailsClosedWhenStopFlushFails(t *testing.T) {
	store := &shadowStoreStub{}
	session := &shadowSessionStub{flushErr: errors.New("qualification flush failure")}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.A11ShadowClaim) (shadowSession, error) {
		return session, nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	work.finishClaim("shadow-a11", session, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if store.failures != 1 || store.failureReason != "shadow_stop_failed" || session.checkpointed || store.completions != 0 {
		t.Fatalf("failed stop = %#v %#v", store, session)
	}
}

type shadowStoreStub struct {
	mutex         sync.Mutex
	postures      []postgresstore.A11ShadowPosture
	activations   int
	completions   int
	failures      int
	failureReason string
}

func (*shadowStoreStub) Claim(context.Context) (postgresstore.A11ShadowClaim, bool, error) {
	return postgresstore.A11ShadowClaim{}, false, nil
}
func (*shadowStoreStub) Renew(context.Context, string) error { return nil }
func (store *shadowStoreStub) Posture(context.Context, string) (postgresstore.A11ShadowPosture, error) {
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
}

func (*shadowSessionStub) Run(ctx context.Context) error {
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
