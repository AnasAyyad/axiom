package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/strategies/crossarb"

	"github.com/jackc/pgx/v5"
)

// InitializeCrossExchangeShadowInventory atomically converts each venue's
// isolated virtual capital into the exact selected-instrument prefund. It uses
// public-book evidence only and creates no exchange order or transfer route.
func (store *PublicShadowStore) InitializeCrossExchangeShadowInventory(
	ctx context.Context,
	claim PublicShadowClaim,
	markets []crossarb.MarketInput,
	at time.Time,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if store == nil || claim.StrategyID != "cross-exchange-arbitrage-1-0-0" ||
		claim.StrategyVersion != "cross-exchange-arbitrage@1.0.0" || config.Validate(claim.Configuration) != nil ||
		len(claim.VenueAccountIDs) != 2 || at.IsZero() || at.Location() != time.UTC {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
	}
	capital, capitalErr := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	half, halfErr := domain.ParsePercent("0.5")
	venueCapital, scaleErr := domain.ScaleBalanceFloor(capital, half, 18)
	projected, initializations, initializationErr := crossarb.InitializeSingleInstrumentInventory(markets, venueCapital)
	if capitalErr != nil || halfErr != nil || scaleErr != nil || initializationErr != nil ||
		len(projected) != 2 || len(initializations) != 2 {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return nil, err
	}
	var instrumentID string
	if err = tx.QueryRow(ctx, `SELECT id FROM instruments
      WHERE base_asset || quote_asset=$1 AND product='spot'`, claim.InstrumentID).Scan(&instrumentID); err != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_instrument_invalid")
	}
	result := make(map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, 2)
	for _, initialization := range initializations {
		updated, updateErr := initializeOwnerConsoleCrossExchangeVenue(ctx, tx, claim, initialization,
			projected[initialization.Exchange], venueCapital, instrumentID, at, uint64(1+len(result)))
		if updateErr != nil {
			return nil, updateErr
		}
		result[initialization.Exchange] = updated
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type ownerConsoleCrossExchangeInitializationPayload struct {
	SessionID string                                `json:"session_id"`
	AccountID string                                `json:"account_id"`
	Evidence  crossarb.VenueInitialization          `json:"evidence"`
	Balances  map[domain.AssetSymbol]domain.Balance `json:"balances"`
}

func initializeOwnerConsoleCrossExchangeVenue(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	initialization crossarb.VenueInitialization, projected map[domain.AssetSymbol]domain.Balance,
	venueCapital domain.Balance, instrumentID string, at time.Time, ordinal uint64,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	accountID := claim.VenueAccountIDs[initialization.Exchange]
	if accountID == "" || initialization.BaseAsset == "" {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
	}
	current, err := loadOwnerConsoleCrossExchangeAccountBalances(ctx, tx, accountID)
	if err != nil || !ownerConsoleCrossExchangeUninitializedBalances(current, venueCapital) {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_stale")
	}
	payload, err := json.Marshal(ownerConsoleCrossExchangeInitializationPayload{
		claim.ID, accountID, initialization, projected})
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
	}
	hash := ownerConsoleSHA256(payload)
	transactionID := "cross-shadow-init-" + hash[:24]
	if err = insertOwnerConsoleCrossExchangeInitializationJournal(ctx, tx, claim, initialization,
		accountID, transactionID, hash, at, ordinal); err != nil {
		return nil, err
	}
	updated, err := projectOwnerConsoleCrossExchangeInitializationBalances(
		ctx, tx, accountID, current, projected, at)
	if err != nil {
		return nil, err
	}
	if err = insertOwnerConsoleCrossExchangeInitializationEvidence(ctx, tx, claim, initialization,
		accountID, instrumentID, transactionID, hash, payload, at); err != nil {
		return nil, err
	}
	return updated, nil
}

func insertOwnerConsoleCrossExchangeInitializationJournal(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	initialization crossarb.VenueInitialization, accountID, transactionID, hash string, at time.Time,
	ordinal uint64,
) error {
	consumed, err := initialization.VenueCapital.Subtract(initialization.AvailableUSDT)
	if err != nil || consumed.String() == "0" {
		return fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
	}
	causationID := "cross-shadow-init-event-" + hash[:20]
	if _, err = tx.Exec(ctx, `INSERT INTO journal_transactions(id,transaction_type,run_id,portfolio_id,
      configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
      VALUES($1,'cross_exchange_inventory_initialization',$2,$3,$4,$5,$6,$7,$8)`, transactionID,
		claim.RunID, claim.PortfolioID, claim.ConfigurationID, causationID, claim.ID, at, ordinal); err != nil {
		return err
	}
	owner := "cross_exchange:" + initialization.Exchange
	lines := [][4]string{
		{string(accounting.ExternalEquity), "USDT", string(accounting.Debit), consumed.String()},
		{string(accounting.AvailableAsset), "USDT", string(accounting.Credit), consumed.String()},
		{string(accounting.AvailableAsset), string(initialization.BaseAsset), string(accounting.Debit), initialization.BaseQuantity.String()},
		{string(accounting.ExternalEquity), string(initialization.BaseAsset), string(accounting.Credit), initialization.BaseQuantity.String()}}
	for index, line := range lines {
		if _, err = tx.Exec(ctx, `INSERT INTO ledger_entries(transaction_id,line_number,account_class,
          account_owner,asset_symbol,direction,quantity) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			transactionID, index+1, line[0], owner, line[1], line[2], line[3]); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO portfolio_ownership(account_id,portfolio_id,exchange_id,
      strategy_version_id,strategy_key,initialization_transaction_id,numeraire_asset,ownership_hash,created_at)
      VALUES($1,$2,$3,$4,'cross_exchange',$5,'USDT',$6,$7)`, accountID, claim.PortfolioID,
		initialization.Exchange, claim.StrategyID, transactionID,
		ownerConsoleSHA256([]byte(claim.PortfolioID+"\x00"+accountID+"\x00"+initialization.Exchange+"\x00"+hash)), at)
	return err
}

func projectOwnerConsoleCrossExchangeInitializationBalances(ctx context.Context, tx pgx.Tx, accountID string,
	current map[domain.AssetSymbol]accounting.BalanceSnapshot,
	projected map[domain.AssetSymbol]domain.Balance, at time.Time,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	updated := make(map[domain.AssetSymbol]accounting.BalanceSnapshot, 3)
	for asset, before := range current {
		after, exists := projected[asset]
		if !exists {
			return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_invalid")
		}
		tag, err := tx.Exec(ctx, `UPDATE virtual_balances SET available=$1,reserved=0,
          revision=revision+1,updated_at=$2 WHERE account_id=$3 AND asset_symbol=$4
          AND revision=$5 AND available=$6 AND reserved=0`, after.String(), at, accountID,
			asset, before.Revision, before.Available.String())
		if err != nil || tag.RowsAffected() != 1 {
			return nil, fmt.Errorf("owner_console_shadow_cross_exchange_initialization_conflict")
		}
		updated[asset] = accounting.BalanceSnapshot{Available: after, Revision: before.Revision + 1}
	}
	return updated, nil
}

func insertOwnerConsoleCrossExchangeInitializationEvidence(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	initialization crossarb.VenueInitialization, accountID, instrumentID, transactionID, hash string,
	payload []byte, at time.Time,
) error {
	_, err := tx.Exec(ctx, `INSERT INTO shadow_cross_exchange_inventory_initializations(
      session_id,exchange_id,account_id,instrument_id,base_asset,venue_capital,target_base_value,
      reference_price,base_quantity,available_usdt,model_version,unselected_asset_rule,
      initialization_transaction_id,canonical_hash,canonical_payload,initialized_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, claim.ID,
		initialization.Exchange, accountID, instrumentID, initialization.BaseAsset,
		initialization.VenueCapital.String(), initialization.TargetBaseValue.String(),
		initialization.ReferencePrice.String(), initialization.BaseQuantity.String(),
		initialization.AvailableUSDT.String(), initialization.ModelVersion,
		initialization.UnselectedAssetRule, transactionID, hash, payload, at)
	return err
}

func loadOwnerConsoleCrossExchangeAccountBalances(ctx context.Context, tx pgx.Tx,
	accountID string,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	return loadOwnerConsoleTriangularBalances(ctx, tx, accountID)
}

func ownerConsoleCrossExchangeUninitializedBalances(
	values map[domain.AssetSymbol]accounting.BalanceSnapshot,
	venueCapital domain.Balance,
) bool {
	if len(values) != 3 {
		return false
	}
	zero, _ := domain.ParseBalance("0")
	for asset, value := range values {
		if value.Revision != 1 || value.Reserved.Compare(zero) != 0 {
			return false
		}
		if asset == domain.AssetSymbol("USDT") {
			if value.Available.Compare(venueCapital) != 0 {
				return false
			}
		} else if value.Available.Compare(zero) != 0 {
			return false
		}
	}
	return true
}
