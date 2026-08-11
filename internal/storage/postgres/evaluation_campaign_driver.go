package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type evaluationStageAdvancer interface {
	Advance(context.Context, evaluation.Campaign) (evaluation.StageProgress, error)
}

type evaluationReportStage struct {
	Stage, State, Reason string
	Attempt              int
	CheckpointHash       string
}

type evaluationReportMember struct {
	ID, Strategy, Configuration, Mode, State, Verdict, Reason, RunID, ResultHash string
	CapitalMicros, CostStressBPS, RepeatOrdinal                                  int64
	Metrics                                                                      json.RawMessage
	RunManifest                                                                  json.RawMessage
}

type evaluationReportStress struct {
	Scenario, State, Reason, EvidenceHash string
	Payload                               json.RawMessage
}

// EvaluationCampaignDriver binds the policy state machine to the existing
// durable import, audit, recorder, offline-run, shadow, and report stores.
type EvaluationCampaignDriver struct {
	pool       *pgxpool.Pool
	root       string
	clock      domain.Clock
	historical evaluationStageAdvancer
	audit      evaluationStageAdvancer
	shadow     evaluationStageAdvancer
	recorder   *EvaluationRecorderControlStore
	base       config.Configuration
}

// NewEvaluationCampaignDriver constructs the durable campaign-stage adapter.
func NewEvaluationCampaignDriver(pool *pgxpool.Pool, root string, clock domain.Clock, base config.Configuration,
	historical, audit, shadow evaluationStageAdvancer) (*EvaluationCampaignDriver, error) {
	clean := filepath.Clean(root)
	if pool == nil || !filepath.IsAbs(clean) || clean == string(filepath.Separator) || clock == nil ||
		historical == nil || audit == nil || shadow == nil || config.Validate(base) != nil || len(base.PublicExchanges()) != 2 {
		return nil, fmt.Errorf("evaluation_campaign_driver_dependencies_missing")
	}
	recorder, err := NewEvaluationRecorderControlStore(pool, clock)
	if err != nil {
		return nil, err
	}
	return &EvaluationCampaignDriver{pool: pool, root: clean, clock: clock,
		historical: historical, audit: audit, shadow: shadow, recorder: recorder, base: base}, nil
}

// HistoricalImport advances the campaign's official-candle import stage.
func (driver *EvaluationCampaignDriver) HistoricalImport(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	return driver.historical.Advance(ctx, campaign)
}

// ExistingDataAudit advances the campaign's preserved-recording audit stage.
func (driver *EvaluationCampaignDriver) ExistingDataAudit(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	return driver.audit.Advance(ctx, campaign)
}

// RotateRecorder advances the campaign-bound recorder rotation stage.
func (driver *EvaluationCampaignDriver) RotateRecorder(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	rotation, err := driver.recorder.Rotation(ctx, campaign.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		baseline, sizeErr := evaluationFilesystemBytes(driver.root)
		if sizeErr != nil {
			return evaluation.StageProgress{}, sizeErr
		}
		rotation, err = driver.recorder.EnsureRotation(ctx, campaign, baseline)
	}
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	checkpoint, _ := json.Marshal(map[string]any{"session_id": rotation.DesiredSessionID,
		"state": rotation.State, "recorded_bytes": rotation.RecordedBytes})
	switch rotation.State {
	case "REQUESTED", "FINALIZED", "ACTIVATING":
		return evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary: "Recorder rotation is advancing at a safe segment boundary.", Checkpoint: checkpoint,
			LinkedResourceType: "recorder_session", LinkedResourceID: rotation.DesiredSessionID}, nil
	case "ACTIVE", "PAUSED":
		return evaluation.StageProgress{State: evaluation.ProgressComplete,
			Summary: "Fresh campaign-bound Binance and Bybit recorder session is active.", Checkpoint: checkpoint,
			LinkedResourceType: "recorder_session", LinkedResourceID: rotation.DesiredSessionID}, nil
	case "BLOCKED":
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonPersistenceFailed,
			Summary: "Recorder rotation stopped before a safe campaign session could start.", Checkpoint: checkpoint}, nil
	default:
		return evaluation.StageProgress{}, fmt.Errorf("evaluation_recorder_rotation_state_invalid")
	}
}

// QualifyRecorder evaluates fresh recorder evidence and its shadow reserve.
func (driver *EvaluationCampaignDriver) QualifyRecorder(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	qualification, err := driver.recorder.Qualification(ctx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	checkpoint := recorderQualificationCheckpoint(qualification)
	if progress, terminal, checkErr := driver.preReserveQualification(ctx, campaign.ID, qualification, checkpoint); terminal || checkErr != nil {
		return progress, checkErr
	}
	reserve, err := driver.recorder.ProtectShadowReserve(ctx, campaign.ID)
	if err != nil {
		return driver.shadowReserveFailure(ctx, campaign.ID, err)
	}
	checkpoint, _ = json.Marshal(map[string]any{"valid_seconds": qualification.ValidSeconds,
		"recorded_bytes": qualification.RecordedBytes, "measured_bytes_per_hour": qualification.MeasuredBytesPerHour,
		"shadow_reserved_bytes": reserve, "observation_count": qualification.ObservationCount})
	return evaluation.StageProgress{State: evaluation.ProgressComplete,
		Summary:    "Seventy-two valid recorder hours are qualified and the full shadow reserve is protected.",
		Checkpoint: checkpoint}, nil
}

func (driver *EvaluationCampaignDriver) preReserveQualification(ctx context.Context, campaignID string,
	qualification EvaluationRecorderQualification, checkpoint []byte) (evaluation.StageProgress, bool, error) {
	if qualification.State == "BLOCKED" {
		reason := qualification.Reason
		if reason == "" {
			reason = evaluation.ReasonPersistenceFailed
		}
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: reason,
			Summary: "Fresh recorder qualification is blocked with preserved evidence.", Checkpoint: checkpoint}, true, nil
	}
	if qualification.LossObserved {
		if err := driver.recorder.Block(ctx, campaignID, evaluation.ReasonDataCorrupt); err != nil {
			return evaluation.StageProgress{}, true, err
		}
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonDataCorrupt,
			Summary:    "Queue loss, a source gap, or a decoder failure invalidated recorder qualification.",
			Checkpoint: checkpoint}, true, nil
	}
	now := driver.clock.Now().UTC
	healthy := qualification.LatestAllEligible && qualification.LatestPersistence &&
		!qualification.LastObservedAt.IsZero() && now.Sub(qualification.LastObservedAt) <= 2*time.Minute
	if qualification.ObservationCount == 0 || !healthy {
		return evaluation.StageProgress{State: evaluation.ProgressPause, Reason: evaluation.ReasonDataUnavailable,
			Summary:    "Valid-time clock is paused until all six public feeds, clocks, and persistence recover.",
			Checkpoint: checkpoint}, true, nil
	}
	if qualification.ValidSeconds < int64(evaluation.RequiredRecordingValidTime/time.Second) {
		return evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary:    "Fresh simultaneous Binance and Bybit evidence is accumulating valid time.",
			Checkpoint: checkpoint}, true, nil
	}
	return evaluation.StageProgress{}, false, nil
}

func (driver *EvaluationCampaignDriver) shadowReserveFailure(ctx context.Context, campaignID string,
	reserveErr error) (evaluation.StageProgress, error) {
	latest, readErr := driver.recorder.Qualification(ctx, campaignID)
	if readErr == nil && latest.State == "BLOCKED" && latest.Reason == evaluation.ReasonStorageInsufficient {
		return evaluation.StageProgress{State: evaluation.ProgressBlock,
			Reason:     evaluation.ReasonStorageInsufficient,
			Summary:    "The 72-hour evidence and protected seven-day shadow reserve cannot fit under 200 GiB.",
			Checkpoint: recorderQualificationCheckpoint(latest)}, nil
	}
	return evaluation.StageProgress{}, reserveErr
}

func recorderQualificationCheckpoint(value EvaluationRecorderQualification) []byte {
	payload, _ := json.Marshal(map[string]any{"state": value.State, "valid_seconds": value.ValidSeconds,
		"recorded_bytes": value.RecordedBytes, "measured_bytes_per_hour": value.MeasuredBytesPerHour,
		"shadow_reserved_bytes": value.ShadowReservedBytes, "observation_count": value.ObservationCount,
		"last_observed_at": value.LastObservedAt, "all_feeds_eligible": value.LatestAllEligible,
		"persistence_healthy": value.LatestPersistence, "loss_observed": value.LossObserved})
	return payload
}

func evaluationFilesystemBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() < 0 || total > int64(^uint64(0)>>1)-info.Size() {
			return fmt.Errorf("evaluation_storage_baseline_overflow")
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// OfflineMatrix advances either the campaign backtest or replay matrix.
func (driver *EvaluationCampaignDriver) OfflineMatrix(ctx context.Context, campaign evaluation.Campaign,
	mode string) (evaluation.StageProgress, error) {
	return driver.advanceOfflineMatrix(ctx, campaign, mode)
}

// SelectCandidates locks eligible configurations before final-test evidence opens.
func (driver *EvaluationCampaignDriver) SelectCandidates(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	return driver.selectEvaluationCandidates(ctx, campaign)
}

// CombinedShadow advances the single multi-member simulated shadow campaign.
func (driver *EvaluationCampaignDriver) CombinedShadow(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	return driver.advanceCombinedShadow(ctx, campaign)
}

// BuildReport builds the immutable full or partial campaign evidence report.
func (driver *EvaluationCampaignDriver) BuildReport(ctx context.Context, campaign evaluation.Campaign,
	partial bool) (evaluation.Report, error) {
	stages, err := driver.loadEvaluationReportStages(ctx, campaign.ID)
	if err != nil {
		return evaluation.Report{}, err
	}
	stress, err := driver.loadEvaluationReportStress(ctx, campaign.ID)
	if err != nil {
		return evaluation.Report{}, err
	}
	members, err := driver.loadEvaluationReportMembers(ctx, campaign.ID)
	if err != nil {
		return evaluation.Report{}, err
	}
	operational, err := driver.loadEvaluationReportEvidence(ctx, campaign.ID)
	if err != nil {
		return evaluation.Report{}, err
	}
	verdict, reason := reportVerdict(campaign, members, partial)
	state := "final"
	summary := "Automated strategy evaluation completed with preserved immutable evidence."
	if partial {
		state, verdict, reason = "partial", evaluation.VerdictBlocked, campaign.Reason
		if reason == "" {
			reason = evaluation.ReasonPersistenceFailed
		}
		summary = "Evaluation stopped early; completed work and the exact terminal reason are preserved."
	}
	payload := map[string]any{"schema_version": "axiom.evaluation-report.v1", "campaign_id": campaign.ID,
		"preset": campaign.Preset, "state": state, "verdict": verdict, "reason_code": reason,
		"summary": summary, "completed_stages": campaign.CompletedStages,
		"valid_recording_seconds": int64(campaign.ValidRecording / time.Second),
		"valid_shadow_seconds":    int64(campaign.ValidShadow / time.Second), "stages": stages,
		"members": members, "focused_stress": stress, "next_action": evaluationReportNextAction(reason, partial),
		"simulation_only": true, "spot_only": true, "external_execution_available": false}
	for key, value := range operational {
		payload[key] = value
	}
	return evaluation.NewReport(state, verdict, reason, summary, payload, driver.clock.Now().UTC)
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportStages(ctx context.Context,
	campaignID string) ([]evaluationReportStage, error) {
	rows, err := driver.pool.Query(ctx, `SELECT stage,state,COALESCE(reason_code,''),attempt,
  COALESCE(encode(checkpoint_hash,'hex'),'') FROM evaluation_campaign_stages
WHERE campaign_id=$1 ORDER BY ordinal`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stages := make([]evaluationReportStage, 0, len(evaluation.Stages()))
	for rows.Next() {
		var item evaluationReportStage
		if err = rows.Scan(&item.Stage, &item.State, &item.Reason, &item.Attempt, &item.CheckpointHash); err != nil {
			return nil, err
		}
		stages = append(stages, item)
	}
	return stages, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportStress(ctx context.Context,
	campaignID string) ([]evaluationReportStress, error) {
	rows, err := driver.pool.Query(ctx, `SELECT scenario,state,COALESCE(reason_code,''),
  encode(evidence_hash,'hex'),canonical_payload FROM evaluation_campaign_stress_results
WHERE campaign_id=$1 ORDER BY ordinal`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stress := make([]evaluationReportStress, 0, 8)
	for rows.Next() {
		var item evaluationReportStress
		var payload []byte
		if err = rows.Scan(&item.Scenario, &item.State, &item.Reason, &item.EvidenceHash, &payload); err != nil {
			return nil, err
		}
		item.Payload = validEvaluationReportJSON(payload)
		stress = append(stress, item)
	}
	return stress, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportMembers(ctx context.Context,
	campaignID string) ([]evaluationReportMember, error) {
	rows, err := driver.pool.Query(ctx, `SELECT member.id,member.strategy_id,member.configuration_key,
  member.mode,member.state,COALESCE(member.verdict,''),COALESCE(member.reason_code,''),
  COALESCE(member.linked_run_id,''),COALESCE(encode(member.result_hash,'hex'),''),
  member.capital_micros,member.cost_stress_bps,member.repeat_ordinal,
  COALESCE(member.metrics_payload,'{}'::bytea),COALESCE(manifest.canonical_payload,'{}'::bytea)
FROM evaluation_campaign_members member LEFT JOIN run_manifests manifest ON manifest.run_id=member.linked_run_id
WHERE member.campaign_id=$1 ORDER BY member.strategy_id,member.configuration_key,member.mode,
  member.capital_micros,member.repeat_ordinal,member.cost_stress_bps`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]evaluationReportMember, 0, len(evaluation.BalancedFullRuns()))
	for rows.Next() {
		var item evaluationReportMember
		var metrics, manifest []byte
		if err = rows.Scan(&item.ID, &item.Strategy, &item.Configuration, &item.Mode, &item.State,
			&item.Verdict, &item.Reason, &item.RunID, &item.ResultHash, &item.CapitalMicros,
			&item.CostStressBPS, &item.RepeatOrdinal, &metrics, &manifest); err != nil {
			return nil, err
		}
		item.Metrics, item.RunManifest = validEvaluationReportJSON(metrics), validEvaluationReportJSON(manifest)
		members = append(members, item)
	}
	return members, rows.Err()
}

func validEvaluationReportJSON(payload []byte) json.RawMessage {
	if json.Valid(payload) {
		return append(json.RawMessage(nil), payload...)
	}
	return json.RawMessage(`{}`)
}

func reportVerdict(campaign evaluation.Campaign, members []evaluationReportMember,
	partial bool) (evaluation.Verdict, evaluation.ReasonCode) {
	if partial {
		return evaluation.VerdictBlocked, campaign.Reason
	}
	counts := map[evaluation.Verdict]int{}
	for _, member := range members {
		if member.Verdict != "" {
			counts[evaluation.Verdict(member.Verdict)]++
		}
	}
	if counts[evaluation.VerdictBlocked] > 0 {
		return evaluation.VerdictBlocked, evaluation.ReasonSafetyFailed
	}
	if counts[evaluation.VerdictContinue] > 0 {
		return evaluation.VerdictContinue, ""
	}
	if counts[evaluation.VerdictImprove] > 0 {
		return evaluation.VerdictImprove, ""
	}
	return evaluation.VerdictReject, ""
}

func evaluationReportNextAction(reason evaluation.ReasonCode, partial bool) string {
	if !partial {
		return "Review the per-strategy verdicts; promotion remains a separate owner-controlled gate."
	}
	switch reason {
	case evaluation.ReasonStorageInsufficient:
		return "Provide additional storage or reduce the reviewed universe before starting a new campaign."
	case evaluation.ReasonDataUnavailable:
		return "Restore both public feeds and start a new campaign after health is stable."
	case evaluation.ReasonDataCorrupt:
		return "Inspect the preserved audit and recorder evidence; do not repair it in place."
	case evaluation.ReasonCanceled:
		return "No automatic work remains; start a new campaign if evaluation should resume."
	default:
		return "Inspect the terminal stage evidence and correct the shared failure before starting a new campaign."
	}
}

var _ evaluation.Driver = (*EvaluationCampaignDriver)(nil)
