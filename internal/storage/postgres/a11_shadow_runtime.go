package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const a11ShadowLease = 30 * time.Second

// A11ShadowClaim is one exclusively owned production-public simulation session.
type A11ShadowClaim struct {
	ID                string
	RunID             string
	AccountID         string
	ClaimEpoch        int64
	PortfolioID       string
	ConfigurationID   string
	ConfigurationHash string
	StrategyID        string
	StrategyVersion   string
	Configuration     config.Configuration
	Models            backtest.ModelNamespace
	SlippageModelID   string
	GapModelID        string
}

// A11ShadowPosture is the authoritative durable control state for one session.
type A11ShadowPosture struct {
	State     string
	RiskState string
}

// A11ShadowCheckpoint is the canonical in-process state captured after entry
// disablement and evidence flush during a graceful stop.
type A11ShadowCheckpoint struct {
	InputOrdinal      uint64
	CursorLogicalTime uint64
	Canonical         json.RawMessage
}

// A11ShadowStore owns durable engine-shadow claim and lifecycle transitions.
type A11ShadowStore struct {
	pool  *pgxpool.Pool
	owner string
	clock domain.Clock
}

// NewA11ShadowStore constructs the engine-shadow storage boundary.
func NewA11ShadowStore(pool *pgxpool.Pool, owner string, clock domain.Clock) (*A11ShadowStore, error) {
	if pool == nil || owner == "" || clock == nil {
		return nil, fmt.Errorf("a11_shadow_store_dependencies_missing")
	}
	return &A11ShadowStore{pool: pool, owner: owner, clock: clock}, nil
}

// Claim pauses and exclusively leases the oldest queued shadow session.
func (store *A11ShadowStore) Claim(ctx context.Context) (A11ShadowClaim, bool, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return A11ShadowClaim{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = expireA11ShadowClaims(ctx, tx, now); err != nil {
		return A11ShadowClaim{}, false, err
	}
	claim, found, err := selectA11ShadowClaim(ctx, tx)
	if err != nil || !found {
		if !found && err == nil {
			err = tx.Commit(ctx)
		}
		return A11ShadowClaim{}, false, err
	}
	claim.Models, err = resolveA11ShadowModels(ctx, tx, claim.Configuration)
	if err != nil {
		return A11ShadowClaim{}, false, err
	}
	if claim.SlippageModelID, err = resolveA11ShadowModel(ctx, tx, "slippage"); err != nil {
		return A11ShadowClaim{}, false, err
	}
	if claim.GapModelID, err = resolveA11ShadowModel(ctx, tx, "gap"); err != nil {
		return A11ShadowClaim{}, false, err
	}
	if err = store.startA11ShadowClaim(ctx, tx, &claim, now); err != nil {
		return A11ShadowClaim{}, false, err
	}
	return claim, true, tx.Commit(ctx)
}

func expireA11ShadowClaims(ctx context.Context, tx pgx.Tx, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE shadow_sessions SET state='FAILED',revision=revision+1,entries_enabled=false,
      failure_code='shadow_lease_expired',stopped_at=$1,claim_owner=NULL,claim_epoch=NULL,claim_expires_at=NULL
      WHERE state IN ('PAUSED','RUNNING','CANCEL_REQUESTED') AND claim_expires_at<=$1`, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE runs run SET state='failed',completed_at=$1 FROM shadow_sessions session
      WHERE session.run_id=run.id AND session.state='FAILED' AND session.failure_code='shadow_lease_expired'
	  AND run.state='running'`, now)
	return err
}

func selectA11ShadowClaim(ctx context.Context, tx pgx.Tx) (A11ShadowClaim, bool, error) {
	var claim A11ShadowClaim
	var canonical []byte
	err := tx.QueryRow(ctx, `SELECT ss.id,ss.portfolio_id,ss.configuration_id,ss.strategy_version_id,
      CASE WHEN sv.id='trend-v1a-1' THEN 'trend.v1a.1' ELSE sv.version::text END,
      cv.configuration_hash,cv.canonical_payload
      FROM shadow_sessions ss JOIN configuration_versions cv ON cv.id=ss.configuration_id
      JOIN strategy_versions sv ON sv.id=ss.strategy_version_id
      WHERE ss.state='QUEUED' ORDER BY ss.created_at,ss.id FOR UPDATE OF ss SKIP LOCKED LIMIT 1`).
		Scan(&claim.ID, &claim.PortfolioID, &claim.ConfigurationID, &claim.StrategyID,
			&claim.StrategyVersion, &claim.ConfigurationHash, &canonical)
	if err == pgx.ErrNoRows {
		return A11ShadowClaim{}, false, nil
	}
	if err != nil || json.Unmarshal(canonical, &claim.Configuration) != nil || config.Validate(claim.Configuration) != nil ||
		claim.StrategyVersion != claim.Configuration.Trend.StrategyVersion {
		return A11ShadowClaim{}, false, fmt.Errorf("a11_shadow_claim_invalid")
	}
	return claim, true, nil
}

func resolveA11ShadowModels(ctx context.Context, tx pgx.Tx,
	configuration config.Configuration) (backtest.ModelNamespace, error) {
	rows, err := tx.Query(ctx, `SELECT id,market_context,liquidity_domain,fee_model_id,latency_model_id,fill_model_id
      FROM model_namespaces WHERE market_context='production-public' AND fee_model_id=$1 AND latency_model_id=$2
	  ORDER BY id LIMIT 2`, configuration.Models.Fee, configuration.Models.Latency)
	if err != nil {
		return backtest.ModelNamespace{}, err
	}
	models := make([]backtest.ModelNamespace, 0, 2)
	for rows.Next() {
		var item backtest.ModelNamespace
		if err = rows.Scan(&item.ID, &item.MarketContext, &item.LiquidityDomain, &item.FeeDomain,
			&item.LatencyDomain, &item.FillDomain); err != nil {
			return backtest.ModelNamespace{}, err
		}
		models = append(models, item)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil || len(models) != 1 {
		return backtest.ModelNamespace{}, fmt.Errorf("a11_shadow_model_namespace_ambiguous")
	}
	return models[0], nil
}

func resolveA11ShadowModel(ctx context.Context, tx pgx.Tx, kind string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM model_versions WHERE model_type=$1 ORDER BY version DESC,id LIMIT 2`, kind)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		ids = append(ids, id)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil || len(ids) != 1 {
		return "", fmt.Errorf("a11_shadow_%s_model_ambiguous", kind)
	}
	return ids[0], nil
}

func (store *A11ShadowStore) startA11ShadowClaim(ctx context.Context, tx pgx.Tx,
	claim *A11ShadowClaim, now time.Time) error {
	err := tx.QueryRow(ctx, `UPDATE shadow_sessions SET state='PAUSED',revision=revision+1,entries_enabled=false,
      claim_owner=$1,claim_epoch=coalesce(claim_epoch,0)+1,claim_expires_at=$2,started_at=coalesce(started_at,$3)
	  WHERE id=$4 AND state='QUEUED' RETURNING claim_epoch`, store.owner, now.Add(a11ShadowLease), now, claim.ID).
		Scan(&claim.ClaimEpoch)
	if err != nil || claim.ClaimEpoch <= 0 {
		return fmt.Errorf("a11_shadow_claim_conflict")
	}
	claim.RunID, claim.AccountID = claim.ID, "shadow-account-"+claim.ID
	seed := a11SHA256([]byte("shadow-seed:" + claim.ID))
	if _, err = tx.Exec(ctx, `INSERT INTO runs(id,mode,configuration_id,strategy_version_id,root_seed_hash,
      reproducibility_hash,state,created_at) VALUES($1,'shadow',$2,$3,$4,$5,'created',$6)`,
		claim.RunID, claim.ConfigurationID, claim.StrategyID, seed, claim.ConfigurationHash, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE runs SET state='running',started_at=$2 WHERE id=$1`, claim.RunID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO virtual_accounts(id,portfolio_id,run_id,name,created_at)
      VALUES($1,$2,$3,'main',$4)`, claim.AccountID, claim.PortfolioID, claim.RunID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO virtual_balances(account_id,asset_symbol,available,reserved,revision,updated_at)
      VALUES($1,'USDT',$2,0,1,$3),($1,'BTC',0,0,1,$3),($1,'ETH',0,0,1,$3)`,
		claim.AccountID, claim.Configuration.Portfolio.StartingCapital.Value, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shadow_sessions SET run_id=$1 WHERE id=$1`, claim.RunID); err != nil {
		return err
	}
	return nil
}

// Renew extends only this engine's still-active session lease.
func (store *A11ShadowStore) Renew(ctx context.Context, id string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE shadow_sessions SET claim_expires_at=$1 WHERE id=$2
      AND state IN ('PAUSED','RUNNING','CANCEL_REQUESTED') AND claim_owner=$3 AND claim_expires_at>$4`,
		now.Add(a11ShadowLease), id, store.owner, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_lease_lost")
	}
	return nil
}

// Posture returns the durable session command and global risk posture.
func (store *A11ShadowStore) Posture(ctx context.Context, id string) (A11ShadowPosture, error) {
	var posture A11ShadowPosture
	err := store.pool.QueryRow(ctx, `SELECT ss.state,coalesce(
      (SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),'PAUSED')
      FROM shadow_sessions ss WHERE ss.id=$1 AND ss.claim_owner=$2`, id, store.owner).
		Scan(&posture.State, &posture.RiskState)
	if err != nil {
		return A11ShadowPosture{}, fmt.Errorf("a11_shadow_posture_unavailable")
	}
	return posture, nil
}

// Activate enables entries only while durable global risk is NORMAL.
func (store *A11ShadowStore) Activate(ctx context.Context, id string) error {
	return store.transition(ctx, id, "PAUSED", "RUNNING", true, "")
}

// Pause stops new entries while retaining the active public-data session.
func (store *A11ShadowStore) Pause(ctx context.Context, id string) error {
	return store.transition(ctx, id, "RUNNING", "PAUSED", false, "")
}

// LinkDecisionDataset records the newest qualified cumulative decision-input
// dataset while the session lease is still held.
func (store *A11ShadowStore) LinkDecisionDataset(ctx context.Context, id, datasetID string) error {
	if id == "" || datasetID == "" {
		return fmt.Errorf("a11_shadow_dataset_invalid")
	}
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE shadow_sessions session SET decision_dataset_id=$1
      FROM dataset_manifests dataset WHERE session.id=$2 AND session.claim_owner=$3
      AND session.claim_expires_at>$4 AND session.state IN ('PAUSED','RUNNING','CANCEL_REQUESTED')
      AND dataset.id=$1 AND dataset.state='qualified' AND dataset.dataset_kind='decision_inputs'`,
		datasetID, id, store.owner, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_dataset_link_failed")
	}
	return nil
}

// Checkpoint atomically appends a run checkpoint and an account snapshot while
// the canceled runtime is still fenced by its live claim.
func (store *A11ShadowStore) Checkpoint(ctx context.Context, claim A11ShadowClaim,
	checkpoint A11ShadowCheckpoint) error {
	if checkpoint.CursorLogicalTime == 0 || len(checkpoint.Canonical) == 0 ||
		!json.Valid(checkpoint.Canonical) || checkpoint.InputOrdinal > uint64(^uint64(0)>>1) ||
		checkpoint.CursorLogicalTime > uint64(^uint64(0)>>1) {
		return fmt.Errorf("a11_shadow_checkpoint_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyA11ShadowCheckpointLease(ctx, tx, store.owner, claim.ID, now); err != nil {
		return err
	}
	hash := a11SHA256(checkpoint.Canonical)
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

func verifyA11ShadowCheckpointLease(ctx context.Context, tx pgx.Tx, owner, id string, now time.Time) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT state='CANCEL_REQUESTED' AND claim_expires_at>$3
      FROM shadow_sessions WHERE id=$1 AND claim_owner=$2 FOR UPDATE`, id, owner, now).Scan(&active); err != nil || !active {
		return fmt.Errorf("a11_shadow_checkpoint_lease_lost")
	}
	return nil
}

// CompleteStop terminates a requested session after evidence is flushed.
func (store *A11ShadowStore) CompleteStop(ctx context.Context, id string) error {
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
		return fmt.Errorf("a11_shadow_transition_conflict")
	}
	var checkpointCount, snapshotCount int
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM run_checkpoints WHERE run_id=$1),
      (SELECT count(*) FROM account_snapshots snapshot JOIN virtual_accounts account ON account.id=snapshot.account_id
       WHERE account.run_id=$1)`, id).Scan(&checkpointCount, &snapshotCount); err != nil ||
		checkpointCount == 0 || snapshotCount == 0 {
		return fmt.Errorf("a11_shadow_stop_evidence_missing")
	}
	runTag, err := tx.Exec(ctx, `UPDATE runs SET state='completed',completed_at=$2 WHERE id=$1 AND state='running'`, id, now)
	if err != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_run_completion_conflict")
	}
	return tx.Commit(ctx)
}

// Fail terminates a leased session with one safe stable failure code.
func (store *A11ShadowStore) Fail(ctx context.Context, id, reason string) error {
	if !a11FailureCode.MatchString(reason) {
		return fmt.Errorf("a11_shadow_failure_invalid")
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
		return fmt.Errorf("a11_shadow_failure_conflict")
	}
	runTag, err := tx.Exec(ctx, `UPDATE runs SET state='failed',completed_at=$2 WHERE id=$1 AND state='running'`, id, now)
	if err != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_run_failure_conflict")
	}
	return tx.Commit(ctx)
}

func (store *A11ShadowStore) transition(ctx context.Context, id, current, next string, entries bool, failure string) error {
	now := store.clock.Now().UTC
	riskClause := ""
	if next == "RUNNING" {
		riskClause = ` AND coalesce((SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),'PAUSED')='NORMAL'`
	}
	query := `UPDATE shadow_sessions SET state=$1,revision=revision+1,entries_enabled=$2,
      failure_code=$3,stopped_at=CASE WHEN $1='CANCELED' THEN $4 ELSE stopped_at END,
      claim_owner=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_owner END,
      claim_epoch=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_epoch END,
      claim_expires_at=CASE WHEN $1='CANCELED' THEN NULL ELSE claim_expires_at END
      WHERE id=$5 AND state=$6 AND claim_owner=$7` + riskClause
	tag, err := store.pool.Exec(ctx, query, next, entries, failure, now, id, current, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_shadow_transition_conflict")
	}
	return nil
}
