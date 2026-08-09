package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func createFreshCanarySession(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	command sandbox.CanarySessionCommand,
) (sandbox.CanarySession, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		command.CreatedAt = time.Now().UTC()
		session, err := store.CreateCanarySession(ctx, command)
		if err == nil {
			return session, nil
		}
		if time.Now().After(deadline) {
			return sandbox.CanarySession{},
				fmt.Errorf("sandbox_canary_account_not_ready")
		}
		select {
		case <-ctx.Done():
			return sandbox.CanarySession{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func createCanaryArm(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	principal authentication.Principal,
	authorization authentication.ConsumedAuthorization,
	id string,
) (sandbox.Arm, error) {
	now := time.Now().UTC()
	arm := sandbox.Arm{
		ID: id, SessionID: session.ID,
		AccountIDs: []sandbox.AccountID{session.AccountID},
		AuthorizationHash: canaryHash(
			authorization.ID,
			authorization.SourceHash,
			authorization.ReasonHash,
		),
		ActorUserID:    principal.UserID,
		ActorSessionID: principal.SessionID,
		ReasonHash:     authorization.ReasonHash,
		CreatedAt:      now, ExpiresAt: now.Add(sandbox.ArmLifetime),
		Revision: 1,
	}
	return store.CreateSandboxArm(
		ctx,
		sandbox.ArmCommand{
			Arm: arm, AuthorizationID: authorization.ID,
			SourceHash:              authorization.SourceHash,
			ExpectedSessionRevision: session.Revision,
		},
	)
}

func waitCanaryAdmission(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	session sandbox.CanarySession,
	armID, instrument string,
	switches [4]bool,
) (
	sandbox.EligibilitySnapshot,
	sandbox.EntrySafetySnapshot,
	uint64,
	time.Time,
	error,
) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		approvedAt := time.Now().UTC()
		eligibility, safety, cycle, err := store.CanaryAdmission(
			ctx,
			session.ID,
			armID,
			session.AccountID,
			session.Exchange,
			instrument,
			approvedAt,
			switches,
		)
		if err == nil {
			return eligibility, safety, cycle, approvedAt, nil
		}
		if time.Now().After(deadline) {
			return sandbox.EligibilitySnapshot{},
				sandbox.EntrySafetySnapshot{}, 0, time.Time{},
				fmt.Errorf("sandbox_canary_admission_unavailable")
		}
		select {
		case <-ctx.Done():
			return sandbox.EligibilitySnapshot{},
				sandbox.EntrySafetySnapshot{}, 0, time.Time{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitCanaryOrderAttempt(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID string,
) (sandbox.CanaryOrderStatus, error) {
	deadline := time.Now().Add(time.Minute)
	for {
		status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
		if err == nil && status.Attempt == 1 &&
			(status.OutboxState == sandbox.OutboxAcknowledged ||
				status.OutboxState == sandbox.OutboxUnknown ||
				status.OutboxState == sandbox.OutboxTerminal) {
			return status, nil
		}
		if time.Now().After(deadline) {
			return sandbox.CanaryOrderStatus{},
				fmt.Errorf("sandbox_canary_submission_unconfirmed")
		}
		select {
		case <-ctx.Done():
			return sandbox.CanaryOrderStatus{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func executeCanaryEngineCommand(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	status sandbox.CanaryOrderStatus,
	kind sandbox.EngineCommandKind,
) (string, error) {
	id, err := randomCanaryIdentifier(
		"canary-" + string(kind),
	)
	if err != nil {
		return "", err
	}
	command := sandbox.EngineCommand{
		ID: id, AccountID: status.AccountID,
		AccountEpoch: status.AccountEpoch, Kind: kind,
		RequestedAt: time.Now().UTC(),
	}
	if kind == sandbox.EngineCommandQuery ||
		kind == sandbox.EngineCommandCancel {
		command.ClientOrderID = status.ClientOrderID
	}
	if err = store.QueueEngineCommand(ctx, command); err != nil {
		return "", err
	}
	deadline := time.Now().Add(time.Minute)
	for {
		state, evidenceHash, readErr :=
			store.ReadEngineCommandResult(ctx, id)
		if readErr == nil {
			switch state {
			case sandbox.EngineCommandSucceeded:
				if evidenceHash == "" {
					return "", fmt.Errorf("sandbox_canary_command_invalid")
				}
				return evidenceHash, nil
			case sandbox.EngineCommandFailed:
				return "", fmt.Errorf("sandbox_canary_command_failed")
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("sandbox_canary_command_timeout")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func canaryEvidenceFor(
	session sandbox.CanarySession,
	planID string,
	stage sandbox.CanaryEvidenceStage,
	cycle uint64,
	factHash string,
) sandbox.CanaryEvidence {
	return sandbox.CanaryEvidence{
		CanaryID: planID, Exchange: session.Exchange,
		AccountID: session.AccountID, AccountEpoch: session.AccountEpoch,
		SessionID: session.ID, PlanID: planID, Stage: stage,
		StartupCycle: cycle, FactHash: factHash,
		ObservedAt: time.Now().UTC(),
	}
}
