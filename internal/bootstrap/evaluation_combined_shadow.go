package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/recorder"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type evaluationCombinedShadowEngine struct {
	pool        *pgxpool.Pool
	root        string
	clock       domain.Clock
	materialize postgresstore.OfflineJobMaterializer
	control     *postgresstore.EvaluationRecorderControlStore
	mu          sync.Mutex
	runtimes    map[string]*evaluationCombinedShadowRuntime
}

type evaluationCombinedShadowRuntime struct {
	startOrdinal uint64
	lastOrdinal  uint64
	members      []*evaluationCombinedShadowMember
	liquidity    *evaluationMarketProcessor
	seenFills    map[string]string
}

type evaluationCombinedShadowMember struct {
	id        string
	strategy  evaluation.Strategy
	processor backtest.Processor
	failed    bool
}

type evaluationCompletedShadowMember struct {
	id, strategy, verdict, reason string
}

type evaluationShadowSessionProjection struct {
	state                     string
	sessionID                 string
	startOrdinal, lastOrdinal uint64
	validSeconds              int64
	reason                    string
	startedAt, updatedAt      time.Time
}

type evaluationShadowManifests struct {
	binance, bybit recorder.DatasetManifest
	binanceRoot    string
	bybitRoot      string
	lastOrdinal    uint64
	finalOrdinal   uint64
	hash           [32]byte
}

func newEvaluationCombinedShadowEngine(pool *pgxpool.Pool, root string, clock domain.Clock,
	materialize postgresstore.OfflineJobMaterializer) (*evaluationCombinedShadowEngine, error) {
	clean := filepath.Clean(root)
	if pool == nil || !filepath.IsAbs(clean) || clean == string(filepath.Separator) ||
		clock == nil || materialize == nil {
		return nil, fmt.Errorf("evaluation_shadow_dependencies_missing")
	}
	control, err := postgresstore.NewEvaluationRecorderControlStore(pool, clock)
	if err != nil {
		return nil, err
	}
	return &evaluationCombinedShadowEngine{pool: pool, root: clean, clock: clock,
		materialize: materialize, control: control,
		runtimes: make(map[string]*evaluationCombinedShadowRuntime)}, nil
}

// Advance owns one simulation-only, multi-member campaign. The recorder is
// the sole live input boundary; processors have no exchange clients or order
// submission capability.
func (engine *evaluationCombinedShadowEngine) Advance(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if campaign.ID == "" || campaign.CurrentStage != evaluation.StageCombinedShadow {
		return evaluation.StageProgress{}, fmt.Errorf("evaluation_shadow_campaign_invalid")
	}
	session, found, err := engine.readSession(ctx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if !found {
		session, err = engine.initializeSession(ctx, campaign)
		if err != nil {
			return evaluation.StageProgress{}, err
		}
	}
	if progress, terminal := terminalShadowSessionProgress(campaign.ID, session); terminal {
		return progress, nil
	}
	rotation, progress, terminal, err := engine.prepareShadowRotation(ctx, campaign, session)
	if terminal || err != nil {
		return progress, err
	}
	session, _, err = engine.readSession(ctx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if progress, waiting, healthErr := engine.shadowValidTimeProgress(ctx, campaign.ID, session); waiting || healthErr != nil {
		return progress, healthErr
	}
	return engine.finalizeCombinedShadow(ctx, campaign, session, rotation)
}

func terminalShadowSessionProgress(campaignID string,
	session evaluationShadowSessionProjection) (evaluation.StageProgress, bool) {
	if session.state == "BLOCKED" {
		return shadowStageProgress(campaignID, session, evaluation.ProgressBlock,
			evaluation.ReasonCode(session.reason), "Combined shadow stopped at a fail-closed shared safety boundary."), true
	}
	if session.state == "COMPLETED" {
		return shadowStageProgress(campaignID, session, evaluation.ProgressComplete, "",
			"Seven valid shadow days completed with final recorded inputs and member evidence."), true
	}
	return evaluation.StageProgress{}, false
}

func shadowStageProgress(campaignID string, session evaluationShadowSessionProjection,
	state evaluation.ProgressState, reason evaluation.ReasonCode, summary string) evaluation.StageProgress {
	return evaluation.StageProgress{State: state, Reason: reason, Summary: summary,
		Checkpoint: shadowSessionCheckpoint(session), LinkedResourceType: "combined_shadow",
		LinkedResourceID: campaignID}
}

func (engine *evaluationCombinedShadowEngine) prepareShadowRotation(ctx context.Context,
	campaign evaluation.Campaign, session evaluationShadowSessionProjection) (
	postgresstore.EvaluationRecorderRotation, evaluation.StageProgress, bool, error) {
	memberCount, err := engine.shadowMemberCount(ctx, campaign.ID)
	if err != nil {
		return postgresstore.EvaluationRecorderRotation{}, evaluation.StageProgress{}, false, err
	}
	if memberCount == 0 {
		progress, completeErr := engine.completeEmptyShadow(ctx, campaign, session)
		return postgresstore.EvaluationRecorderRotation{}, progress, true, completeErr
	}
	if memberCount > 4 {
		progress, blockErr := engine.blockSession(ctx, campaign.ID, evaluation.ReasonSafetyFailed,
			"Selected strategy count exceeded the fixed combined allocator.")
		return postgresstore.EvaluationRecorderRotation{}, progress, true, blockErr
	}
	rotation, err := engine.control.Rotation(ctx, campaign.ID)
	if err != nil {
		return rotation, evaluation.StageProgress{}, false, err
	}
	if rotation.State == "BLOCKED" {
		progress, blockErr := engine.blockSession(ctx, campaign.ID, evaluation.ReasonStorageInsufficient,
			"Campaign recording stopped before shadow evidence could be completed.")
		return rotation, progress, true, blockErr
	}
	if rotation.State == "PAUSED" {
		if err = engine.control.ResumeCampaign(ctx, campaign.ID); err != nil {
			return rotation, evaluation.StageProgress{}, false, err
		}
		rotation.State = "ACTIVE"
	}
	manifests, available, err := engine.latestManifests(rotation.DesiredSessionID)
	if err != nil {
		progress, blockErr := engine.blockSession(ctx, campaign.ID, evaluation.ReasonDataCorrupt,
			"A campaign recorder manifest failed immutable verification.")
		return rotation, progress, true, blockErr
	}
	if available {
		if processErr := engine.processAvailable(ctx, campaign, session, manifests); processErr != nil {
			if !sharedShadowFailure(processErr) {
				return rotation, evaluation.StageProgress{}, false, processErr
			}
			progress, blockErr := engine.blockSession(ctx, campaign.ID, evaluation.ReasonSafetyFailed,
				"Combined shadow input, accounting, or allocator evidence failed closed.")
			return rotation, progress, true, blockErr
		}
	}
	return rotation, evaluation.StageProgress{}, false, nil
}

func (engine *evaluationCombinedShadowEngine) shadowValidTimeProgress(ctx context.Context, campaignID string,
	session evaluationShadowSessionProjection) (evaluation.StageProgress, bool, error) {
	validSeconds, latestHealthy, latestAt, err := engine.shadowHealth(ctx, campaignID)
	if err != nil {
		return evaluation.StageProgress{}, false, err
	}
	if validSeconds >= int64(evaluation.RequiredShadowValidTime/time.Second) {
		return evaluation.StageProgress{}, false, nil
	}
	if !latestHealthy || latestAt.IsZero() || engine.clock.Now().UTC.Sub(latestAt) > 2*time.Minute {
		return shadowStageProgress(campaignID, session, evaluation.ProgressPause, evaluation.ReasonDataUnavailable,
			"Shadow valid-time is paused until all recorded public feeds and persistence recover."), true, nil
	}
	return shadowStageProgress(campaignID, session, evaluation.ProgressWaiting, "",
		"Selected strategies are running together on recorded public feeds with simulated orders only."), true, nil
}

func (engine *evaluationCombinedShadowEngine) finalizeCombinedShadow(ctx context.Context,
	campaign evaluation.Campaign, session evaluationShadowSessionProjection,
	rotation postgresstore.EvaluationRecorderRotation) (evaluation.StageProgress, error) {
	if rotation.State == "ACTIVE" {
		if err := engine.control.RequestCompletion(ctx, campaign.ID); err != nil {
			return evaluation.StageProgress{}, err
		}
		return shadowStageProgress(campaign.ID, session, evaluation.ProgressWaiting, "",
			"Seven valid days are complete; the recorder is finalizing the last safe evidence boundary."), nil
	}
	if rotation.State == "FINALIZING" {
		return shadowStageProgress(campaign.ID, session, evaluation.ProgressWaiting, "",
			"Seven valid days are complete; final Binance and Bybit manifests are being committed."), nil
	}
	if rotation.State != "COMPLETED" {
		return evaluation.StageProgress{}, fmt.Errorf("evaluation_shadow_recorder_state_invalid")
	}
	manifests, available, err := engine.latestManifests(rotation.DesiredSessionID)
	if err != nil || !available {
		return engine.blockSession(ctx, campaign.ID, evaluation.ReasonDataCorrupt,
			"Final combined-shadow recorder manifests are unavailable or invalid.")
	}
	manifests.lastOrdinal = manifests.finalOrdinal
	if err = engine.processAvailable(ctx, campaign, session, manifests); err != nil {
		return engine.blockSession(ctx, campaign.ID, evaluation.ReasonSafetyFailed,
			"Final combined-shadow processing failed a shared invariant.")
	}
	if err = engine.completeSession(ctx, campaign.ID); err != nil {
		return evaluation.StageProgress{}, err
	}
	delete(engine.runtimes, campaign.ID)
	session, _, err = engine.readSession(ctx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	return shadowStageProgress(campaign.ID, session, evaluation.ProgressComplete, "",
		"Seven valid shadow days completed with final recorded inputs and member evidence."), nil
}

func (engine *evaluationCombinedShadowEngine) initializeSession(ctx context.Context,
	campaign evaluation.Campaign) (evaluationShadowSessionProjection, error) {
	rotation, start, err := engine.shadowSessionStart(ctx, campaign.ID)
	if err != nil {
		return evaluationShadowSessionProjection{}, err
	}
	if err = engine.insertShadowSession(ctx, campaign.ID, rotation.DesiredSessionID, start); err != nil {
		return evaluationShadowSessionProjection{}, err
	}
	if rotation.State == "PAUSED" || rotation.State == "ACTIVE" {
		if err = engine.control.ResumeCampaign(ctx, campaign.ID); err != nil {
			return evaluationShadowSessionProjection{}, err
		}
	}
	session, _, err := engine.readSession(ctx, campaign.ID)
	return session, err
}

func (engine *evaluationCombinedShadowEngine) shadowSessionStart(ctx context.Context,
	campaignID string) (postgresstore.EvaluationRecorderRotation, uint64, error) {
	var combinedConfigurationID string
	if err := engine.pool.QueryRow(ctx, `SELECT combined_configuration_id FROM evaluation_campaigns
WHERE id=$1`, campaignID).Scan(&combinedConfigurationID); err != nil || combinedConfigurationID == "" {
		return postgresstore.EvaluationRecorderRotation{}, 0, fmt.Errorf("evaluation_shadow_combined_configuration_missing")
	}
	rotation, err := engine.control.Rotation(ctx, campaignID)
	if err != nil {
		return rotation, 0, err
	}
	manifests, available, manifestErr := engine.latestManifests(rotation.DesiredSessionID)
	if manifestErr != nil {
		return rotation, 0, manifestErr
	}
	var start uint64
	if available {
		start = manifests.lastOrdinal
	}
	return rotation, start, nil
}
