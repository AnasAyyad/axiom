package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func abortSandboxCanary(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	configurationID string,
	exchange sandbox.Exchange,
	canaryID string,
) (string, error) {
	status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
	if err != nil || status.ConfigurationID != configurationID {
		return "", fmt.Errorf("sandbox_canary_abort_identity_invalid")
	}
	if !canaryAbortable(status) {
		return "", fmt.Errorf("sandbox_canary_abort_not_terminal")
	}
	if err = store.StopCanarySession(
		ctx,
		status.SessionID,
		status.AccountID,
		false,
		time.Now().UTC(),
	); err != nil {
		return "", err
	}
	return canaryID, nil
}

func canaryAbortable(status sandbox.CanaryOrderStatus) bool {
	if status.Attempt != 1 || status.OutboxState != sandbox.OutboxTerminal {
		return false
	}
	switch status.OrderState {
	case "CANCELED", "FILLED", "REJECTED":
		return true
	default:
		return false
	}
}
