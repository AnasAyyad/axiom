package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"

	"github.com/jackc/pgx/v5"
)

// RegisterMetadata returns an existing exact public metadata version or appends a new one.
func (store *PublicShadowStore) RegisterMetadata(ctx context.Context,
	metadata domain.InstrumentMetadata) (PublicShadowMetadataEvidence, error) {
	return store.RegisterPublicMetadata(ctx, "binance", metadata)
}

// RegisterPublicInstrument preserves the exact maximum Spot quantity alongside
// the shared minimum/tick/step metadata. This is required by multi-leg sizing.
func (store *PublicShadowStore) RegisterPublicInstrument(ctx context.Context,
	record exchangecontracts.InstrumentRecord) (PublicShadowMetadataEvidence, error) {
	zero, _ := domain.ParseQuantity("0")
	if record.RawPayloadHash == "" || record.NativeSymbol != record.Metadata.Instrument.Symbol() ||
		record.MaximumQuantity.Compare(zero) <= 0 ||
		record.MaximumQuantity.Compare(record.Metadata.MinimumQuantity) < 0 {
		return PublicShadowMetadataEvidence{}, fmt.Errorf("owner_console_shadow_instrument_record_invalid")
	}
	return store.registerPublicMetadata(ctx, string(record.Exchange), record.Metadata, &record.MaximumQuantity)
}

// RegisterPublicMetadata appends metadata under one allowlisted production-public venue.
func (store *PublicShadowStore) RegisterPublicMetadata(ctx context.Context, exchange string,
	metadata domain.InstrumentMetadata) (PublicShadowMetadataEvidence, error) {
	return store.registerPublicMetadata(ctx, exchange, metadata, nil)
}

func (store *PublicShadowStore) registerPublicMetadata(ctx context.Context, exchange string,
	metadata domain.InstrumentMetadata, maximumQuantity *domain.Quantity) (PublicShadowMetadataEvidence, error) {
	if metadata.Instrument.Product != domain.ProductSpot || metadata.Version == 0 {
		return PublicShadowMetadataEvidence{}, fmt.Errorf("owner_console_shadow_metadata_invalid")
	}
	if exchange != "binance" && exchange != "bybit" {
		return PublicShadowMetadataEvidence{}, fmt.Errorf("owner_console_shadow_exchange_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"axiom:metadata:"+exchange+":"+metadata.Instrument.Symbol()); err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	exchangeID, instrumentID, err := publicShadowMetadataReferences(ctx, tx, exchange, metadata.Instrument)
	if err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	evidence, found, err := exactPublicShadowMetadata(ctx, tx, exchangeID, instrumentID, metadata, maximumQuantity)
	if err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	if found {
		return evidence, tx.Commit(ctx)
	}
	return appendPublicShadowMetadata(ctx, tx, exchangeID, instrumentID, metadata, maximumQuantity, store.clock.Now().UTC)
}

func publicShadowMetadataReferences(ctx context.Context, tx pgx.Tx, exchange string,
	instrument domain.Instrument) (string, string, error) {
	var exchangeID, instrumentID string
	err := tx.QueryRow(ctx, `SELECT exchange.id,instrument.id FROM exchanges exchange CROSS JOIN instruments instrument
	  WHERE exchange.id=$1 AND exchange.environment='production_public' AND
      instrument.base_asset=$2 AND instrument.quote_asset=$3 AND instrument.product='spot'`,
		exchange, instrument.Base, instrument.Quote).Scan(&exchangeID, &instrumentID)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("owner_console_shadow_metadata_reference_missing")
	}
	if err != nil {
		return "", "", fmt.Errorf("owner_console_shadow_metadata_reference_query_failed: %w", err)
	}
	return exchangeID, instrumentID, nil
}

func exactPublicShadowMetadata(ctx context.Context, tx pgx.Tx, exchangeID, instrumentID string,
	metadata domain.InstrumentMetadata, maximumQuantity *domain.Quantity) (PublicShadowMetadataEvidence, bool, error) {
	var id string
	var version int64
	err := tx.QueryRow(ctx, `SELECT id,version FROM instrument_metadata_versions
      WHERE exchange_id=$1 AND instrument_id=$2 AND price_tick=$3::numeric AND quantity_step=$4::numeric
      AND minimum_quantity=$5::numeric AND minimum_notional=$6::numeric
	  AND (($7::numeric IS NULL AND maximum_quantity IS NULL) OR maximum_quantity=$7::numeric)
      ORDER BY version DESC LIMIT 1`, exchangeID, instrumentID, metadata.PriceTick.String(),
		metadata.QuantityStep.String(), metadata.MinimumQuantity.String(), metadata.MinimumNotional.String(),
		nullableOwnerConsoleMaximumQuantity(maximumQuantity)).
		Scan(&id, &version)
	if err == pgx.ErrNoRows {
		return PublicShadowMetadataEvidence{}, false, nil
	}
	if err != nil {
		return PublicShadowMetadataEvidence{}, false, err
	}
	metadata.Version = uint64(version)
	evidence := PublicShadowMetadataEvidence{ID: id, Metadata: metadata}
	if maximumQuantity != nil {
		evidence.MaximumQuantity = *maximumQuantity
	}
	return evidence, true, nil
}

func appendPublicShadowMetadata(ctx context.Context, tx pgx.Tx, exchangeID, instrumentID string,
	metadata domain.InstrumentMetadata, maximumQuantity *domain.Quantity, now time.Time) (PublicShadowMetadataEvidence, error) {
	var version int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM instrument_metadata_versions
      WHERE exchange_id=$1 AND instrument_id=$2`, exchangeID, instrumentID).Scan(&version); err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	id := fmt.Sprintf("metadata-%s-%s-%d", exchangeID, metadata.Instrument.Symbol(), version)
	_, err := tx.Exec(ctx, `INSERT INTO instrument_metadata_versions(id,exchange_id,instrument_id,version,
	  price_tick,quantity_step,minimum_quantity,minimum_notional,maximum_quantity,effective_at,recorded_at)
	  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, id, exchangeID, instrumentID, version,
		metadata.PriceTick.String(), metadata.QuantityStep.String(), metadata.MinimumQuantity.String(),
		metadata.MinimumNotional.String(), nullableOwnerConsoleMaximumQuantity(maximumQuantity), now)
	if err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PublicShadowMetadataEvidence{}, err
	}
	metadata.Version = uint64(version)
	evidence := PublicShadowMetadataEvidence{ID: id, Metadata: metadata}
	if maximumQuantity != nil {
		evidence.MaximumQuantity = *maximumQuantity
	}
	return evidence, nil
}

func nullableOwnerConsoleMaximumQuantity(value *domain.Quantity) any {
	if value == nil {
		return nil
	}
	return value.String()
}
