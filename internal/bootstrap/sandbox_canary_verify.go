package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxCanaryQualificationEvidence struct {
	SchemaVersion              string                         `json:"schema_version"`
	EvidenceID                 string                         `json:"evidence_id"`
	CanaryID                   string                         `json:"canary_id"`
	Exchange                   sandbox.Exchange               `json:"exchange"`
	AccountID                  sandbox.AccountID              `json:"account_id"`
	AccountEpoch               uint64                         `json:"account_epoch"`
	ConfigurationID            string                         `json:"configuration_id"`
	Build                      buildinfo.Info                 `json:"build"`
	ExecutableSHA256           string                         `json:"executable_sha256"`
	CreateRequestEvidenceCount int64                          `json:"create_request_evidence_count"`
	OutboxAttemptCount         uint32                         `json:"outbox_attempt_count"`
	DuplicateSubmissions       uint32                         `json:"duplicate_submissions"`
	ProfitabilityEvidence      bool                           `json:"profitability_evidence"`
	Qualified                  bool                           `json:"qualified"`
	CompletedAt                time.Time                      `json:"completed_at"`
	Stages                     []sandbox.CanaryEvidenceRecord `json:"stages"`
}

func verifySandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	_ config.Configuration,
	configurationID string,
	exchange sandbox.Exchange,
	canaryID, evidenceDirectory string,
) (string, error) {
	status, records, priorCycle, err := loadPreparedSandboxCanary(
		ctx, store, exchange, canaryID, configurationID,
	)
	if err != nil {
		return "", err
	}
	cycle, reconcileHash, status, err := verifyRestartedSandboxCanary(
		ctx, store, exchange, canaryID, status, priorCycle,
	)
	if err != nil {
		return "", err
	}
	createCount, build, executableSHA256, now, err :=
		inspectSandboxCanaryCandidate(ctx, store, exchange, status)
	if err != nil {
		return "", err
	}
	if err = recordSandboxCanaryRestart(
		ctx, store, records, status, exchange, canaryID, cycle,
		reconcileHash, createCount, build, configurationID, now,
	); err != nil {
		return "", err
	}
	records, err = finalizeSandboxCanary(
		ctx, store, status, exchange, canaryID,
	)
	if err != nil {
		return "", err
	}
	evidence := newSandboxCanaryQualificationEvidence(
		status, records, exchange, canaryID, configurationID,
		build, executableSHA256, createCount,
	)
	return writeSandboxCanaryEvidence(evidenceDirectory, &evidence)
}

func loadPreparedSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID, configurationID string,
) (
	sandbox.CanaryOrderStatus,
	[]sandbox.CanaryEvidenceRecord,
	uint64,
	error,
) {
	status, err := store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
	if err != nil || status.ConfigurationID != configurationID {
		return sandbox.CanaryOrderStatus{}, nil, 0,
			fmt.Errorf("sandbox_canary_verification_identity_invalid")
	}
	records, err := store.ReadCanaryEvidence(ctx, exchange, canaryID)
	if err != nil {
		return sandbox.CanaryOrderStatus{}, nil, 0, err
	}
	priorCycle, complete := preparedCanaryCycle(records)
	if !complete {
		return sandbox.CanaryOrderStatus{}, nil, 0,
			fmt.Errorf("sandbox_canary_prepare_evidence_incomplete")
	}
	return status, records, priorCycle, nil
}

func verifyRestartedSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	canaryID string,
	status sandbox.CanaryOrderStatus,
	priorCycle uint64,
) (uint64, string, sandbox.CanaryOrderStatus, error) {
	cycle, err := waitCanaryRestart(ctx, store, status, priorCycle)
	if err != nil {
		return 0, "", sandbox.CanaryOrderStatus{}, err
	}
	if _, err = executeCanaryEngineCommand(
		ctx, store, status, sandbox.EngineCommandQuery,
	); err != nil {
		return 0, "", sandbox.CanaryOrderStatus{}, err
	}
	reconcileHash, err := executeCanaryEngineCommand(
		ctx, store, status, sandbox.EngineCommandReconcile,
	)
	if err != nil {
		return 0, "", sandbox.CanaryOrderStatus{}, err
	}
	status, err = store.ReadCanaryOrderStatus(ctx, exchange, canaryID)
	if err != nil || status.Attempt != 1 {
		return 0, "", sandbox.CanaryOrderStatus{},
			fmt.Errorf("sandbox_canary_duplicate_submission_detected")
	}
	return cycle, reconcileHash, status, nil
}

func inspectSandboxCanaryCandidate(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	exchange sandbox.Exchange,
	status sandbox.CanaryOrderStatus,
) (int64, buildinfo.Info, string, time.Time, error) {
	now := time.Now().UTC()
	createCount, err := store.CountCanaryCreateEvidence(
		ctx, exchange, status.ApprovedAt, now,
	)
	if err != nil || createCount != 1 {
		return 0, buildinfo.Info{}, "", time.Time{},
			fmt.Errorf("sandbox_canary_create_evidence_invalid")
	}
	build := buildinfo.Current()
	executableSHA256, err := currentExecutableSHA256()
	if build.Commit == "unknown" || err != nil {
		return 0, buildinfo.Info{}, "", time.Time{},
			fmt.Errorf("sandbox_canary_build_identity_invalid")
	}
	return createCount, build, executableSHA256, now, nil
}

func recordSandboxCanaryRestart(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	records []sandbox.CanaryEvidenceRecord,
	status sandbox.CanaryOrderStatus,
	exchange sandbox.Exchange,
	canaryID string,
	cycle uint64,
	reconcileHash string,
	createCount int64,
	build buildinfo.Info,
	configurationID string,
	now time.Time,
) error {
	if hasCanaryStage(records, sandbox.CanaryRestartVerified) {
		return nil
	}
	factHash := canaryHash(
		reconcileHash, fmt.Sprint(createCount), fmt.Sprint(status.Attempt),
		build.Commit, configurationID,
	)
	_, err := store.RecordCanaryEvidence(ctx, sandbox.CanaryEvidence{
		CanaryID: canaryID, Exchange: exchange,
		AccountID: status.AccountID, AccountEpoch: status.AccountEpoch,
		SessionID: status.SessionID, PlanID: status.PlanID,
		Stage: sandbox.CanaryRestartVerified, StartupCycle: cycle,
		FactHash: factHash, ObservedAt: now,
	})
	return err
}

func finalizeSandboxCanary(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	status sandbox.CanaryOrderStatus,
	exchange sandbox.Exchange,
	canaryID string,
) ([]sandbox.CanaryEvidenceRecord, error) {
	if err := store.StopCanarySession(
		ctx, status.SessionID, status.AccountID, false, time.Now().UTC(),
	); err != nil {
		return nil, err
	}
	records, err := store.ReadCanaryEvidence(ctx, exchange, canaryID)
	if err != nil || !completeCanaryEvidence(records) {
		return nil, fmt.Errorf("sandbox_canary_evidence_incomplete")
	}
	return records, nil
}

func newSandboxCanaryQualificationEvidence(
	status sandbox.CanaryOrderStatus,
	records []sandbox.CanaryEvidenceRecord,
	exchange sandbox.Exchange,
	canaryID, configurationID string,
	build buildinfo.Info,
	executableSHA256 string,
	createCount int64,
) sandboxCanaryQualificationEvidence {
	return sandboxCanaryQualificationEvidence{
		SchemaVersion: "axiom.sandbox_runtime.sandbox_connectivity.canary-evidence.v1",
		CanaryID:      canaryID, Exchange: exchange,
		AccountID: status.AccountID, AccountEpoch: status.AccountEpoch,
		ConfigurationID: configurationID, Build: build,
		ExecutableSHA256:           executableSHA256,
		CreateRequestEvidenceCount: createCount,
		OutboxAttemptCount:         status.Attempt, DuplicateSubmissions: 0,
		ProfitabilityEvidence: false, Qualified: true,
		CompletedAt: time.Now().UTC(), Stages: records,
	}
}

func currentExecutableSHA256() (string, error) {
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func waitCanaryRestart(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	status sandbox.CanaryOrderStatus,
	priorCycle uint64,
) (uint64, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		cycle, err := store.ReadyCanaryRestartCycle(
			ctx,
			status.AccountID,
			status.AccountEpoch,
			priorCycle,
			time.Now().UTC(),
		)
		if err == nil {
			return cycle, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("sandbox_canary_restart_not_verified")
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func preparedCanaryCycle(
	records []sandbox.CanaryEvidenceRecord,
) (uint64, bool) {
	required := map[sandbox.CanaryEvidenceStage]bool{
		sandbox.CanaryPlanApproved:          false,
		sandbox.CanaryQuerySucceeded:        false,
		sandbox.CanaryCancelOrFillConfirmed: false,
		sandbox.CanaryReconciled:            false,
	}
	var cycle uint64
	for _, record := range records {
		if _, exists := required[record.Stage]; exists {
			required[record.Stage] = true
			if cycle == 0 {
				cycle = record.StartupCycle
			} else if cycle != record.StartupCycle {
				return 0, false
			}
		}
	}
	for _, present := range required {
		if !present {
			return 0, false
		}
	}
	return cycle, cycle > 0
}

func completeCanaryEvidence(records []sandbox.CanaryEvidenceRecord) bool {
	_, prepared := preparedCanaryCycle(records)
	return prepared &&
		hasCanaryStage(records, sandbox.CanaryRestartVerified) &&
		len(records) == 5
}

func hasCanaryStage(
	records []sandbox.CanaryEvidenceRecord,
	stage sandbox.CanaryEvidenceStage,
) bool {
	for _, record := range records {
		if record.Stage == stage {
			return true
		}
	}
	return false
}
