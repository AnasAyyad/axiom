package bootstrap

import (
	"context"
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
	work.finishClaim("shadow-a11", session)
	if store.activations != 1 || store.completions != 1 || !session.flushed || !session.checkpointed || session.entries {
		t.Fatalf("shadow control = %#v %#v", store, session)
	}
}

type shadowStoreStub struct {
	mutex       sync.Mutex
	postures    []postgresstore.A11ShadowPosture
	activations int
	completions int
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
func (*shadowStoreStub) Fail(context.Context, string, string) error { return nil }

type shadowSessionStub struct {
	entries      bool
	flushed      bool
	checkpointed bool
}

func (*shadowSessionStub) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
func (session *shadowSessionStub) SetEntriesEnabled(enabled bool) { session.entries = enabled }
func (session *shadowSessionStub) Flush(context.Context) error {
	session.flushed = true
	return nil
}
func (session *shadowSessionStub) Checkpoint(context.Context) error {
	session.checkpointed = true
	return nil
}
