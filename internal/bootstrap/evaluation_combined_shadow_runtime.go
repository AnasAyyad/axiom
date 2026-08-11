package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/recorder"
	"axiom/internal/replay"

	"github.com/jackc/pgx/v5"
)

func (engine *evaluationCombinedShadowEngine) insertShadowSession(ctx context.Context, campaignID,
	sessionID string, start uint64) error {
	now := engine.clock.Now().UTC
	tx, err := engine.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO evaluation_shadow_sessions(campaign_id,recorder_session_id,state,
start_ordinal,last_processed_ordinal,started_at,updated_at) VALUES($1,$2,'RUNNING',$3,$3,$4,$4)
ON CONFLICT (campaign_id) DO NOTHING`, campaignID, sessionID, int64(start), now)
	// The insert is idempotent; immutable session identity is enforced by the table constraints.

	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,strategy_id FROM evaluation_campaign_members
WHERE campaign_id=$1 AND mode='shadow' ORDER BY strategy_id,id FOR UPDATE`, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, strategy string
		if err = rows.Scan(&id, &strategy); err != nil {
			return err
		}
		count++
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_shadow_member_checkpoints(campaign_id,member_id,
strategy_id,state,last_processed_ordinal,updated_at) VALUES($1,$2,$3,'RUNNING',$4,$5)
ON CONFLICT (campaign_id,member_id) DO NOTHING`, campaignID, id, strategy, int64(start), now); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count > 4 {
		return fmt.Errorf("evaluation_shadow_member_limit_exceeded")
	}
	if _, err = tx.Exec(ctx, `UPDATE evaluation_campaign_members SET state='RUNNING',updated_at=$2
WHERE campaign_id=$1 AND mode='shadow' AND state='PENDING'`, campaignID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (engine *evaluationCombinedShadowEngine) readSession(ctx context.Context,
	campaignID string) (evaluationShadowSessionProjection, bool, error) {
	var value evaluationShadowSessionProjection
	var start, last int64
	var reason *string
	err := engine.pool.QueryRow(ctx, `SELECT state,recorder_session_id,start_ordinal,last_processed_ordinal,
valid_seconds,reason_code,started_at,updated_at FROM evaluation_shadow_sessions WHERE campaign_id=$1`, campaignID).
		Scan(&value.state, &value.sessionID, &start, &last, &value.validSeconds, &reason,
			&value.startedAt, &value.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluationShadowSessionProjection{}, false, nil
	}
	if err != nil || start < 0 || last < start {
		return evaluationShadowSessionProjection{}, false, err
	}
	value.startOrdinal, value.lastOrdinal = uint64(start), uint64(last)
	if reason != nil {
		value.reason = *reason
	}
	return value, true, nil
}

func (engine *evaluationCombinedShadowEngine) shadowMemberCount(ctx context.Context,
	campaignID string) (int, error) {
	var count int
	err := engine.pool.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND mode='shadow'`, campaignID).Scan(&count)
	return count, err
}

func (engine *evaluationCombinedShadowEngine) latestManifests(session string) (
	evaluationShadowManifests, bool, error) {
	binanceRoot := recorderExchangeRoot(engine.root, "binance", 2)
	bybitRoot := recorderExchangeRoot(engine.root, "bybit", 2)
	binanceManifest, binanceFound, err := recorder.LatestManifest(binanceRoot, session)
	if err != nil {
		return evaluationShadowManifests{}, false, err
	}
	bybitManifest, bybitFound, err := recorder.LatestManifest(bybitRoot, session+"-bybit")
	if err != nil {
		return evaluationShadowManifests{}, false, err
	}
	if !binanceFound || !bybitFound {
		return evaluationShadowManifests{}, false, nil
	}
	if !binanceManifest.Complete || !bybitManifest.Complete || len(binanceManifest.Gaps) != 0 ||
		len(bybitManifest.Gaps) != 0 || binanceManifest.Exchange != "binance" || bybitManifest.Exchange != "bybit" {
		return evaluationShadowManifests{}, false, fmt.Errorf("evaluation_shadow_manifest_ineligible")
	}
	binanceLast, err := recorder.ManifestLastOrdinal(binanceManifest)
	if err != nil {
		return evaluationShadowManifests{}, false, err
	}
	bybitLast, err := recorder.ManifestLastOrdinal(bybitManifest)
	if err != nil {
		return evaluationShadowManifests{}, false, err
	}
	last, final := binanceLast, binanceLast
	if bybitLast < last {
		last = bybitLast
	}
	if bybitLast > final {
		final = bybitLast
	}
	hash := sha256.Sum256([]byte("evaluation-shadow-input-v1:" + binanceManifest.Hash + ":" + bybitManifest.Hash))
	return evaluationShadowManifests{binance: binanceManifest, bybit: bybitManifest,
		binanceRoot: binanceRoot, bybitRoot: bybitRoot, lastOrdinal: last,
		finalOrdinal: final, hash: hash}, true, nil
}

func (engine *evaluationCombinedShadowEngine) processAvailable(ctx context.Context, campaign evaluation.Campaign,
	session evaluationShadowSessionProjection, manifests evaluationShadowManifests) error {
	runtime, err := engine.runtime(ctx, campaign, session)
	if err != nil {
		return err
	}
	if manifests.lastOrdinal < runtime.lastOrdinal {
		return fmt.Errorf("evaluation_shadow_manifest_regressed")
	}
	if manifests.lastOrdinal == runtime.lastOrdinal {
		return engine.persistRuntime(ctx, campaign.ID, runtime, manifests.hash)
	}
	source, err := engine.openShadowSource(manifests, runtime.lastOrdinal+1, manifests.lastOrdinal)
	if err != nil {
		return err
	}
	for {
		event, ok, nextErr := source.Next()
		if nextErr != nil {
			return nextErr
		}
		if !ok {
			break
		}
		if err = engine.processShadowEvent(ctx, campaign.ID, runtime, event); err != nil {
			return err
		}
	}
	if runtime.lastOrdinal < manifests.lastOrdinal {
		return fmt.Errorf("evaluation_shadow_source_incomplete")
	}
	return engine.persistRuntime(ctx, campaign.ID, runtime, manifests.hash)
}

func (engine *evaluationCombinedShadowEngine) processShadowEvent(ctx context.Context, campaignID string,
	runtime *evaluationCombinedShadowRuntime, event replay.Event) error {
	if err := observeCombinedShadowBook(runtime.liquidity, event); err != nil {
		return err
	}
	consumption := make(map[string]domain.Quantity)
	for _, member := range runtime.members {
		if member.failed {
			continue
		}
		if err := engine.processShadowMember(ctx, campaignID, runtime, member, event, consumption); err != nil {
			return err
		}
	}
	if err := validateCombinedLiquidity(runtime.liquidity, consumption); err != nil {
		return err
	}
	runtime.lastOrdinal = event.Ordinal
	return nil
}

func (engine *evaluationCombinedShadowEngine) processShadowMember(ctx context.Context, campaignID string,
	runtime *evaluationCombinedShadowRuntime, member *evaluationCombinedShadowMember, event replay.Event,
	consumption map[string]domain.Quantity) error {
	result, err := member.processor.Process(ctx, event)
	if err != nil {
		if sharedShadowFailure(err) {
			return err
		}
		member.failed = true
		return engine.failMember(ctx, campaignID, member.id, err)
	}
	if err = engine.observeCombinedFills(runtime, member, event, result, consumption); err != nil {
		return err
	}
	if shadowDecisionMaterial(result) {
		return engine.persistDecision(ctx, campaignID, member, event, result)
	}
	return nil
}

func (engine *evaluationCombinedShadowEngine) runtime(ctx context.Context, campaign evaluation.Campaign,
	session evaluationShadowSessionProjection) (*evaluationCombinedShadowRuntime, error) {
	if value := engine.runtimes[campaign.ID]; value != nil {
		return value, nil
	}
	runtime := &evaluationCombinedShadowRuntime{startOrdinal: session.startOrdinal,
		lastOrdinal: session.startOrdinal, liquidity: &evaluationMarketProcessor{books: make(map[string]*evaluationBook)},
		seenFills: make(map[string]string)}
	members, err := engine.loadShadowRuntimeMembers(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}
	runtime.members = members
	if len(runtime.members) == 0 || len(runtime.members) > 4 {
		return nil, fmt.Errorf("evaluation_shadow_members_invalid")
	}
	engine.runtimes[campaign.ID] = runtime
	return runtime, nil
}

func (engine *evaluationCombinedShadowEngine) loadShadowRuntimeMembers(ctx context.Context,
	campaignID string) ([]*evaluationCombinedShadowMember, error) {
	rows, err := engine.pool.Query(ctx, `SELECT shadow.id,shadow.strategy_id,baseline.linked_job_id,
job.job_type,job.request_payload,checkpoint.state
FROM evaluation_campaign_members shadow
JOIN evaluation_campaign_members baseline ON baseline.campaign_id=shadow.campaign_id
  AND baseline.strategy_id=shadow.strategy_id AND baseline.configuration_id=shadow.configuration_id
  AND baseline.mode='replay' AND baseline.capital_micros=2000000000
  AND baseline.repeat_ordinal=0 AND baseline.cost_stress_bps=10000
JOIN jobs job ON job.id=baseline.linked_job_id
JOIN evaluation_shadow_member_checkpoints checkpoint ON checkpoint.campaign_id=shadow.campaign_id
  AND checkpoint.member_id=shadow.id
WHERE shadow.campaign_id=$1 AND shadow.mode='shadow'
	ORDER BY shadow.strategy_id,shadow.id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]*evaluationCombinedShadowMember, 0, 4)
	for rows.Next() {
		var id, strategy, jobID, kind, state string
		var payload []byte
		if err = rows.Scan(&id, &strategy, &jobID, &kind, &payload, &state); err != nil {
			return nil, err
		}
		member, materializeErr := engine.materializeShadowMember(ctx, id, strategy, jobID, kind, state, payload)
		if materializeErr != nil {
			return nil, materializeErr
		}
		members = append(members, member)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func (engine *evaluationCombinedShadowEngine) materializeShadowMember(ctx context.Context,
	id, strategy, jobID, kind, state string, payload []byte) (*evaluationCombinedShadowMember, error) {
	member := &evaluationCombinedShadowMember{id: id, strategy: evaluation.Strategy(strategy),
		failed: state == "FAILED"}
	if member.failed {
		return member, nil
	}
	claim, err := engine.materialize(ctx, jobID, kind, append(json.RawMessage(nil), payload...))
	if err != nil {
		return nil, err
	}
	claim.ID, claim.ResumeOrdinal, claim.SingleStep = id, 0, false
	claim.EvaluationInputKind = "public_market"
	claim.Manifest.Mode = "shadow"
	claim.Manifest.Evaluation.MemberID = id
	member.processor, err = newOfflineOperationalProcessor(claim)
	return member, err
}

func (engine *evaluationCombinedShadowEngine) openShadowSource(manifests evaluationShadowManifests,
	first, last uint64) (replay.Source, error) {
	if first == 0 || last < first {
		return nil, fmt.Errorf("evaluation_shadow_window_invalid")
	}
	open := func(root string, manifest recorder.DatasetManifest) (*backtest.DatasetReader, error) {
		if len(manifest.Segments) < 2 || manifest.Compatibility == nil {
			return nil, fmt.Errorf("evaluation_shadow_compatibility_missing")
		}
		canonical := manifest.Segments[1].Manifest.Spec
		if canonical.ParserVersion == "" || canonical.NormalizationVersion == "" {
			return nil, fmt.Errorf("evaluation_shadow_compatibility_missing")
		}
		path := filepath.Join(root, fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision))
		commit := claimConfigurationCommit()
		if commit == "" {
			return nil, fmt.Errorf("evaluation_shadow_build_identity_missing")
		}
		return backtest.OpenDataset(root, path, backtest.DatasetCompatibility{SourceCommit: commit,
			ParserVersion:         canonical.ParserVersion,
			NormalizationVersion:  canonical.NormalizationVersion,
			MinimumRecordsPerPair: 1, MaximumLowDensityPairs: ^uint64(0)})
	}
	binanceReader, err := open(manifests.binanceRoot, manifests.binance)
	if err != nil {
		return nil, err
	}
	bybitReader, err := open(manifests.bybitRoot, manifests.bybit)
	if err != nil {
		return nil, err
	}
	merged, err := backtest.NewEvaluationMergedSource(binanceReader, bybitReader)
	if err != nil {
		return nil, err
	}
	return backtest.NewEvaluationWindowSource(merged, first, last)
}
