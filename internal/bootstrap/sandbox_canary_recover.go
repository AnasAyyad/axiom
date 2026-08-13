package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxCanaryRecovery struct {
	status  sandbox.CanaryOrderStatus
	session sandbox.CanarySession
	cycle   uint64
	stages  map[sandbox.CanaryEvidenceStage]bool
}

// recoverSandboxCanaryPrepare resumes one exactly-once canary from durable
// state. It can only query, cancel the already-created order, and reconcile;
// it has no create path and receives no exchange credentials.
func recoverSandboxCanaryPrepare(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	configurationID string,
	exchange sandbox.Exchange,
	canaryID string,
) (string, error) {
	recovery, err := loadSandboxCanaryRecovery(
		ctx, store, configurationID, exchange, canaryID,
	)
	if err != nil {
		return "", err
	}
	if err = recoverSandboxCanaryQuery(ctx, store, recovery); err != nil {
		return "", err
	}
	status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
	if err != nil || !resumableCanaryStatus(status) {
		return "", fmt.Errorf("sandbox_canary_recovery_status_invalid")
	}
	recovery.status = status
	return finishRecoveredSandboxCanary(
		ctx, store, exchange, canaryID, recovery,
	)
}

func finishRecoveredSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID string,
	recovery sandboxCanaryRecovery,
) (string, error) {
	if !recoverableCanaryStatus(recovery.status) {
		if err := finishSandboxCanary(
			ctx,
			store,
			recovery.session,
			exchange,
			recovery.status.PlanID,
			recovery.cycle,
			recovery.status,
		); err != nil {
			return "", err
		}
		return completeSandboxCanaryRecovery(
			ctx, store, exchange, canaryID, recovery,
		)
	}
	if err := recoverSandboxCanaryTerminal(
		ctx, store, recovery,
	); err != nil {
		return "", err
	}
	if err := recoverSandboxCanaryReconciliation(
		ctx, store, recovery,
	); err != nil {
		return "", err
	}
	return completeSandboxCanaryRecovery(
		ctx, store, exchange, canaryID, recovery,
	)
}

func loadSandboxCanaryRecovery(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	configurationID string,
	exchange sandbox.Exchange,
	canaryID string,
) (sandboxCanaryRecovery, error) {
	status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
	if err != nil ||
		status.ConfigurationID != configurationID ||
		!resumableCanaryStatus(status) {
		return sandboxCanaryRecovery{},
			fmt.Errorf("sandbox_canary_recovery_identity_invalid")
	}
	records, err := store.ReadCanaryEvidence(ctx, exchange, canaryID)
	if err != nil {
		return sandboxCanaryRecovery{}, err
	}
	cycle, stages, err := recoverableCanaryEvidence(records)
	if err != nil {
		return sandboxCanaryRecovery{}, err
	}
	createCount, err := store.CountCanaryCreateEvidence(
		ctx, exchange, status.ApprovedAt, time.Now().UTC(),
	)
	if err != nil || createCount != 1 {
		return sandboxCanaryRecovery{},
			fmt.Errorf("sandbox_canary_create_evidence_invalid")
	}
	session := sandbox.CanarySession{
		ID:           status.SessionID,
		AccountID:    status.AccountID,
		AccountEpoch: status.AccountEpoch,
		Exchange:     exchange,
		StartupCycle: cycle,
	}
	return sandboxCanaryRecovery{
		status:  status,
		session: session,
		cycle:   cycle,
		stages:  stages,
	}, nil
}

func recoverSandboxCanaryQuery(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	recovery sandboxCanaryRecovery,
) error {
	if recovery.stages[sandbox.CanaryQuerySucceeded] {
		return nil
	}
	return querySandboxCanary(
		ctx,
		store,
		recovery.session,
		recovery.status.PlanID,
		recovery.cycle,
		recovery.status,
	)
}

func recoverSandboxCanaryTerminal(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	recovery sandboxCanaryRecovery,
) error {
	if recovery.stages[sandbox.CanaryCancelOrFillConfirmed] {
		return nil
	}
	return recordSandboxCanaryStage(
		ctx,
		store,
		recovery.session,
		recovery.status.PlanID,
		sandbox.CanaryCancelOrFillConfirmed,
		recovery.cycle,
		canaryHash(
			recovery.status.OrderState,
			string(recovery.status.OutboxState),
		),
	)
}

func recoverSandboxCanaryReconciliation(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	recovery sandboxCanaryRecovery,
) error {
	if recovery.stages[sandbox.CanaryReconciled] {
		return nil
	}
	reconcileHash, err := executeCanaryEngineCommand(
		ctx,
		store,
		recovery.status,
		sandbox.EngineCommandReconcile,
	)
	if err != nil {
		return err
	}
	return recordSandboxCanaryStage(
		ctx,
		store,
		recovery.session,
		recovery.status.PlanID,
		sandbox.CanaryReconciled,
		recovery.cycle,
		reconcileHash,
	)
}

func completeSandboxCanaryRecovery(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID string,
	recovery sandboxCanaryRecovery,
) (string, error) {
	if err := store.StopCanarySession(
		ctx,
		recovery.status.SessionID,
		recovery.status.AccountID,
		false,
		time.Now().UTC(),
	); err != nil {
		return "", err
	}
	records, err := store.ReadCanaryEvidence(ctx, exchange, canaryID)
	if err != nil {
		return "", err
	}
	if preparedCycle, complete := preparedCanaryCycle(records); !complete ||
		preparedCycle != recovery.cycle ||
		len(records) != 4 {
		return "", fmt.Errorf("sandbox_canary_recovery_evidence_incomplete")
	}
	return canaryID, nil
}

func recoverableCanaryStatus(status sandbox.CanaryOrderStatus) bool {
	return status.Attempt == 1 &&
		status.OutboxState == sandbox.OutboxTerminal &&
		(status.OrderState == "CANCELED" || status.OrderState == "FILLED")
}

func resumableCanaryStatus(status sandbox.CanaryOrderStatus) bool {
	if status.Attempt != 1 {
		return false
	}
	if recoverableCanaryStatus(status) {
		return true
	}
	switch status.OutboxState {
	case sandbox.OutboxUnknown:
		return status.OrderState == "UNKNOWN" ||
			status.OrderState == "RECOVERY_REQUIRED"
	case sandbox.OutboxAcknowledged:
		switch status.OrderState {
		case "ACKNOWLEDGED", "PARTIALLY_FILLED", "CANCEL_PENDING":
			return true
		}
	}
	return false
}

func recoverableCanaryEvidence(
	records []sandbox.CanaryEvidenceRecord,
) (
	uint64,
	map[sandbox.CanaryEvidenceStage]bool,
	error,
) {
	order := []sandbox.CanaryEvidenceStage{
		sandbox.CanaryPlanApproved,
		sandbox.CanaryQuerySucceeded,
		sandbox.CanaryCancelOrFillConfirmed,
		sandbox.CanaryReconciled,
	}
	stages := make(map[sandbox.CanaryEvidenceStage]bool, len(order))
	var cycle uint64
	highest := -1
	for _, record := range records {
		index := -1
		for candidate, stage := range order {
			if record.Stage == stage {
				index = candidate
				break
			}
		}
		if index < 0 ||
			stages[record.Stage] ||
			(cycle != 0 && record.StartupCycle != cycle) {
			return 0, nil, fmt.Errorf("sandbox_canary_recovery_evidence_invalid")
		}
		if cycle == 0 {
			cycle = record.StartupCycle
		}
		stages[record.Stage] = true
		if index > highest {
			highest = index
		}
	}
	if cycle == 0 || !stages[sandbox.CanaryPlanApproved] {
		return 0, nil, fmt.Errorf("sandbox_canary_recovery_evidence_invalid")
	}
	for index := 0; index <= highest; index++ {
		if !stages[order[index]] {
			return 0, nil, fmt.Errorf("sandbox_canary_recovery_evidence_invalid")
		}
	}
	return cycle, stages, nil
}
