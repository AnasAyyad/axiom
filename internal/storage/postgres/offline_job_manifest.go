package postgres

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/replay"
)

func decodeOwnerConsoleOfflineRequest(kind string, payload json.RawMessage) (ownerConsoleOfflineRequest, error) {
	var request ownerConsoleOfflineRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) == nil {
		return request, fmt.Errorf("owner_console_job_request_invalid")
	}
	seed, err := hex.DecodeString(request.RootSeedHash)
	if (kind != "backtest" && kind != "replay") || request.ConfigurationID == "" || request.DatasetID == "" ||
		request.ResearchGenerationID == "" ||
		!ownerConsoleOfflineStrategySupported(request.StrategyVersion) || err != nil || len(seed) != sha256.Size {
		return request, fmt.Errorf("owner_console_job_request_invalid")
	}
	if (request.FirstOrdinal == nil) != (request.LastOrdinal == nil) {
		return request, fmt.Errorf("owner_console_job_window_invalid")
	}
	evaluationRequest := request.EvaluationCampaignID != "" || request.EvaluationMemberID != "" ||
		request.ConfigurationKey != "" || request.CapitalMicros != 0 || request.CostStressBPS != 0
	if evaluationRequest && (request.EvaluationCampaignID == "" || request.EvaluationMemberID == "" ||
		request.ConfigurationKey == "" || !evaluationCapitalAllowed(request.CapitalMicros) ||
		(request.CostStressBPS != 10_000 && request.CostStressBPS != 15_000 && request.CostStressBPS != 20_000)) {
		return request, fmt.Errorf("owner_console_evaluation_job_request_invalid")
	}
	if kind == "backtest" && (request.IncidentID != nil || request.Speed != nil ||
		(request.FirstOrdinal != nil && !evaluationRequest)) {
		return request, fmt.Errorf("owner_console_job_request_invalid")
	}
	if kind == "replay" {
		if request.Speed != nil && *request.Speed != "original" && *request.Speed != "accelerated" && *request.Speed != "maximum" {
			return request, fmt.Errorf("owner_console_job_request_invalid")
		}
		if request.IncidentID != nil && (*request.IncidentID == "" || request.FirstOrdinal == nil) {
			return request, fmt.Errorf("owner_console_job_window_invalid")
		}
	}
	return request, nil
}

func evaluationCapitalAllowed(value int64) bool {
	switch value {
	case 500_000_000, 1_000_000_000, 1_500_000_000, 2_000_000_000, 10_000_000_000:
		return true
	default:
		return false
	}
}

func ownerConsoleRunManifest(jobID, kind string, request ownerConsoleOfflineRequest, configuration config.Configuration, configurationHash string,
	dataset backtest.DatasetDescriptor, namespace backtest.ModelNamespace, timing replay.TimingMode,
	acceleration uint64) (backtest.RunManifest, error) {
	build := buildinfo.Current()
	if build.Dirty || !ownerConsoleBuildIdentityValid(build.Commit, build.GoSumHash, build.PNPMLockHash) {
		return backtest.RunManifest{}, fmt.Errorf("owner_console_job_build_identity_invalid")
	}
	runID, err := domain.NewRunID(jobID)
	if err != nil {
		return backtest.RunManifest{}, fmt.Errorf("owner_console_job_run_identity_invalid")
	}
	startingPayload, _ := json.Marshal(struct {
		Asset    string `json:"asset"`
		Quantity string `json:"quantity"`
	}{Asset: configuration.Portfolio.SettlementAsset, Quantity: configuration.Portfolio.StartingCapital.Value})
	manifest := backtest.RunManifest{RunID: runID, Mode: kind, CodeCommit: build.Commit,
		Build: backtest.CurrentBuildIdentity([]string{"trimpath"}, build.GoSumHash, build.PNPMLockHash), Dataset: dataset,
		ConfigurationHash: configurationHash, StrategyVersion: request.StrategyVersion, Seed: request.RootSeedHash,
		ResearchGenerationID: request.ResearchGenerationID,
		SchedulerVersion:     fmt.Sprintf("deterministic-scheduler-v1:%s:%d", timing, acceleration),
		SerializationVersion: "canonical-json-v1",
		Models:               namespace, StartingBalanceHash: ownerConsoleSHA256(startingPayload)}
	if request.EvaluationCampaignID != "" {
		manifest.Evaluation = &backtest.EvaluationRunIdentity{CampaignID: request.EvaluationCampaignID,
			MemberID: request.EvaluationMemberID, ConfigurationKey: request.ConfigurationKey,
			CapitalMicros: request.CapitalMicros, CostStressBPS: request.CostStressBPS}
	}
	return manifest, nil
}

func ownerConsoleOfflineStrategySupported(value string) bool {
	switch value {
	case "trend-following@1.0.0", "mean-reversion@1.0.0", "triangular-arbitrage@1.0.0", "cross-exchange-arbitrage@1.0.0",
		"inventory-rebalancing@1.0.0":
		return true
	default:
		return false
	}
}

func ownerConsoleConfigurationMatchesStrategy(configuration config.Configuration, version string) bool {
	switch version {
	case "trend-following@1.0.0":
		return configuration.Trend.StrategyVersion == version
	case "mean-reversion@1.0.0":
		return configuration.MeanReversion.StrategyVersion == version
	case "triangular-arbitrage@1.0.0":
		return configuration.Triangular.StrategyVersion == version
	case "cross-exchange-arbitrage@1.0.0":
		return configuration.CrossExchange.StrategyVersion == version
	case "inventory-rebalancing@1.0.0":
		return config.ValidateRebalancingConfiguration(configuration.Rebalancing) == nil
	default:
		return false
	}
}

func offlineJobTiming(kind string, speed *string) (replay.TimingMode, uint64, error) {
	if kind == "backtest" {
		return replay.MaximumTiming, 1, nil
	}
	selected := "original"
	if speed != nil {
		selected = *speed
	}
	switch selected {
	case "original":
		return replay.OriginalTiming, 1, nil
	case "accelerated":
		return replay.AcceleratedTiming, 10, nil
	case "maximum":
		return replay.MaximumTiming, 1, nil
	default:
		return "", 0, fmt.Errorf("owner_console_job_timing_invalid")
	}
}

func ownerConsoleReplaySource(reader *backtest.DatasetReader, request ownerConsoleOfflineRequest) (replay.Source, error) {
	if request.FirstOrdinal == nil {
		return backtest.NewDatasetSource(reader)
	}
	first, firstErr := strconv.ParseUint(*request.FirstOrdinal, 10, 64)
	last, lastErr := strconv.ParseUint(*request.LastOrdinal, 10, 64)
	if firstErr != nil || lastErr != nil {
		return nil, fmt.Errorf("owner_console_job_window_invalid")
	}
	return backtest.NewDatasetWindowSource(reader, first, last)
}

func ownerConsoleBuildIdentityValid(values ...string) bool {
	for _, value := range values {
		decoded, err := hex.DecodeString(value)
		if err != nil || (len(decoded) != sha256.Size && len(decoded) != 20) {
			return false
		}
	}
	return true
}

func ownerConsoleSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
