package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5"
)

func publicShadowExchangeID(ctx context.Context, tx pgx.Tx, selected string) (string, error) {
	if selected != "binance" && selected != "bybit" {
		return "", fmt.Errorf("owner_console_shadow_exchange_invalid")
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM exchanges WHERE id=$1 AND environment='production_public'`, selected).Scan(&id)
	return id, err
}

func nullableOwnerConsoleText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func verifyPublicShadowEvidenceLease(ctx context.Context, tx pgx.Tx, owner, id string) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT state IN ('PAUSED','RUNNING') AND claim_expires_at>CURRENT_TIMESTAMP
	      FROM shadow_sessions WHERE id=$1 AND claim_owner=$2 FOR UPDATE`, id, owner).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("owner_console_shadow_evidence_lease_lost")
	}
	if err != nil {
		return fmt.Errorf("owner_console_shadow_evidence_verification_failed")
	}
	if !active {
		return fmt.Errorf("owner_console_shadow_evidence_lease_lost")
	}
	return nil
}

// Evidence writes explicitly lock the leased shadow row before touching any
// projections. Read committed makes that lock the serialization boundary and
// avoids false SSI aborts when the independent lease-renewal loop updates the
// same row while an evidence transaction is beginning.
func publicShadowEvidenceTxOptions() pgx.TxOptions {
	return pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
}

func insertOwnerConsoleTrendDecision(ctx context.Context, tx pgx.Tx, input trend.Input, decision trend.Decision,
	explanation []byte) error {
	evidence := input.Evidence
	_, err := tx.Exec(ctx, `INSERT INTO trend_decisions(decision_id,explanation_hash,canonical_explanation,
      candle_view_id,candle_view_revision,market_view_id,market_view_revision,instrument_metadata_id,
      asset_eligibility_version,portfolio_revision,position_revision,fee_model_id,latency_model_id,fill_model_id,
      slippage_model_id,gap_model_id,correlation_id,causation_id,recorded_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		decision.ID.Value(), ownerConsoleSHA256(explanation), explanation, evidence.CandleViewID,
		evidence.CandleViewRevision, evidence.MarketViewID, evidence.MarketViewRevision,
		evidence.InstrumentMetadataID, evidence.AssetEligibilityVersion, evidence.PortfolioRevision,
		evidence.PositionRevision, evidence.FeeModelID, evidence.LatencyModelID, evidence.FillModelID,
		evidence.SlippageModelID, evidence.GapModelID, evidence.CorrelationID, evidence.CausationID, input.Now)
	return err
}

func storePublicShadowProjections(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	snapshot portfolio.Snapshot, now time.Time) error {
	for asset, balance := range snapshot.Balances {
		tag, err := tx.Exec(ctx, `UPDATE virtual_balances SET available=$1,reserved=$2,revision=$3,updated_at=$4
        WHERE account_id=$5 AND asset_symbol=$6`, balance.Available.String(), balance.Reserved.String(),
			balance.Revision, now, claim.AccountID, asset)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("owner_console_shadow_balance_projection_failed")
		}
	}
	for _, position := range snapshot.Positions {
		if position.Instrument.Base == "" {
			continue
		}
		instrumentID, err := publicShadowInstrumentID(ctx, tx, position.Instrument)
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

func insertPublicShadowStrategyEvidence(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence) error {
	_, err := tx.Exec(ctx, `INSERT INTO shadow_strategy_decision_evidence(decision_id,strategy_version_id,
      input_kind,input_hash,canonical_input,decision_hash,canonical_decision,correlation_id,causation_id,recorded_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, evidence.decisionID, claim.StrategyID,
		evidence.inputKind, ownerConsoleSHA256(evidence.inputPayload), evidence.inputPayload,
		ownerConsoleSHA256(evidence.decisionPayload), evidence.decisionPayload, evidence.correlationID,
		evidence.causationID, evidence.now)
	return err
}

func insertOwnerConsoleMultilegDecisionEvidence(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence,
) error {
	outcome := "rejected"
	if evidence.accepted {
		outcome = "accepted"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO decisions(id,run_id,configuration_id,strategy_version_id,outcome,
      reason_code,causation_id,decided_at,ingest_ordinal) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		evidence.decisionID, claim.RunID, claim.ConfigurationID, claim.StrategyID, outcome,
		evidence.reasonCode, evidence.causationID, evidence.now, evidence.ordinal); err != nil {
		return err
	}
	if err := insertPublicShadowStrategyEvidence(ctx, tx, claim, evidence); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO decision_inputs(decision_id,input_kind,input_id,version,input_hash)
      VALUES($1,$2,$3,$4,$5)`, evidence.decisionID, evidence.inputKind, evidence.inputID,
		evidence.inputRevision, ownerConsoleSHA256(evidence.inputPayload))
	return err
}

func insertPublicShadowOutbox(ctx context.Context, tx pgx.Tx, evidence publicShadowDecisionEvidence) error {
	identity := append([]byte(evidence.decisionID+"\x00"), evidence.decisionPayload...)
	id := "stream-" + ownerConsoleSHA256(identity)[:24]
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events(id,topic,payload_hash,created_at,stream,schema_version,
      entity_type,entity_id,entity_revision,event_time,correlation_id,causation_id,payload)
	      VALUES($1,$2,$3,$4,$5,'axiom.stream.v1',$6,$7,$8,$4,$9,$10,$11)`,
		id, evidence.outboxTopic, ownerConsoleSHA256(evidence.decisionPayload), evidence.now, evidence.outboxStream,
		evidence.outboxEntity, evidence.decisionID, evidence.ordinal, evidence.correlationID,
		evidence.causationID, string(evidence.decisionPayload))
	return err
}

func publicShadowInstrumentID(ctx context.Context, tx pgx.Tx, instrument domain.Instrument) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM instruments WHERE base_asset=$1 AND quote_asset=$2 AND product='spot'`,
		instrument.Base, instrument.Quote).Scan(&id)
	return id, err
}
