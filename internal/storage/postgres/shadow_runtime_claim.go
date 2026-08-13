package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (store *PublicShadowStore) startPublicShadowClaim(ctx context.Context, tx pgx.Tx,
	claim *PublicShadowClaim, now time.Time) error {
	err := tx.QueryRow(ctx, `UPDATE shadow_sessions SET state='PAUSED',revision=revision+1,entries_enabled=false,
      claim_owner=$1,claim_epoch=coalesce(claim_epoch,0)+1,claim_expires_at=$2,started_at=coalesce(started_at,$3)
	  WHERE id=$4 AND state='QUEUED' RETURNING claim_epoch`, store.owner, now.Add(publicShadowLease), now, claim.ID).
		Scan(&claim.ClaimEpoch)
	if err != nil || claim.ClaimEpoch <= 0 {
		return fmt.Errorf("owner_console_shadow_claim_conflict")
	}
	claim.RunID, claim.AccountID = claim.ID, "shadow-account-"+claim.ID
	if claim.StrategyID == "cross-exchange-arbitrage-1-0-0" {
		claim.VenueAccountIDs = map[string]string{
			"binance": claim.AccountID + "-binance",
			"bybit":   claim.AccountID + "-bybit",
		}
		claim.AccountID = claim.VenueAccountIDs["binance"]
	}
	seed := ownerConsoleSHA256([]byte("shadow-seed:" + claim.ID))
	if _, err = tx.Exec(ctx, `INSERT INTO runs(id,mode,configuration_id,strategy_version_id,root_seed_hash,
      reproducibility_hash,state,created_at) VALUES($1,'shadow',$2,$3,$4,$5,'created',$6)`,
		claim.RunID, claim.ConfigurationID, claim.StrategyID, seed, claim.ConfigurationHash, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE runs SET state='running',started_at=$2 WHERE id=$1`, claim.RunID, now); err != nil {
		return err
	}
	if err = insertPublicShadowAccounts(ctx, tx, *claim, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shadow_sessions SET run_id=$1 WHERE id=$1`, claim.RunID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shadow_sessions SET model_namespace_id=$2,slippage_model_id=$3,gap_model_id=$4
	  WHERE id=$1`, claim.ID, claim.Models.ID, claim.SlippageModelID, claim.GapModelID); err != nil {
		return err
	}
	return nil
}

func insertPublicShadowAccounts(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim, now time.Time) error {
	if claim.StrategyID != "cross-exchange-arbitrage-1-0-0" {
		if _, err := tx.Exec(ctx, `INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at)
          VALUES($1,$2,$3,'main',$4)`, claim.AccountID, claim.PortfolioID, claim.RunID, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
          VALUES($1,'USDT',$2,0,1,$3),($1,'BTC',0,0,1,$3),($1,'ETH',0,0,1,$3)`,
			claim.AccountID, claim.Configuration.Portfolio.StartingCapital.Value, now)
		return err
	}
	capital, capitalErr := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	half, halfErr := domain.ParsePercent("0.5")
	venueCapital, scaleErr := domain.ScaleBalanceFloor(capital, half, 18)
	if capitalErr != nil || halfErr != nil || scaleErr != nil {
		return fmt.Errorf("owner_console_shadow_cross_exchange_capital_invalid")
	}
	for _, exchange := range []string{"binance", "bybit"} {
		accountID := claim.VenueAccountIDs[exchange]
		if _, err := tx.Exec(ctx, `INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at)
          VALUES($1,$2,$3,$4,$5)`, accountID, claim.PortfolioID, claim.RunID, exchange, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
          VALUES($1,'USDT',$2,0,1,$3),($1,'BTC',0,0,1,$3),($1,'ETH',0,0,1,$3)`,
			accountID, venueCapital.String(), now); err != nil {
			return err
		}
	}
	return nil
}

// Renew extends only this engine's still-active session lease.
func (store *PublicShadowStore) Renew(ctx context.Context, id string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE shadow_sessions SET claim_expires_at=$1 WHERE id=$2
      AND state IN ('PAUSED','RUNNING','CANCEL_REQUESTED') AND claim_owner=$3 AND claim_expires_at>$4`,
		now.Add(publicShadowLease), id, store.owner, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_lease_lost")
	}
	return nil
}

// Activate enables entries only while durable global risk is NORMAL.
func (store *PublicShadowStore) Activate(ctx context.Context, id string) error {
	return store.transition(ctx, id, "PAUSED", "RUNNING", true, "")
}

// Pause stops new entries while retaining the active public-data session.
func (store *PublicShadowStore) Pause(ctx context.Context, id string) error {
	return store.transition(ctx, id, "RUNNING", "PAUSED", false, "")
}

// LinkDecisionDataset records the newest qualified cumulative decision-input
// dataset while the session lease is still held.
func (store *PublicShadowStore) LinkDecisionDataset(ctx context.Context, id, datasetID string) error {
	if id == "" || datasetID == "" {
		return fmt.Errorf("owner_console_shadow_dataset_invalid")
	}
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE shadow_sessions session SET decision_dataset_id=$1
      FROM dataset_manifests dataset WHERE session.id=$2 AND session.claim_owner=$3
      AND session.claim_expires_at>$4 AND session.state IN ('PAUSED','RUNNING','CANCEL_REQUESTED')
      AND dataset.id=$1 AND dataset.state='qualified' AND dataset.dataset_kind='decision_inputs'`,
		datasetID, id, store.owner, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_dataset_link_failed")
	}
	return nil
}

// Checkpoint atomically appends a run checkpoint and an account snapshot while
// the canceled runtime is still fenced by its live claim.
func (store *PublicShadowStore) Checkpoint(ctx context.Context, claim PublicShadowClaim,
	checkpoint PublicShadowCheckpoint) error {
	if checkpoint.CursorLogicalTime == 0 || len(checkpoint.Canonical) == 0 ||
		!json.Valid(checkpoint.Canonical) || checkpoint.InputOrdinal > uint64(^uint64(0)>>1) ||
		checkpoint.CursorLogicalTime > uint64(^uint64(0)>>1) {
		return fmt.Errorf("owner_console_shadow_checkpoint_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowCheckpointLease(ctx, tx, store.owner, claim.ID, now); err != nil {
		return err
	}
	hash := ownerConsoleSHA256(checkpoint.Canonical)
	var checkpointRevision, snapshotRevision int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM run_checkpoints WHERE run_id=$1`,
		claim.RunID).Scan(&checkpointRevision); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM account_snapshots WHERE account_id=$1`,
		claim.AccountID).Scan(&snapshotRevision); err != nil {
		return err
	}
	checkpointID := fmt.Sprintf("shadow-checkpoint-%s-%d", claim.ID, checkpointRevision)
	if _, err = tx.Exec(ctx, `INSERT INTO run_checkpoints(id,run_id,revision,input_ordinal,state_hash,payload,
      created_at,cursor_logical_time,projection_hash,model_namespace_id,deterministic_state_hash)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$5,$9,$5)`, checkpointID, claim.RunID, checkpointRevision,
		int64(checkpoint.InputOrdinal), hash, []byte(checkpoint.Canonical), now,
		int64(checkpoint.CursorLogicalTime), claim.Models.ID); err != nil {
		return err
	}
	snapshotID := fmt.Sprintf("shadow-account-snapshot-%s-%d", claim.ID, snapshotRevision)
	if _, err = tx.Exec(ctx, `INSERT INTO account_snapshots(id,account_id,revision,snapshot_hash,
      canonical_payload,recorded_at) VALUES($1,$2,$3,$4,$5,$6)`, snapshotID, claim.AccountID,
		snapshotRevision, hash, []byte(checkpoint.Canonical), now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyPublicShadowCheckpointLease(ctx context.Context, tx pgx.Tx, owner, id string, now time.Time) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT state='CANCEL_REQUESTED' AND claim_expires_at>$3
      FROM shadow_sessions WHERE id=$1 AND claim_owner=$2 FOR UPDATE`, id, owner, now).Scan(&active); err != nil || !active {
		return fmt.Errorf("owner_console_shadow_checkpoint_lease_lost")
	}
	return nil
}

// CompleteStop terminates a requested session after evidence is flushed.
func (store *PublicShadowStore) CompleteStop(ctx context.Context, id string) error {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	shadowTag, err := tx.Exec(ctx, `UPDATE shadow_sessions SET state='CANCELED',revision=revision+1,
      entries_enabled=false,stopped_at=$1,claim_owner=NULL,claim_epoch=NULL,claim_expires_at=NULL
      WHERE id=$2 AND state='CANCEL_REQUESTED' AND claim_owner=$3`, now, id, store.owner)
	if err != nil || shadowTag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_transition_conflict")
	}
	var checkpointCount, snapshotCount int
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM run_checkpoints WHERE run_id=$1),
      (SELECT count(*) FROM account_snapshots snapshot JOIN virtual_accounts account ON account.id=snapshot.account_id
       WHERE account.run_id=$1)`, id).Scan(&checkpointCount, &snapshotCount); err != nil ||
		checkpointCount == 0 || snapshotCount == 0 {
		return fmt.Errorf("owner_console_shadow_stop_evidence_missing")
	}
	runTag, err := tx.Exec(ctx, `UPDATE runs SET state='completed',completed_at=$2 WHERE id=$1 AND state='running'`, id, now)
	if err != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_run_completion_conflict")
	}
	return tx.Commit(ctx)
}

// Fail terminates a leased session with one safe stable failure code.
func (store *PublicShadowStore) Fail(ctx context.Context, id, reason string) error {
	if !ownerConsoleFailureCode.MatchString(reason) {
		return fmt.Errorf("owner_console_shadow_failure_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	shadowTag, err := tx.Exec(ctx, `UPDATE shadow_sessions SET state='FAILED',revision=revision+1,
      entries_enabled=false,failure_code=$1,stopped_at=$2,claim_owner=NULL,claim_epoch=NULL,claim_expires_at=NULL
      WHERE id=$3 AND state IN ('PAUSED','RUNNING','CANCEL_REQUESTED') AND claim_owner=$4`,
		reason, now, id, store.owner)
	if err != nil || shadowTag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_failure_conflict")
	}
	runTag, err := tx.Exec(ctx, `UPDATE runs SET state='failed',completed_at=$2 WHERE id=$1 AND state='running'`, id, now)
	if err != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_run_failure_conflict")
	}
	return tx.Commit(ctx)
}

func (store *PublicShadowStore) transition(ctx context.Context, id, current, next string, entries bool, failure string) error {
	now := store.clock.Now().UTC
	riskClause := ""
	if next == "RUNNING" {
		riskClause = ` AND coalesce((SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),'PAUSED')='NORMAL'
			  AND EXISTS(SELECT 1 FROM owner_console_storage_pressure_state WHERE scope_id='market-data'
			    AND level='NORMAL' AND source_instance<>'migration-bootstrap'
			    AND observed_at>=CURRENT_TIMESTAMP-interval '2 minutes')`
	}
	query := `UPDATE shadow_sessions SET state=$1,revision=revision+1,entries_enabled=$2,
      failure_code=$3,stopped_at=CASE WHEN $1='CANCELED' THEN $4 ELSE stopped_at END,
      claim_owner=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_owner END,
      claim_epoch=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_epoch END,
      claim_expires_at=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_expires_at END
      WHERE id=$5 AND state=$6 AND claim_owner=$7` + riskClause
	tag, err := store.pool.Exec(ctx, query, next, entries, failure, now, id, current, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_transition_conflict")
	}
	return nil
}
