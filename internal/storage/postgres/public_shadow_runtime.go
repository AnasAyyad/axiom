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

const publicShadowLease = 30 * time.Second

// PublicShadowClaim is one exclusively owned production-public simulation session.
type PublicShadowClaim struct {
	ID                  string
	RunID               string
	AccountID           string
	VenueAccountIDs     map[string]string
	ClaimEpoch          int64
	PortfolioID         string
	ConfigurationID     string
	ConfigurationHash   string
	StrategyID          string
	StrategyVersion     string
	ExchangeID          string
	InstrumentID        string
	MarketScopeRequired bool
	MarketScopes        []PublicShadowMarketScope
	Configuration       config.Configuration
	Models              backtest.ModelNamespace
	SlippageModelID     string
	GapModelID          string
	Recovery            bool
	RecoveryCheckpoint  PublicShadowCheckpoint
}

// PublicShadowMarketScope is one immutable production-public book selected for a
// shadow session. Multi-market strategies consume the ordered complete set.
type PublicShadowMarketScope struct {
	Ordinal      int16
	ExchangeID   string
	InstrumentID string
	Purpose      string
}

// PublicShadowPosture is the authoritative durable control state for one session.
type PublicShadowPosture struct {
	State           string
	RiskState       string
	StoragePressure string
}

// PublicShadowCheckpoint is the canonical in-process state captured after entry
// disablement and evidence flush during a graceful stop.
type PublicShadowCheckpoint struct {
	InputOrdinal      uint64
	CursorLogicalTime uint64
	Canonical         json.RawMessage
}

// PublicShadowStore owns durable engine-shadow claim and lifecycle transitions.
type PublicShadowStore struct {
	pool  *pgxpool.Pool
	owner string
	clock domain.Clock
}

// NewPublicShadowStore constructs the engine-shadow storage boundary.
func NewPublicShadowStore(pool *pgxpool.Pool, owner string, clock domain.Clock) (*PublicShadowStore, error) {
	if pool == nil || owner == "" || clock == nil {
		return nil, fmt.Errorf("owner_console_shadow_store_dependencies_missing")
	}
	return &PublicShadowStore{pool: pool, owner: owner, clock: clock}, nil
}

// Claim pauses and exclusively leases the oldest queued shadow session.
func (store *PublicShadowStore) Claim(ctx context.Context) (PublicShadowClaim, bool, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PublicShadowClaim{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = expirePublicShadowClaims(ctx, tx, now); err != nil {
		return PublicShadowClaim{}, false, err
	}
	claim, found, err := selectPublicShadowClaim(ctx, tx)
	if err != nil || !found {
		if !found && err == nil {
			err = tx.Commit(ctx)
		}
		return PublicShadowClaim{}, false, err
	}
	claim.MarketScopes, err = loadPublicShadowMarketScopes(ctx, tx, claim.ID)
	if err != nil || !publicShadowMarketScopesConfigured(claim) {
		return PublicShadowClaim{}, false, fmt.Errorf("owner_console_shadow_market_scope_invalid")
	}
	claim.Models, err = resolvePublicShadowModels(ctx, tx, claim.Configuration)
	if err != nil {
		return PublicShadowClaim{}, false, err
	}
	if claim.SlippageModelID, err = resolvePublicShadowModel(ctx, tx, "slippage"); err != nil {
		return PublicShadowClaim{}, false, err
	}
	if claim.GapModelID, err = resolvePublicShadowModel(ctx, tx, "gap"); err != nil {
		return PublicShadowClaim{}, false, err
	}
	if claim.Recovery {
		claim.RecoveryCheckpoint, err = loadPublicShadowCheckpoint(ctx, tx, claim.RunID)
		if err != nil {
			return PublicShadowClaim{}, false, err
		}
	}
	if err = store.startPublicShadowClaim(ctx, tx, &claim, now); err != nil {
		return PublicShadowClaim{}, false, err
	}
	return claim, true, tx.Commit(ctx)
}

func expirePublicShadowClaims(ctx context.Context, tx pgx.Tx, now time.Time) error {
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

func selectPublicShadowClaim(ctx context.Context, tx pgx.Tx) (PublicShadowClaim, bool, error) {
	var claim PublicShadowClaim
	var canonical []byte
	err := tx.QueryRow(ctx, `SELECT ss.id,coalesce(ss.run_id,''),ss.portfolio_id,ss.configuration_id,ss.strategy_version_id,
      CASE sv.id WHEN 'trend-following-1-0-0' THEN 'trend-following@1.0.0'
                 WHEN 'mean-reversion-1-0-0' THEN 'mean-reversion@1.0.0'
				 WHEN 'triangular-arbitrage-1-0-0' THEN 'triangular-arbitrage@1.0.0'
				 WHEN 'cross-exchange-arbitrage-1-0-0' THEN 'cross-exchange-arbitrage@1.0.0'
                 ELSE sv.version::text END,
	  ss.exchange_id,
	  CASE WHEN instrument.id IS NULL THEN '' ELSE instrument.base_asset || instrument.quote_asset END,
	  ss.market_scope_required,cv.configuration_hash,cv.canonical_payload
	  FROM shadow_sessions ss JOIN configuration_versions cv ON cv.id=ss.configuration_id
	  JOIN strategy_versions sv ON sv.id=ss.strategy_version_id
	  LEFT JOIN instruments instrument ON instrument.id=ss.instrument_id
	      WHERE ss.state='QUEUED' AND EXISTS(
	        SELECT 1 FROM owner_console_storage_pressure_state pressure
	        WHERE pressure.scope_id='market-data' AND pressure.level='NORMAL'
	          AND pressure.source_instance<>'migration-bootstrap'
	          AND pressure.observed_at>=CURRENT_TIMESTAMP-interval '2 minutes'
	      ) ORDER BY ss.created_at,ss.id FOR UPDATE OF ss SKIP LOCKED LIMIT 1`).
		Scan(&claim.ID, &claim.RunID, &claim.PortfolioID, &claim.ConfigurationID, &claim.StrategyID,
			&claim.StrategyVersion, &claim.ExchangeID, &claim.InstrumentID, &claim.MarketScopeRequired,
			&claim.ConfigurationHash, &canonical)
	if err == pgx.ErrNoRows {
		return PublicShadowClaim{}, false, nil
	}
	if err != nil || json.Unmarshal(canonical, &claim.Configuration) != nil || config.Validate(claim.Configuration) != nil ||
		!publicShadowStrategyConfigured(claim) ||
		(claim.ExchangeID != "binance" && claim.ExchangeID != "bybit") ||
		!publicShadowSelectionConfigured(claim.Configuration, claim.ExchangeID, claim.InstrumentID) {
		return PublicShadowClaim{}, false, fmt.Errorf("owner_console_shadow_claim_invalid")
	}
	claim.Recovery = claim.RunID != ""
	return claim, true, nil
}

func loadPublicShadowCheckpoint(ctx context.Context, tx pgx.Tx, runID string) (PublicShadowCheckpoint, error) {
	var checkpoint PublicShadowCheckpoint
	var ordinal, logical int64
	err := tx.QueryRow(ctx, `SELECT input_ordinal,cursor_logical_time,payload FROM run_checkpoints
	  WHERE run_id=$1 ORDER BY revision DESC LIMIT 1`, runID).Scan(&ordinal, &logical, &checkpoint.Canonical)
	if err != nil || ordinal < 0 || logical <= 0 || !json.Valid(checkpoint.Canonical) {
		return PublicShadowCheckpoint{}, fmt.Errorf("owner_console_shadow_recovery_checkpoint_invalid")
	}
	checkpoint.InputOrdinal, checkpoint.CursorLogicalTime = uint64(ordinal), uint64(logical)
	return checkpoint, nil
}

func loadPublicShadowMarketScopes(ctx context.Context, tx pgx.Tx, sessionID string) ([]PublicShadowMarketScope, error) {
	rows, err := tx.Query(ctx, `SELECT scope.ordinal,scope.exchange_id,
	  instrument.base_asset || instrument.quote_asset,scope.purpose
	  FROM shadow_session_market_scopes scope
	  JOIN instruments instrument ON instrument.id=scope.instrument_id
	  WHERE scope.session_id=$1 ORDER BY scope.ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scopes := make([]PublicShadowMarketScope, 0, 3)
	for rows.Next() {
		var scope PublicShadowMarketScope
		if err = rows.Scan(&scope.Ordinal, &scope.ExchangeID, &scope.InstrumentID, &scope.Purpose); err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	return scopes, rows.Err()
}

func publicShadowMarketScopesConfigured(claim PublicShadowClaim) bool {
	if !claim.MarketScopeRequired {
		return len(claim.MarketScopes) == 0 || publicShadowLegacyScopeMatches(claim)
	}
	if claim.StrategyID == "triangular-arbitrage-1-0-0" {
		if len(claim.MarketScopes) != 3 ||
			!publicShadowSelectionConfigured(claim.Configuration, claim.ExchangeID, claim.InstrumentID) {
			return false
		}
		expected := map[string]bool{"BTCUSDT": false, "ETHBTC": false, "ETHUSDT": false}
		for index, scope := range claim.MarketScopes {
			if scope.Ordinal != int16(index+1) || scope.Purpose != "triangle_market" ||
				scope.ExchangeID != claim.ExchangeID {
				return false
			}
			if _, exists := expected[scope.InstrumentID]; !exists || expected[scope.InstrumentID] ||
				!publicShadowSelectionConfigured(claim.Configuration, scope.ExchangeID, scope.InstrumentID) {
				return false
			}
			expected[scope.InstrumentID] = true
		}
		return expected["BTCUSDT"] && expected["ETHBTC"] && expected["ETHUSDT"]
	}
	if claim.StrategyID == "cross-exchange-arbitrage-1-0-0" {
		if claim.ExchangeID != "binance" || len(claim.MarketScopes) != 2 ||
			!publicShadowSelectionConfigured(claim.Configuration, "binance", claim.InstrumentID) ||
			!publicShadowSelectionConfigured(claim.Configuration, "bybit", claim.InstrumentID) {
			return false
		}
		for index, exchange := range []string{"binance", "bybit"} {
			scope := claim.MarketScopes[index]
			if scope.Ordinal != int16(index+1) || scope.ExchangeID != exchange ||
				scope.InstrumentID != claim.InstrumentID || scope.Purpose != "paired_market" {
				return false
			}
		}
		return true
	}
	if len(claim.MarketScopes) != 1 ||
		(claim.StrategyID != "trend-following-1-0-0" && claim.StrategyID != "mean-reversion-1-0-0") {
		return false
	}
	scope := claim.MarketScopes[0]
	return scope.Ordinal == 1 && scope.Purpose == "primary" &&
		scope.ExchangeID == claim.ExchangeID && scope.InstrumentID == claim.InstrumentID &&
		publicShadowSelectionConfigured(claim.Configuration, scope.ExchangeID, scope.InstrumentID)
}

func publicShadowLegacyScopeMatches(claim PublicShadowClaim) bool {
	if len(claim.MarketScopes) != 1 {
		return false
	}
	scope := claim.MarketScopes[0]
	return scope.Ordinal == 1 && scope.Purpose == "primary" &&
		scope.ExchangeID == claim.ExchangeID && scope.InstrumentID == claim.InstrumentID
}

func publicShadowStrategyConfigured(claim PublicShadowClaim) bool {
	switch claim.StrategyID {
	case "trend-following-1-0-0":
		return claim.StrategyVersion == "trend-following@1.0.0" &&
			claim.StrategyVersion == claim.Configuration.Trend.StrategyVersion
	case "mean-reversion-1-0-0":
		return claim.StrategyVersion == "mean-reversion@1.0.0" &&
			claim.StrategyVersion == claim.Configuration.MeanReversion.StrategyVersion
	case "triangular-arbitrage-1-0-0":
		return claim.StrategyVersion == "triangular-arbitrage@1.0.0" &&
			claim.StrategyVersion == claim.Configuration.Triangular.StrategyVersion
	case "cross-exchange-arbitrage-1-0-0":
		return claim.StrategyVersion == "cross-exchange-arbitrage@1.0.0" &&
			claim.StrategyVersion == claim.Configuration.CrossExchange.StrategyVersion
	default:
		return false
	}
}

func publicShadowSelectionConfigured(configuration config.Configuration, exchange, instrumentID string) bool {
	for _, venue := range configuration.PublicExchanges() {
		if venue.ID != exchange {
			continue
		}
		if instrumentID == "" {
			return true
		}
		for _, configured := range venue.Instruments {
			if configured.Product == "spot" && configured.Base+configured.Quote == instrumentID {
				return true
			}
		}
	}
	return false
}

func resolvePublicShadowModels(ctx context.Context, tx pgx.Tx,
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
		return backtest.ModelNamespace{}, fmt.Errorf("owner_console_shadow_model_namespace_ambiguous")
	}
	return models[0], nil
}

func resolvePublicShadowModel(ctx context.Context, tx pgx.Tx, kind string) (string, error) {
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
		return "", fmt.Errorf("owner_console_shadow_%s_model_ambiguous", kind)
	}
	return ids[0], nil
}
