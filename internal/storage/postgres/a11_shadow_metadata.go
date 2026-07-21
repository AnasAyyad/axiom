package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
)

// RegisterMetadata returns an existing exact public metadata version or appends a new one.
func (store *A11ShadowStore) RegisterMetadata(ctx context.Context,
	metadata domain.InstrumentMetadata) (A11MetadataEvidence, error) {
	return store.RegisterPublicMetadata(ctx, "binance", metadata)
}

// RegisterPublicMetadata appends metadata under one allowlisted production-public venue.
func (store *A11ShadowStore) RegisterPublicMetadata(ctx context.Context, exchange string,
	metadata domain.InstrumentMetadata) (A11MetadataEvidence, error) {
	if metadata.Instrument.Product != domain.ProductSpot || metadata.Version == 0 {
		return A11MetadataEvidence{}, fmt.Errorf("a11_shadow_metadata_invalid")
	}
	if exchange != "binance" && exchange != "bybit" {
		return A11MetadataEvidence{}, fmt.Errorf("a11_shadow_exchange_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return A11MetadataEvidence{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"axiom:metadata:"+exchange+":"+metadata.Instrument.Symbol()); err != nil {
		return A11MetadataEvidence{}, err
	}
	exchangeID, instrumentID, err := a11MetadataReferences(ctx, tx, exchange, metadata.Instrument)
	if err != nil {
		return A11MetadataEvidence{}, err
	}
	evidence, found, err := exactA11Metadata(ctx, tx, exchangeID, instrumentID, metadata)
	if err != nil {
		return A11MetadataEvidence{}, err
	}
	if found {
		return evidence, tx.Commit(ctx)
	}
	return appendA11Metadata(ctx, tx, exchangeID, instrumentID, metadata, store.clock.Now().UTC)
}

func a11MetadataReferences(ctx context.Context, tx pgx.Tx, exchange string,
	instrument domain.Instrument) (string, string, error) {
	var exchangeID, instrumentID string
	err := tx.QueryRow(ctx, `SELECT exchange.id,instrument.id FROM exchanges exchange CROSS JOIN instruments instrument
	  WHERE exchange.id=$1 AND exchange.environment='production_public' AND
      instrument.base_asset=$2 AND instrument.quote_asset=$3 AND instrument.product='spot'`,
		exchange, instrument.Base, instrument.Quote).Scan(&exchangeID, &instrumentID)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("a11_shadow_metadata_reference_missing")
	}
	if err != nil {
		return "", "", fmt.Errorf("a11_shadow_metadata_reference_query_failed: %w", err)
	}
	return exchangeID, instrumentID, nil
}

func exactA11Metadata(ctx context.Context, tx pgx.Tx, exchangeID, instrumentID string,
	metadata domain.InstrumentMetadata) (A11MetadataEvidence, bool, error) {
	var id string
	var version int64
	err := tx.QueryRow(ctx, `SELECT id,version FROM instrument_metadata_versions
      WHERE exchange_id=$1 AND instrument_id=$2 AND price_tick=$3::numeric AND quantity_step=$4::numeric
      AND minimum_quantity=$5::numeric AND minimum_notional=$6::numeric
      ORDER BY version DESC LIMIT 1`, exchangeID, instrumentID, metadata.PriceTick.String(),
		metadata.QuantityStep.String(), metadata.MinimumQuantity.String(), metadata.MinimumNotional.String()).
		Scan(&id, &version)
	if err == pgx.ErrNoRows {
		return A11MetadataEvidence{}, false, nil
	}
	if err != nil {
		return A11MetadataEvidence{}, false, err
	}
	metadata.Version = uint64(version)
	return A11MetadataEvidence{ID: id, Metadata: metadata}, true, nil
}

func appendA11Metadata(ctx context.Context, tx pgx.Tx, exchangeID, instrumentID string,
	metadata domain.InstrumentMetadata, now time.Time) (A11MetadataEvidence, error) {
	var version int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM instrument_metadata_versions
      WHERE exchange_id=$1 AND instrument_id=$2`, exchangeID, instrumentID).Scan(&version); err != nil {
		return A11MetadataEvidence{}, err
	}
	id := fmt.Sprintf("metadata-%s-%s-%d", exchangeID, metadata.Instrument.Symbol(), version)
	_, err := tx.Exec(ctx, `INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,
      price_tick,quantity_step,minimum_quantity,minimum_notional,effective_at,recorded_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, id, exchangeID, instrumentID, version,
		metadata.PriceTick.String(), metadata.QuantityStep.String(), metadata.MinimumQuantity.String(),
		metadata.MinimumNotional.String(), now)
	if err != nil {
		return A11MetadataEvidence{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return A11MetadataEvidence{}, err
	}
	metadata.Version = uint64(version)
	return A11MetadataEvidence{ID: id, Metadata: metadata}, nil
}
