package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"axiom/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type a11ReferenceExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// EnsureV1AReferenceData idempotently installs the immutable credential-free
// product graph needed by recorder, offline workers, and the A11 console.
func EnsureV1AReferenceData(ctx context.Context, pool *pgxpool.Pool,
	configuration config.Configuration, now time.Time) error {
	if pool == nil || now.IsZero() || now.Location() != time.UTC || config.Validate(configuration) != nil {
		return fmt.Errorf("a11_reference_bootstrap_invalid")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:v1a-reference-bootstrap'))`); err != nil {
		return err
	}
	configurationID, configurationHash, err := bootstrapA11Configuration(ctx, tx, configuration, now)
	if err != nil {
		return err
	}
	if err = bootstrapA11MarketReferences(ctx, tx, configuration, configurationID, now); err != nil {
		return err
	}
	if err = bootstrapA11Models(ctx, tx, now); err != nil {
		return err
	}
	if err = bootstrapA11Trend(ctx, tx, configuration, now); err != nil {
		return err
	}
	if err = bootstrapA11Portfolio(ctx, tx, configuration, now); err != nil {
		return err
	}
	if err = verifyA11ReferenceBootstrap(ctx, tx, configurationID, configurationHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func bootstrapA11Configuration(ctx context.Context, tx pgx.Tx, configuration config.Configuration,
	now time.Time) (string, string, error) {
	canonical, err := json.Marshal(configuration)
	if err != nil {
		return "", "", err
	}
	hash := a11SHA256(canonical)
	id := "configuration-" + hash[:24]
	var version int64
	if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM configuration_versions WHERE configuration_hash=$1),
      (SELECT coalesce(max(version),0)+1 FROM configuration_versions))`, hash).Scan(&version); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO configuration_versions(id,version,configuration_hash,canonical_payload,
      actor,recorded_at) VALUES($1,$2,$3,$4,'admin_migrate',$5) ON CONFLICT (configuration_hash) DO NOTHING`,
		id, version, hash, canonical, now); err != nil {
		return "", "", err
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM configuration_versions WHERE configuration_hash=$1`, hash).Scan(&id); err != nil {
		return "", "", err
	}
	var active string
	_ = tx.QueryRow(ctx, `SELECT configuration_id FROM configuration_activations ORDER BY revision DESC LIMIT 1`).Scan(&active)
	if active != id {
		if _, err = tx.Exec(ctx, `INSERT INTO configuration_activations(configuration_id,actor,reason,activated_at)
        VALUES($1,'admin_migrate','credential-free V1A product bootstrap',$2)`, id, now); err != nil {
			return "", "", err
		}
	}
	return id, hash, nil
}

func bootstrapA11MarketReferences(ctx context.Context, tx pgx.Tx, configuration config.Configuration,
	configurationID string, now time.Time) error {
	exchanges := configuration.PublicExchanges()
	for _, asset := range configuration.Assets {
		if _, err := tx.Exec(ctx, `INSERT INTO assets(symbol) VALUES($1) ON CONFLICT DO NOTHING`, asset.Symbol); err != nil {
			return err
		}
		if err := ensureA11AssetScreening(ctx, tx, string(asset.Symbol), string(asset.Status), configurationID, now); err != nil {
			return err
		}
	}
	for _, exchange := range exchanges {
		if err := bootstrapPublicExchange(ctx, tx, exchange, now); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapPublicExchange(ctx context.Context, tx pgx.Tx,
	exchange config.ExchangeConfiguration, now time.Time) error {
	name := "Binance"
	if exchange.ID == "bybit" {
		name = "Bybit"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO exchanges(id,name,environment)
      VALUES($1,$2,'production_public') ON CONFLICT (id) DO NOTHING`, exchange.ID, name); err != nil {
		return err
	}
	for _, instrument := range exchange.Instruments {
		id := "instrument-" + instrument.Base + "-" + instrument.Quote
		if _, err := tx.Exec(ctx, `INSERT INTO instruments(id,base_asset,quote_asset,product)
        VALUES($1,$2,$3,'spot') ON CONFLICT(base_asset,quote_asset,product) DO NOTHING`,
			id, instrument.Base, instrument.Quote); err != nil {
			return err
		}
	}
	// Only callable production-public capabilities are materialized. Every
	// absent capability remains unsupported by construction and has no dormant
	// activation row in the runtime database.
	capabilities := map[string]bool{"public_metadata": true, "public_server_time": true,
		"public_trades": true, "public_candles": true, "public_order_book": true}
	for capability, supported := range capabilities {
		if _, err := tx.Exec(ctx, `INSERT INTO exchange_capabilities(exchange_id,version,capability,supported,recorded_at)
        VALUES($1,1,$2,$3,$4) ON CONFLICT DO NOTHING`, exchange.ID, capability, supported, now); err != nil {
			return err
		}
	}
	return nil
}

func ensureA11AssetScreening(ctx context.Context, executor a11ReferenceExecutor, symbol, status,
	configurationID string, now time.Time) error {
	id := "asset-screening-" + symbol + "-1"
	var exact bool
	err := executor.QueryRow(ctx, `SELECT id=$2 AND prior_status IS NULL AND status=$3
      AND actor='admin_migrate' AND reason='V1A locked registry'
      AND causation_id='reference-bootstrap' AND configuration_id=$4
      FROM asset_screening_versions WHERE asset_symbol=$1 AND version=1`,
		symbol, id, status, configurationID).Scan(&exact)
	if err == nil {
		if !exact {
			return fmt.Errorf("a11_reference_bootstrap_conflict")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = executor.Exec(ctx, `INSERT INTO asset_screening_versions(id,asset_symbol,version,prior_status,status,
      actor,reason,causation_id,configuration_id,effective_at,recorded_at)
      VALUES($1,$2,1,NULL,$3,'admin_migrate','V1A locked registry','reference-bootstrap',$4,$5,$5)`,
		id, symbol, status, configurationID, now)
	return err
}

func bootstrapA11Models(ctx context.Context, tx pgx.Tx, now time.Time) error {
	models := []struct{ id, kind, payload string }{
		{"fixed-bps-v1", "fee", `{"taker_rate":"0.001","maker_rate":"0"}`},
		{"fixed-zero-v1", "latency", `{"duration_nanos":"0"}`},
		{"full-fill-v1", "fill", `{"partial_ratio":"1"}`},
		{"fixed-zero-slippage-v1", "slippage", `{"rate":"0"}`},
		{"fixed-zero-gap-v1", "gap", `{"allowance":"0"}`},
	}
	for _, model := range models {
		payload := []byte(model.payload)
		if _, err := tx.Exec(ctx, `INSERT INTO model_versions(id,model_type,version,model_hash,canonical_payload,created_at)
        VALUES($1,$2,1,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, model.id, model.kind,
			a11SHA256(payload), payload, now); err != nil {
			return err
		}
	}
	namespacePayload := []byte(`{"fee":"fixed-bps-v1","fill":"full-fill-v1","latency":"fixed-zero-v1","market_context":"production-public","simulation_only":true}`)
	namespaceHash := a11SHA256(namespacePayload)
	if _, err := tx.Exec(ctx, `INSERT INTO model_namespaces(id,namespace_hash,market_context,liquidity_domain,
      fee_model_id,latency_model_id,fill_model_id,price_model_hash,canonical_payload,created_at)
      VALUES('production-public-v1a',$1,'production-public','production-public-v1a','fixed-bps-v1',
      'fixed-zero-v1','full-fill-v1',$2,$3,$4) ON CONFLICT(id) DO NOTHING`, namespaceHash,
		a11SHA256([]byte("recorded-first-executable-v1")), namespacePayload, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO liquidity_domains(id,namespace_id,available_quantity,revision,updated_at)
      VALUES('production-public-v1a','production-public-v1a',1000000000,1,$1) ON CONFLICT(id) DO NOTHING`, now)
	return err
}

func bootstrapA11Trend(ctx context.Context, tx pgx.Tx, configuration config.Configuration, now time.Time) error {
	manifest, err := json.Marshal(configuration.Trend)
	if err != nil {
		return err
	}
	hash := a11SHA256(manifest)
	if _, err = tx.Exec(ctx, `INSERT INTO strategy_definitions(id,name,family)
      VALUES('trend','trend','completed-candle trend breakout') ON CONFLICT(id) DO NOTHING`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO strategy_versions(id,strategy_id,version,implementation_hash,
      promotion_status,created_at,manifest_hash,canonical_manifest,supported_modes,author,notes)
      VALUES('trend-v1a-1','trend',1,$1,'research',$2,$1,$3,ARRAY['backtest','replay','shadow'],
      'Axiom','V1A implementation; no profitability claim') ON CONFLICT(id) DO NOTHING`, hash, now, manifest); err != nil {
		return err
	}
	for _, parameter := range configuration.Trend.Parameters {
		dependencies, _ := json.Marshal(parameter.ModelDependencies)
		if _, err = tx.Exec(ctx, `INSERT INTO strategy_parameters(strategy_version_id,parameter_name,decimal_value,
        unit,description,algorithm_version,minimum_value,maximum_value,minimum_inclusive,maximum_inclusive,
        decimal_scale,rounding,cadence,warm_up,mutability,model_dependencies)
        VALUES('trend-v1a-1',$1,$2,$3,$4,'trend.v1a.1',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
        ON CONFLICT(strategy_version_id,parameter_name) DO NOTHING`, parameter.ID, parameter.Value, parameter.Unit,
			parameter.Description, parameter.Minimum, parameter.Maximum, parameter.MinimumInclusive,
			parameter.MaximumInclusive, int(parameter.Scale), parameter.Rounding, parameter.Cadence,
			parameter.WarmUp, parameter.Mutability, string(dependencies)); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapA11Portfolio(ctx context.Context, tx pgx.Tx, configuration config.Configuration, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO portfolios(id,name,reporting_asset,created_at)
      VALUES('trend-v1a-portfolio','Trend V1A virtual portfolio',$1,$2) ON CONFLICT(id) DO NOTHING`,
		configuration.Portfolio.SettlementAsset, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO strategy_portfolios(portfolio_id,strategy_version_id,allocation,assigned_at)
      VALUES('trend-v1a-portfolio','trend-v1a-1',$1,$2) ON CONFLICT DO NOTHING`,
		configuration.Portfolio.StartingCapital.Value, now)
	return err
}

func verifyA11ReferenceBootstrap(ctx context.Context, tx pgx.Tx, configurationID, hash string) error {
	var facts int
	err := tx.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM configuration_versions WHERE id=$1 AND configuration_hash=$2)+
      (SELECT count(*) FROM exchanges WHERE id='binance' AND environment='production_public')+
      (SELECT count(*) FROM assets WHERE symbol IN ('USDT','BTC','ETH'))+
      (SELECT count(*) FROM instruments WHERE product='spot')+
      (SELECT count(*) FROM model_versions WHERE id IN ('fixed-bps-v1','fixed-zero-v1','full-fill-v1','fixed-zero-slippage-v1','fixed-zero-gap-v1'))+
      (SELECT count(*) FROM model_namespaces WHERE id='production-public-v1a')+
      (SELECT count(*) FROM strategy_parameters WHERE strategy_version_id='trend-v1a-1')+
      (SELECT count(*) FROM portfolios WHERE id='trend-v1a-portfolio')`, configurationID, hash).Scan(&facts)
	if err != nil || facts != 30 {
		return fmt.Errorf("a11_reference_bootstrap_incomplete")
	}
	return nil
}
