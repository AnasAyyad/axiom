package bootstrap

import (
	"context"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxPrivateStreamSignal struct {
	healthy      bool
	reconcileNow bool
	failureKind  exchangecontracts.ErrorKind
	causeCode    string
	fatal        error
}

func (work *sandboxEngineRoleWork) consumePrivateEvents(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
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
				ctx, store, account, fence, source, signals, err,
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
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	fence uint64,
	source sandbox.PrivateEventSource,
	signals chan<- sandboxPrivateStreamSignal,
	receiveErr error,
) bool {
	incidentStarted := time.Now()
	failureKind, causeCode := sandbox.ClassifyRecoveryFailure(receiveErr)
	if err := store.RecordEngineRuntimeRecoveryEvent(
		ctx, account.AccountID, account.Epoch, work.exchange, fence,
		"PRIVATE_STREAM", time.Since(incidentStarted), false,
		failureKind, causeCode, time.Now().UTC(),
	); err != nil {
		sendSandboxPrivateFatal(ctx, signals, err)
		return false
	}
	if !sendSandboxPrivateSignal(
		ctx, signals, sandboxPrivateStreamSignal{
			healthy: false, failureKind: failureKind, causeCode: causeCode,
		},
	) {
		return false
	}
	return work.reconnectSandboxPrivateEvents(
		ctx, store, account, fence, source, signals,
	)
}

func (work *sandboxEngineRoleWork) reconnectSandboxPrivateEvents(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	fence uint64,
	source sandbox.PrivateEventSource,
	signals chan<- sandboxPrivateStreamSignal,
) bool {
	reconnectStarted := time.Now()
	reconnectErr := reconnectSandboxPrivateSource(ctx, source)
	if reconnectErr != nil {
		if ctx.Err() != nil {
			return false
		}
		failureKind, causeCode := sandbox.ClassifyRecoveryFailure(reconnectErr)
		err := store.RecordEngineRuntimeRecoveryEvent(
			ctx, account.AccountID, account.Epoch, work.exchange, fence,
			"PRIVATE_RECONNECT", time.Since(reconnectStarted), false,
			failureKind, causeCode, time.Now().UTC(),
		)
		if err == nil {
			err = reconnectErr
		}
		sendSandboxPrivateFatal(ctx, signals, err)
		return false
	}
	err := store.RecordEngineRuntimeEvent(
		ctx, account.AccountID, account.Epoch, work.exchange, fence,
		"PRIVATE_RECONNECT", time.Since(reconnectStarted), true, time.Now().UTC(),
	)
	if err != nil {
		sendSandboxPrivateFatal(ctx, signals, err)
		return false
	}
	return sendSandboxPrivateSignal(
		ctx, signals, sandboxPrivateStreamSignal{
			healthy: true, reconcileNow: true,
		},
	)
}

func sendSandboxPrivateFatal(
	ctx context.Context,
	signals chan<- sandboxPrivateStreamSignal,
	err error,
) {
	sendSandboxPrivateSignal(ctx, signals, sandboxPrivateStreamSignal{fatal: err})
}

func reconnectSandboxPrivateSource(
	ctx context.Context,
	source sandbox.PrivateEventSource,
) error {
	return reconnectSandboxPrivateSourceAfter(
		ctx, source, time.Second, sandbox.ReconciliationRecoveryDeadline,
	)
}

func reconnectSandboxPrivateSourceAfter(
	ctx context.Context,
	source sandbox.PrivateEventSource,
	interval time.Duration,
	deadline time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source == nil || interval <= 0 || deadline <= 0 {
		return exchangecontracts.NewDetailedError(
			exchangecontracts.ErrorValidation,
			exchangecontracts.OperationStream,
			0,
			0,
			"private_reconnect_configuration_invalid",
		)
	}
	recoveryCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	attempt := time.NewTimer(0)
	defer attempt.Stop()
	for {
		select {
		case <-recoveryCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return exchangecontracts.NewDetailedError(
				exchangecontracts.ErrorTransient,
				exchangecontracts.OperationStream,
				0,
				0,
				"recovery_deadline_exceeded",
			)
		case <-attempt.C:
			err := source.Reconnect(recoveryCtx)
			if err == nil {
				return nil
			}
			kind, _ := sandbox.ClassifyRecoveryFailure(err)
			if !sandbox.PermittedRecoveryKind(kind) {
				return err
			}
			attempt.Reset(interval)
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
