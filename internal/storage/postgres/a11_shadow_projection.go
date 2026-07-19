package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5"
)

func a11ShadowExchangeID(ctx context.Context, tx pgx.Tx) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM exchanges WHERE id='binance' AND environment='production_public'`).Scan(&id)
	return id, err
}

func nullableA11Text(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func verifyA11ShadowEvidenceLease(ctx context.Context, tx pgx.Tx, owner, id string) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT state IN ('PAUSED','RUNNING') AND claim_expires_at>CURRENT_TIMESTAMP
      FROM shadow_sessions WHERE id=$1 AND claim_owner=$2 FOR UPDATE`, id, owner).Scan(&active); err != nil || !active {
		return fmt.Errorf("a11_shadow_evidence_lease_lost")
	}
	return nil
}

func insertA11TrendDecision(ctx context.Context, tx pgx.Tx, input trend.Input, decision trend.Decision,
	explanation []byte) error {
	evidence := input.Evidence
	_, err := tx.Exec(ctx, `INSERT INTO trend_decisions(decision_id,explanation_hash,canonical_explanation,
      candle_view_id,candle_view_revision,market_view_id,market_view_revision,instrument_metadata_id,
      asset_eligibility_version,portfolio_revision,position_revision,fee_model_id,latency_model_id,fill_model_id,
      slippage_model_id,gap_model_id,correlation_id,causation_id,recorded_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		decision.ID.Value(), a11SHA256(explanation), explanation, evidence.CandleViewID,
		evidence.CandleViewRevision, evidence.MarketViewID, evidence.MarketViewRevision,
		evidence.InstrumentMetadataID, evidence.AssetEligibilityVersion, evidence.PortfolioRevision,
		evidence.PositionRevision, evidence.FeeModelID, evidence.LatencyModelID, evidence.FillModelID,
		evidence.SlippageModelID, evidence.GapModelID, evidence.CorrelationID, evidence.CausationID, input.Now)
	return err
}

func storeA11ShadowProjections(ctx context.Context, tx pgx.Tx, claim A11ShadowClaim,
	snapshot portfolio.Snapshot, now time.Time) error {
	for asset, balance := range snapshot.Balances {
		tag, err := tx.Exec(ctx, `UPDATE virtual_balances SET available=$1,reserved=$2,revision=$3,updated_at=$4
        WHERE account_id=$5 AND asset_symbol=$6`, balance.Available.String(), balance.Reserved.String(),
			balance.Revision, now, claim.AccountID, asset)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("a11_shadow_balance_projection_failed")
		}
	}
	for _, position := range snapshot.Positions {
		if position.Instrument.Base == "" {
			continue
		}
		instrumentID, err := a11ShadowInstrumentID(ctx, tx, position.Instrument)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO positions(account_id,instrument_id,quantity,weighted_average_cost,
        realized_pnl,revision,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT(account_id,instrument_id) DO UPDATE SET quantity=excluded.quantity,
        weighted_average_cost=excluded.weighted_average_cost,realized_pnl=excluded.realized_pnl,
        revision=excluded.revision,updated_at=excluded.updated_at`, claim.AccountID, instrumentID,
			position.Quantity.String(), position.WeightedAverageCost.String(), position.RealizedPnL.String(),
			position.Revision, now)
		if err != nil {
			return err
		}
	}
	return nil
}

func insertA11ShadowOutbox(ctx context.Context, tx pgx.Tx, input trend.Input, decisionID string, payload []byte) error {
	id := "stream-" + a11SHA256(payload)[:24]
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events(id,topic,payload_hash,created_at,stream,schema_version,
      entity_type,entity_id,entity_revision,event_time,correlation_id,causation_id,payload)
      VALUES($1,'trend.decision',$2,$3,'trend','axiom.stream.v1','trend_decision',$4,$5,$3,$6,$7,$8)`,
		id, a11SHA256(payload), input.Now, decisionID, input.Ordinal,
		input.Evidence.CorrelationID, input.Evidence.CausationID, string(payload))
	return err
}

func a11ShadowInstrumentID(ctx context.Context, tx pgx.Tx, instrument domain.Instrument) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM instruments WHERE base_asset=$1 AND quote_asset=$2 AND product='spot'`,
		instrument.Base, instrument.Quote).Scan(&id)
	return id, err
}
