package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"axiom/internal/exchanges/binance"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func (work *sandboxEngineRoleWork) processSandboxEngineCommands(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	owner string,
	fence uint64,
	adapter sandboxEngineAdapter,
	dispatcher *sandbox.SandboxDispatcher,
) error {
	commands, err := store.ClaimEngineCommands(
		ctx,
		account.AccountID,
		account.Epoch,
		owner,
		fence,
		time.Now().UTC(),
		4,
	)
	if err != nil {
		return err
	}
	for _, command := range commands {
		commandErr := work.processSandboxEngineCommand(
			ctx,
			store,
			account,
			fence,
			adapter,
			dispatcher,
			command.ID,
			command,
		)
		if commandErr != nil {
			return commandErr
		}
	}
	return nil
}

func (work *sandboxEngineRoleWork) processSandboxEngineCommand(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	fence uint64,
	adapter sandboxEngineAdapter,
	dispatcher *sandbox.SandboxDispatcher,
	commandID string,
	command sandbox.EngineCommand,
) error {
	evidenceHash, commandErr := work.executeSandboxEngineCommand(
		ctx, store, account, fence, adapter, dispatcher, command,
	)
	if evidenceHash == "" {
		outcome := "failed"
		code := binance.SandboxFailureCode(commandErr)
		if code == "" {
			code = sandboxEngineCommandFailureCode(commandErr)
		}
		if code != "" {
			outcome += ":" + code
		}
		evidenceHash = sandboxEngineCommandEvidence(command, outcome, 0)
	}
	err := store.CompleteEngineCommand(
		ctx, commandID, fence, commandErr == nil, evidenceHash, time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if commandErr != nil {
		return fmt.Errorf("sandbox_engine_command_failed")
	}
	return nil
}

var (
	errSandboxEngineQueryAdapter = errors.New("sandbox_engine_query_adapter")
	errSandboxEngineQueryPersist = errors.New("sandbox_engine_query_persist")
)

func sandboxEngineCommandFailureCode(err error) string {
	switch {
	case errors.Is(err, errSandboxEngineQueryAdapter):
		return "query_adapter"
	case errors.Is(err, errSandboxEngineQueryPersist):
		return "query_persist"
	default:
		return ""
	}
}

func (work *sandboxEngineRoleWork) executeSandboxEngineCommand(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	fence uint64,
	adapter sandboxEngineAdapter,
	dispatcher *sandbox.SandboxDispatcher,
	command sandbox.EngineCommand,
) (string, error) {
	switch command.Kind {
	case sandbox.EngineCommandQuery:
		return querySandboxEngineCommand(
			ctx, store, account, fence, adapter, command,
		)
	case sandbox.EngineCommandCancel:
		if err := dispatcher.Cancel(
			ctx,
			command.ClientOrderID,
			time.Now().UTC(),
		); err != nil {
			return "", err
		}
		return sandboxEngineCommandEvidence(
			command,
			"succeeded",
			1,
		), nil
	case sandbox.EngineCommandReconcile:
		result, err := work.reconcile(
			ctx,
			store,
			adapter,
			account,
		)
		if err != nil || result.State != "clean" {
			return "", fmt.Errorf("sandbox_engine_command_reconcile_failed")
		}
		return result.EvidenceHash, nil
	default:
		return "", fmt.Errorf("sandbox_engine_command_invalid")
	}
}

func querySandboxEngineCommand(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	account postgresstore.SandboxRuntimeEngineAccount,
	fence uint64,
	adapter sandboxEngineAdapter,
	command sandbox.EngineCommand,
) (string, error) {
	events, err := adapter.Query(
		ctx, account.AccountID, account.Epoch, command.ClientOrderID,
	)
	if err != nil {
		return "", errors.Join(errSandboxEngineQueryAdapter, err)
	}
	for _, event := range events {
		if err = store.AppendPrivateEvent(
			ctx, "", fence, event, sandbox.NoKillPoint{},
		); err != nil {
			return "", errors.Join(errSandboxEngineQueryPersist, err)
		}
	}
	return sandboxEngineCommandEvidence(
		command, "succeeded", len(events),
	), nil
}

func sandboxEngineCommandEvidence(
	command sandbox.EngineCommand,
	outcome string,
	factCount int,
) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		command.ID,
		string(command.AccountID),
		strconv.FormatUint(command.AccountEpoch, 10),
		string(command.Kind),
		outcome,
		strconv.Itoa(factCount),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
