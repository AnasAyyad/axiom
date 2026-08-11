package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

// EvaluationCampaigns returns only safe owner-facing campaign progress.
func (store *OwnerConsoleStore) EvaluationCampaigns(ctx context.Context) (generated.EvaluationCampaignPage, error) {
	rows, err := store.pool.Query(ctx, campaignSelect+" ORDER BY created_at DESC,id DESC LIMIT 100")
	if err != nil {
		return generated.EvaluationCampaignPage{}, err
	}
	defer rows.Close()
	page := generated.EvaluationCampaignPage{Items: make([]generated.EvaluationCampaign, 0)}
	for rows.Next() {
		value, scanErr := scanCampaign(rows)
		if scanErr != nil {
			return generated.EvaluationCampaignPage{}, scanErr
		}
		store.decorateEvaluationCampaign(&value)
		page.Items = append(page.Items, value)
	}
	return page, rows.Err()
}

// EvaluationCampaign reads one opaque campaign identifier.
func (store *OwnerConsoleStore) EvaluationCampaign(ctx context.Context, id string) (generated.EvaluationCampaign, error) {
	value, err := scanCampaign(store.pool.QueryRow(ctx, campaignSelect+" WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.EvaluationCampaign{}, console.ErrNotFound
	}
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	store.decorateEvaluationCampaign(&value)
	if err = store.loadEvaluationCampaignDetail(ctx, &value); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	return value, nil
}

// CreateEvaluationCampaign accepts only the fixed server-owned preset. Worker
// resolution of datasets, portfolios, configuration, and strategy versions is
// intentionally outside the browser request.
func (store *OwnerConsoleStore) CreateEvaluationCampaign(ctx context.Context, principal authentication.Principal, key string, request generated.EvaluationCampaignCreateRequest) (generated.EvaluationCampaign, error) {
	if request.Preset != generated.EvaluationCampaignCreateRequestPresetBalancedFullV1 {
		return generated.EvaluationCampaign{}, console.ErrInvalidRequest
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return generated.EvaluationCampaign{}, console.ErrInvalidRequest
	}
	hash := sha256.Sum256(payload)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('axiom:evaluation_campaign'))"); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	if existing, found, readErr := store.existingEvaluationCampaign(ctx, tx, principal.UserID, key, hash); found || readErr != nil {
		if readErr != nil {
			return generated.EvaluationCampaign{}, readErr
		}
		return existing, tx.Commit(ctx)
	}
	if err = ensureNoActiveEvaluationCampaign(ctx, tx); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	now := store.clock.Now().UTC
	value, err := store.insertEvaluationCampaign(ctx, tx, principal.UserID, key, hash, now)
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	return value, tx.Commit(ctx)
}

func (store *OwnerConsoleStore) insertEvaluationCampaign(ctx context.Context, tx pgx.Tx, actorID,
	key string, hash [sha256.Size]byte, now time.Time) (generated.EvaluationCampaign, error) {
	id, err := ownerConsoleIdentifier("evaluation")
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaigns(id,preset,state,current_stage,revision,created_at,updated_at) VALUES($1,'balanced_full_v1','PENDING',NULL,0,$2,$2)`, id, now); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	if err = insertBalancedEvaluationPlan(ctx, tx, id, now); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_events(campaign_id,ordinal,event_type,summary,occurred_at) VALUES($1,0,'campaign_queued','Full evaluation accepted; server-owned data resolution is pending.', $2)`, id, now); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	response, _ := json.Marshal(map[string]string{"campaign_id": id, "state": "PENDING"})
	commandID, _ := ownerConsoleIdentifier("evaluation-command")
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_commands(id,actor_id,idempotency_key,request_hash,target_id,state,response_payload,created_at) VALUES($1,$2,$3,$4,$5,'accepted',$6,$7)`, commandID, actorID, key, hash[:], id, response, now); err != nil {
		return generated.EvaluationCampaign{}, err
	}
	value, err := scanCampaign(tx.QueryRow(ctx, campaignSelect+" WHERE id=$1", id))
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	store.decorateEvaluationCampaign(&value)
	return value, nil
}

func (store *OwnerConsoleStore) existingEvaluationCampaign(ctx context.Context, tx pgx.Tx,
	actorID, key string, hash [sha256.Size]byte) (generated.EvaluationCampaign, bool, error) {
	var existingID string
	var existingHash []byte
	err := tx.QueryRow(ctx, `SELECT target_id,request_hash FROM evaluation_campaign_commands
WHERE actor_id=$1 AND idempotency_key=$2`, actorID, key).Scan(&existingID, &existingHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.EvaluationCampaign{}, false, nil
	}
	if err != nil {
		return generated.EvaluationCampaign{}, false, err
	}
	if !bytes.Equal(existingHash, hash[:]) {
		return generated.EvaluationCampaign{}, true, console.ErrIdempotencyConflict
	}
	value, err := scanCampaign(tx.QueryRow(ctx, campaignSelect+" WHERE id=$1", existingID))
	if err != nil {
		return generated.EvaluationCampaign{}, true, err
	}
	store.decorateEvaluationCampaign(&value)
	return value, true, nil
}

func ensureNoActiveEvaluationCampaign(ctx context.Context, tx pgx.Tx) error {
	var active string
	err := tx.QueryRow(ctx, `SELECT id FROM evaluation_campaigns
WHERE state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE') LIMIT 1`).Scan(&active)
	if err == nil {
		return console.ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func insertBalancedEvaluationPlan(ctx context.Context, tx pgx.Tx, campaignID string, now time.Time) error {
	if err := evaluation.ValidateBalancedFullDefinition(evaluation.BalancedFullDefinition()); err != nil {
		return err
	}
	for index, stage := range evaluation.Stages() {
		if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_stages(campaign_id,stage,ordinal,state,updated_at)
		  VALUES($1,$2,$3,'PENDING',$4)`, campaignID, string(stage), index+1, now); err != nil {
			return err
		}
	}
	windowStart := time.Date(2023, time.August, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	for _, exchange := range []string{"binance", "bybit"} {
		for _, instrument := range []string{"BTC/USDT", "ETH/USDT"} {
			for _, interval := range []string{"15m", "1h", "4h"} {
				importID, err := ownerConsoleIdentifier("historical-import")
				if err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO evaluation_historical_imports(id,campaign_id,exchange_id,
				  instrument,interval,window_start,window_end,checkpoint_time,session_id,recorder_dataset_id,
				  state,created_at,updated_at)
				  VALUES($1,$2,$3,$4,$5,$6,$7,$6,$8,$9,'PENDING',$10,$10)`, importID, campaignID, exchange,
					instrument, interval, windowStart, windowEnd, evaluation.HistoricalImportSessionID(importID),
					evaluation.HistoricalImportDatasetID(importID), now); err != nil {
					return err
				}
			}
		}
	}
	for _, run := range evaluation.BalancedFullRuns() {
		digest := sha256.Sum256([]byte(run.ID))
		memberID := "evaluation-member-" + fmtHex(digest[:16])
		if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_members(campaign_id,id,strategy_id,
		  configuration_key,mode,capital_micros,repeat_ordinal,cost_stress_bps,state,created_at,updated_at)
		  VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PENDING',$9,$9)`, campaignID, memberID, string(run.Strategy),
			run.ConfigurationKey, run.Mode, run.CapitalMicros, run.RepeatOrdinal, run.CostStressBPS, now); err != nil {
			return err
		}
	}
	return nil
}

// CancelEvaluationCampaign records a terminal owner cancel without removing a
// dataset, run, ledger, or report input.
func (store *OwnerConsoleStore) CancelEvaluationCampaign(ctx context.Context, principal authentication.Principal, id, key string, body generated.RevisionCommandRequest) (generated.CommandAccepted, error) {
	if body.Reason == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	payload, err := json.Marshal(map[string]any{"action": "cancel", "campaign_id": id, "body": body})
	if err != nil {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	hash := sha256.Sum256(payload)
	if existing, found, readErr := existingEvaluationCancel(ctx, tx, principal.UserID, key, hash); found || readErr != nil {
		if readErr != nil {
			return generated.CommandAccepted{}, readErr
		}
		return existing, tx.Commit(ctx)
	}
	revision, currentStage, err := lockCancelableEvaluationCampaign(ctx, tx, id, body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	now := store.clock.Now().UTC
	revision++
	if err = cancelEvaluationCampaignState(ctx, tx, id, currentStage, revision, body.Reason, now); err != nil {
		return generated.CommandAccepted{}, err
	}
	accepted, err := recordEvaluationCancelCommand(ctx, tx, principal.UserID, id, key, hash, revision, now)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	return accepted, tx.Commit(ctx)
}

func existingEvaluationCancel(ctx context.Context, tx pgx.Tx, actorID, key string,
	hash [sha256.Size]byte) (generated.CommandAccepted, bool, error) {
	var existingHash, response []byte
	err := tx.QueryRow(ctx, `SELECT request_hash,response_payload FROM evaluation_campaign_commands
WHERE actor_id=$1 AND idempotency_key=$2`, actorID, key).Scan(&existingHash, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.CommandAccepted{}, false, nil
	}
	if err != nil {
		return generated.CommandAccepted{}, false, err
	}
	if !bytes.Equal(existingHash, hash[:]) {
		return generated.CommandAccepted{}, true, console.ErrIdempotencyConflict
	}
	var existing generated.CommandAccepted
	if json.Unmarshal(response, &existing) != nil {
		return generated.CommandAccepted{}, true, console.ErrUnavailable
	}
	return existing, true, nil
}

func lockCancelableEvaluationCampaign(ctx context.Context, tx pgx.Tx, id string,
	expected generated.Revision) (int64, *string, error) {
	var revision int64
	var state string
	var currentStage *string
	err := tx.QueryRow(ctx, `SELECT state,revision,current_stage FROM evaluation_campaigns
WHERE id=$1 FOR UPDATE`, id).Scan(&state, &revision, &currentStage)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, console.ErrNotFound
	}
	if err != nil {
		return 0, nil, err
	}
	if strconv.FormatInt(revision, 10) != string(expected) {
		return 0, nil, console.ErrConflict
	}
	if state != "PENDING" && state != "RUNNING" && state != "PAUSED_RECOVERABLE" {
		return 0, nil, console.ErrPrecondition
	}
	return revision, currentStage, nil
}

func cancelEvaluationCampaignState(ctx context.Context, tx pgx.Tx, id string,
	currentStage *string, revision int64, reason string, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE evaluation_campaigns SET state='CANCELED',current_stage=NULL,
	  reason_code='CANCELED_BY_OWNER',revision=$2,updated_at=$3,claim_owner=NULL,claim_expires_at=NULL,
	  claim_epoch=claim_epoch+1 WHERE id=$1`, id, revision, now); err != nil {
		return err
	}
	if currentStage != nil {
		if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='CANCELED',
		  reason_code='CANCELED_BY_OWNER',updated_at=$3 WHERE campaign_id=$1 AND stage=$2`, id,
			*currentStage, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='CANCELED',
	  reason_code='CANCELED_BY_OWNER',updated_at=$2 WHERE campaign_id=$1 AND state='PENDING'`, id, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_events(campaign_id,ordinal,event_type,reason_code,summary,occurred_at) VALUES($1,$2,'campaign_canceled','CANCELED_BY_OWNER',$3,$4)`, id, revision, reason, now); err != nil {
		return err
	}
	return nil
}

func recordEvaluationCancelCommand(ctx context.Context, tx pgx.Tx, actorID, id, key string,
	hash [sha256.Size]byte, revision int64, now time.Time) (generated.CommandAccepted, error) {
	commandID, _ := ownerConsoleIdentifier("evaluation-command")
	accepted := generated.CommandAccepted{Id: commandID, TargetId: id, State: generated.CommandAcceptedState("accepted"), Revision: generated.Revision(strconv.FormatInt(revision, 10)), CreatedAt: generated.Timestamp(now), CorrelationId: commandID}
	response, _ := json.Marshal(accepted)
	if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_commands(id,actor_id,idempotency_key,request_hash,
	  target_id,state,response_payload,created_at) VALUES($1,$2,$3,$4,$5,'accepted',$6,$7)`, commandID,
		actorID, key, hash[:], id, response, now); err != nil {
		return generated.CommandAccepted{}, err
	}
	return accepted, nil
}

// EvaluationCampaignEvents returns the append-only campaign timeline.
