package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func recordSandboxCanaryStage(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	planID string,
	stage sandbox.CanaryEvidenceStage,
	cycle uint64,
	factHash string,
) error {
	_, err := store.RecordCanaryEvidence(
		ctx,
		canaryEvidenceFor(
			session, planID, stage, cycle, factHash,
		),
	)
	return err
}

func exerciseSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	exchange sandbox.Exchange,
	plan sandbox.ApprovedSandboxPlan,
	cycle uint64,
) error {
	if err := recordSandboxCanaryStage(
		ctx, store, session, plan.ID, sandbox.CanaryPlanApproved,
		cycle, plan.ApprovalHash,
	); err != nil {
		return err
	}
	status, err := waitCanaryOrderAttempt(
		ctx, store, exchange, plan.ID,
	)
	if err != nil {
		return err
	}
	if err = requireSandboxCanaryCreateEvidence(
		ctx,
		store,
		exchange,
		status,
	); err != nil {
		return err
	}
	if err = querySandboxCanary(
		ctx, store, session, plan.ID, cycle, status,
	); err != nil {
		return err
	}
	status, err = store.ReadCanaryOrderStatus(ctx, exchange, plan.ID)
	if err != nil {
		return err
	}
	return finishSandboxCanary(
		ctx,
		store,
		session,
		exchange,
		plan.ID,
		cycle,
		status,
	)
}

func querySandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	planID string,
	cycle uint64,
	status sandbox.CanaryOrderStatus,
) error {
	queryHash, err := executeCanaryEngineCommand(
		ctx, store, status, sandbox.EngineCommandQuery,
	)
	if err != nil {
		return err
	}
	return recordSandboxCanaryStage(
		ctx, store, session, planID, sandbox.CanaryQuerySucceeded,
		cycle, queryHash,
	)
}

func requireSandboxCanaryCreateEvidence(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	status sandbox.CanaryOrderStatus,
) error {
	count, err := store.CountCanaryCreateEvidence(
		ctx,
		exchange,
		status.ApprovedAt,
		time.Now().UTC(),
	)
	if err != nil || count != 1 {
		return fmt.Errorf("sandbox_canary_create_evidence_invalid")
	}
	return nil
}

func finishSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	exchange sandbox.Exchange,
	planID string,
	cycle uint64,
	status sandbox.CanaryOrderStatus,
) error {
	if status.OutboxState != sandbox.OutboxTerminal {
		if _, err := executeCanaryEngineCommand(
			ctx, store, status, sandbox.EngineCommandCancel,
		); err != nil {
			return err
		}
	}
	status, err := waitCanaryCancelOrFill(
		ctx,
		store,
		exchange,
		planID,
	)
	if err != nil {
		return err
	}
	cancelOrFillHash := canaryHash(
		status.OrderState,
		string(status.OutboxState),
	)
	if err = recordSandboxCanaryStage(
		ctx, store, session, planID, sandbox.CanaryCancelOrFillConfirmed,
		cycle, cancelOrFillHash,
	); err != nil {
		return err
	}
	reconcileHash, err := executeCanaryEngineCommand(
		ctx, store, status, sandbox.EngineCommandReconcile,
	)
	if err != nil {
		return err
	}
	return recordSandboxCanaryStage(
		ctx, store, session, planID, sandbox.CanaryReconciled,
		cycle, reconcileHash,
	)
}

func waitCanaryCancelOrFill(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID string,
) (sandbox.CanaryOrderStatus, error) {
	deadline := time.Now().Add(time.Minute)
	for {
		status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
		if err == nil {
			if status.Attempt != 1 {
				return sandbox.CanaryOrderStatus{},
					fmt.Errorf("sandbox_canary_duplicate_submission_detected")
			}
			if status.OutboxState == sandbox.OutboxTerminal {
				if canaryCancelOrFillConfirmed(status) {
					return status, nil
				}
				return sandbox.CanaryOrderStatus{},
					fmt.Errorf("sandbox_canary_cancel_or_fill_unconfirmed")
			}
		}
		if time.Now().After(deadline) {
			return sandbox.CanaryOrderStatus{},
				fmt.Errorf("sandbox_canary_cancel_or_fill_unconfirmed")
		}
		select {
		case <-ctx.Done():
			return sandbox.CanaryOrderStatus{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func canaryCancelOrFillConfirmed(status sandbox.CanaryOrderStatus) bool {
	return status.Attempt == 1 &&
		status.OutboxState == sandbox.OutboxTerminal &&
		(status.OrderState == "CANCELED" || status.OrderState == "FILLED")
}
