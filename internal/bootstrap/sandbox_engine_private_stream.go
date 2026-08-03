package bootstrap

import (
	"context"
	"time"

	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxPrivateStreamSignal struct {
	healthy bool
	fatal   error
}

func (work *sandboxEngineRoleWork) consumePrivateEvents(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	account postgresstore.V1CEngineAccount,
	fence uint64,
	source sandbox.PrivateEventSource,
	signals chan<- sandboxPrivateStreamSignal,
) {
	for {
		event, err := source.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !work.recoverSandboxPrivateEvents(
				ctx, store, account, fence, source, signals,
			) {
				return
			}
			continue
		}
		if err = store.AppendPrivateEvent(
			ctx, "", fence, event, sandbox.NoKillPoint{},
		); err != nil {
			sendSandboxPrivateSignal(
				ctx, signals, sandboxPrivateStreamSignal{fatal: err},
			)
			return
		}
	}
}

func (work *sandboxEngineRoleWork) recoverSandboxPrivateEvents(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	account postgresstore.V1CEngineAccount,
	fence uint64,
	source sandbox.PrivateEventSource,
	signals chan<- sandboxPrivateStreamSignal,
) bool {
	if !sendSandboxPrivateSignal(
		ctx, signals, sandboxPrivateStreamSignal{healthy: false},
	) {
		return false
	}
	reconnectStarted := time.Now()
	if !reconnectSandboxPrivateSource(ctx, source) {
		return false
	}
	err := store.RecordEngineRuntimeEvent(
		ctx, account.AccountID, account.Epoch, work.exchange, fence,
		"PRIVATE_RECONNECT", time.Since(reconnectStarted), true, time.Now().UTC(),
	)
	if err != nil {
		sendSandboxPrivateSignal(
			ctx, signals, sandboxPrivateStreamSignal{fatal: err},
		)
		return false
	}
	return sendSandboxPrivateSignal(
		ctx, signals, sandboxPrivateStreamSignal{healthy: true},
	)
}

func reconnectSandboxPrivateSource(
	ctx context.Context,
	source sandbox.PrivateEventSource,
) bool {
	return reconnectSandboxPrivateSourceAfter(ctx, source, time.Second)
}

func reconnectSandboxPrivateSourceAfter(
	ctx context.Context,
	source sandbox.PrivateEventSource,
	interval time.Duration,
) bool {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if source.Reconnect(ctx) == nil {
				return true
			}
		}
	}
}

func sendSandboxPrivateSignal(
	ctx context.Context,
	signals chan<- sandboxPrivateStreamSignal,
	signal sandboxPrivateStreamSignal,
) bool {
	select {
	case <-ctx.Done():
		return false
	case signals <- signal:
		return true
	}
}
