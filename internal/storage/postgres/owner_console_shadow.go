package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

// CreateShadow persists one production-public, simulation-only shadow request.
func (store *OwnerConsoleStore) CreateShadow(ctx context.Context, principal authentication.Principal, key string, body generated.ShadowSessionRequest) (generated.ShadowSessionResource, error) {
	return store.createShadow(ctx, principal, key, body, "binance", "")
}

// createShadow persists the server-reviewed venue and optional exact
// instrument selected by the unified owner-run catalogue. An empty instrument
// is retained only for the legacy shadow command, whose historical contract
// evaluated the complete configuration universe.
func (store *OwnerConsoleStore) createShadow(ctx context.Context, principal authentication.Principal, key string,
	body generated.ShadowSessionRequest, exchange, instrument string,
) (generated.ShadowSessionResource, error) {
	payload := any(body)
	if instrument != "" || exchange != "binance" {
		payload = map[string]any{"request": body, "exchange": exchange, "instrument": instrument}
	}
	return store.createSelectedShadow(ctx, principal, key, body.PortfolioId, body.ConfigurationId,
		ownerConsoleStrategyVersionID(string(body.StrategyVersion)), payload, exchange, instrument)
}

func (store *OwnerConsoleStore) createSelectedShadow(ctx context.Context, principal authentication.Principal, key,
	portfolioID, configurationID, strategyVersionID string, payload any, exchange, instrument string,
) (generated.ShadowSessionResource, error) {
	if (exchange != "binance" && exchange != "bybit") || portfolioID == "" ||
		configurationID == "" || strategyVersionID == "" {
		return generated.ShadowSessionResource{}, console.ErrInvalidRequest
	}
	_, hash, err := ownerConsoleCommandPayload(payload)
	if err != nil {
		return generated.ShadowSessionResource{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	dedupe := ownerConsoleDedupe(principal.UserID, key)
	existingID, found, err := existingPublicShadow(ctx, tx, dedupe, hash)
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	if found {
		if err = tx.Commit(ctx); err != nil {
			return generated.ShadowSessionResource{}, err
		}
		return store.Shadow(ctx, existingID)
	}
	instrumentID, marketScopes, resolveErr := resolvePublicShadowMarketScopes(ctx, tx,
		strategyVersionID, exchange, instrument)
	if resolveErr != nil {
		return generated.ShadowSessionResource{}, resolveErr
	}
	if ready, checkErr := store.publicShadowReady(ctx, tx, portfolioID, configurationID,
		strategyVersionID, exchange, publicShadowScopeInstrumentIDs(marketScopes)); checkErr != nil || !ready {
		if checkErr != nil {
			return generated.ShadowSessionResource{}, checkErr
		}
		return generated.ShadowSessionResource{}, console.ErrPrecondition
	}
	sessionID, err := store.insertSelectedShadow(ctx, tx, principal, key, hash, portfolioID,
		configurationID, strategyVersionID, exchange, instrument, instrumentID, marketScopes)
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.ShadowSessionResource{}, err
	}
	return store.Shadow(ctx, sessionID)
}

func existingPublicShadow(ctx context.Context, tx pgx.Tx, dedupe, hash string) (string, bool, error) {
	var id, existingHash string
	err := tx.QueryRow(ctx, `SELECT ss.id,cr.payload_hash FROM shadow_sessions ss JOIN command_requests cr ON cr.id=ss.command_id WHERE cr.deduplication_key=$1`, dedupe).Scan(&id, &existingHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if existingHash != hash {
		return "", false, console.ErrIdempotencyConflict
	}
	return id, true, nil
}

func (store *OwnerConsoleStore) insertSelectedShadow(ctx context.Context, tx pgx.Tx,
	principal authentication.Principal, key, hash, portfolioID, configurationID, strategyVersionID,
	exchange, instrument, instrumentID string, marketScopes []publicShadowMarketScopeSelection,
) (string, error) {
	now := store.clock.Now().UTC
	sessionID, _ := ownerConsoleIdentifier("shadow")
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	if err := insertOwnerConsoleCommand(ctx, tx, commandID, principal, key, hash, "create_shadow", "shadow_session",
		sessionID, "start production-public simulation", now, auditID, commandID); err != nil {
		return "", err
	}
	marketScopeRequired := len(marketScopes) > 0
	if _, err := tx.Exec(ctx, `INSERT INTO shadow_sessions(id,command_id,state,revision,public_exchange,simulation_only,entries_enabled,portfolio_id,configuration_id,strategy_version_id,created_at,exchange_id,instrument_id,market_scope_required)
		VALUES($1,$2,'QUEUED',1,$3,true,false,$4,$5,$6,$7,$8,$9,$10)`, sessionID, commandID,
		exchange+"-production-public", portfolioID, configurationID, strategyVersionID, now,
		exchange, nullableOwnerConsoleText(instrumentID), marketScopeRequired); err != nil {
		return "", ownerConsoleConstraintError(err)
	}
	for _, scope := range marketScopes {
		if _, err := tx.Exec(ctx, `INSERT INTO shadow_session_market_scopes(
	      session_id,ordinal,exchange_id,instrument_id,purpose) VALUES($1,$2,$3,$4,$5)`,
			sessionID, scope.ordinal, scope.exchangeID, scope.instrumentID, scope.purpose); err != nil {
			return "", ownerConsoleConstraintError(err)
		}
	}
	_, err := completeOwnerConsoleCommand(ctx, tx, commandID, auditID, principal, "create_shadow", sessionID, hash,
		map[string]any{"shadow_session_id": sessionID, "state": "QUEUED", "simulation_only": true,
			"public_only": true, "exchange": exchange, "instrument": instrument}, now, commandID)
	return sessionID, err
}

type publicShadowMarketScopeSelection struct {
	ordinal      int16
	exchangeID   string
	instrumentID string
	purpose      string
}

func resolvePublicShadowMarketScopes(
	ctx context.Context,
	tx pgx.Tx,
	strategyVersionID, exchange, instrument string,
) (string, []publicShadowMarketScopeSelection, error) {
	primary, err := resolvePublicShadowInstrument(ctx, tx, instrument)
	if err != nil || primary == "" {
		return primary, nil, err
	}
	if strategyVersionID == "cross-exchange-arbitrage-1-0-0" {
		if exchange != "binance" || (instrument != "BTC/USDT" && instrument != "ETH/USDT") {
			return "", nil, console.ErrInvalidRequest
		}
		return primary, []publicShadowMarketScopeSelection{
			{ordinal: 1, exchangeID: "binance", instrumentID: primary, purpose: "paired_market"},
			{ordinal: 2, exchangeID: "bybit", instrumentID: primary, purpose: "paired_market"},
		}, nil
	}
	if strategyVersionID != "triangular-arbitrage-1-0-0" {
		return primary, []publicShadowMarketScopeSelection{{ordinal: 1, exchangeID: exchange,
			instrumentID: primary, purpose: "primary"}}, nil
	}
	base, quote, ok := ownerRunInstrument(instrument)
	if !ok || quote != "USDT" || (base != "BTC" && base != "ETH") {
		return "", nil, console.ErrInvalidRequest
	}
	values := []struct {
		base, quote string
	}{{"BTC", "USDT"}, {"ETH", "BTC"}, {"ETH", "USDT"}}
	scopes := make([]publicShadowMarketScopeSelection, 0, len(values))
	for index, value := range values {
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM instruments
		  WHERE base_asset=$1 AND quote_asset=$2 AND product='spot'`, value.base, value.quote).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", nil, console.NewWorkflowBlocker("TRIANGLE_MARKET_SET_UNAVAILABLE",
					"The complete three-market Triangle set is not registered.",
					"BTC/USDT, ETH/BTC, and ETH/USDT must all have reviewed public Spot metadata.",
					"No shadow session was created.", "Wait for all three public markets to become available.",
					"incomplete Triangle market set", "three reviewed Spot markets", "public instrument metadata")
			}
			return "", nil, err
		}
		scopes = append(scopes, publicShadowMarketScopeSelection{ordinal: int16(index + 1),
			exchangeID: exchange, instrumentID: id, purpose: "triangle_market"})
	}
	return primary, scopes, nil
}

func publicShadowScopeInstrumentIDs(scopes []publicShadowMarketScopeSelection) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scope.instrumentID)
	}
	return result
}

func resolvePublicShadowInstrument(ctx context.Context, tx pgx.Tx, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	base, quote, ok := ownerRunInstrument(value)
	if !ok {
		return "", console.ErrInvalidRequest
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM instruments WHERE base_asset=$1 AND quote_asset=$2 AND product='spot'`,
		base, quote).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", console.NewWorkflowBlocker("SHADOW_INSTRUMENT_UNAVAILABLE",
			"That spot instrument is not registered for public shadow.",
			"The server could not bind the reviewed run choice to an immutable spot instrument.",
			"No shadow session was created.", "Choose an instrument shown by the current run catalogue.",
			"instrument not registered", "registered spot instrument", "instrument metadata")
	}
	return id, err
}

func (store *OwnerConsoleStore) publicShadowReady(ctx context.Context, tx pgx.Tx, portfolioID, configurationID,
	strategyVersionID, exchange string, instrumentIDs []string,
) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM portfolios WHERE id=$1) AND
		EXISTS(SELECT 1 FROM configuration_versions WHERE id=$2) AND
		EXISTS(SELECT 1 FROM strategy_versions WHERE id=$3) AND
		(($3='cross-exchange-arbitrage-1-0-0' AND (SELECT count(DISTINCT (metadata.exchange_id,metadata.instrument_id))
		  FROM instrument_metadata_versions metadata JOIN exchanges venue ON venue.id=metadata.exchange_id
		  WHERE venue.id IN ('binance','bybit') AND venue.environment='production_public'
		    AND metadata.instrument_id=ANY($5::text[]))=2) OR
		 ($3<>'cross-exchange-arbitrage-1-0-0' AND ((cardinality($5::text[])=0 AND EXISTS(
		  SELECT 1 FROM instrument_metadata_versions metadata JOIN exchanges venue ON venue.id=metadata.exchange_id
		  WHERE venue.id=$4 AND venue.environment='production_public')) OR
		 (SELECT count(DISTINCT metadata.instrument_id) FROM instrument_metadata_versions metadata
		  JOIN exchanges venue ON venue.id=metadata.exchange_id WHERE venue.id=$4
		  AND venue.environment='production_public' AND metadata.instrument_id=ANY($5::text[]))=cardinality($5::text[])))) AND
		(($3 NOT IN ('triangular-arbitrage-1-0-0','cross-exchange-arbitrage-1-0-0') AND EXISTS(
		  SELECT 1 FROM market_data_segments segment JOIN exchanges venue ON venue.id=segment.exchange_id
		  WHERE venue.id=$4 AND venue.environment='production_public' AND segment.state='ready'
		    AND segment.event_type IN ('candle','mixed_public') AND segment.ended_at >= $6)) OR
		 ($3='triangular-arbitrage-1-0-0' AND (SELECT count(DISTINCT segment.instrument_id)
		  FROM market_data_segments segment JOIN exchanges venue ON venue.id=segment.exchange_id
		  WHERE venue.id=$4 AND venue.environment='production_public' AND segment.state='ready'
		    AND segment.event_type IN ('book','mixed_public') AND segment.instrument_id=ANY($5::text[])
		    AND segment.ended_at >= $6)=cardinality($5::text[])) OR
		 ($3='cross-exchange-arbitrage-1-0-0' AND (SELECT count(DISTINCT (segment.exchange_id,segment.instrument_id))
		  FROM market_data_segments segment JOIN exchanges venue ON venue.id=segment.exchange_id
		  WHERE venue.id IN ('binance','bybit') AND venue.environment='production_public'
		    AND segment.state='ready' AND segment.event_type IN ('book','mixed_public')
		    AND segment.instrument_id=ANY($5::text[]) AND segment.ended_at >= $6)=2)) AND
		EXISTS(SELECT 1 FROM startup_recovery_attempts attempt WHERE attempt.state='ready_paused' AND
		  (SELECT count(*) FROM startup_recovery_evidence evidence WHERE evidence.attempt_id=attempt.id)=14) AND
		NOT EXISTS(SELECT 1 FROM circuit_breaker_events WHERE breaker_kind='disk_failure') AND
			EXISTS(SELECT 1 FROM owner_console_storage_pressure_state WHERE scope_id='market-data'
			  AND level='NORMAL' AND source_instance<>'migration-bootstrap'
			  AND observed_at>=CURRENT_TIMESTAMP-interval '2 minutes')`,
		portfolioID, configurationID, strategyVersionID, exchange, instrumentIDs,
		store.clock.Now().UTC.Add(-5*time.Hour)).Scan(&ready)
	return ready, err
}

// StopShadow records and applies an idempotent graceful stop request.
func (store *OwnerConsoleStore) StopShadow(ctx context.Context, principal authentication.Principal, id, key string, body generated.RevisionCommandRequest) (generated.CommandAccepted, error) {
	_, hash, err := ownerConsoleCommandPayload(map[string]any{"id": id, "body": body})
	if err != nil || body.Reason == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupOwnerConsoleCommand(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
		return generated.CommandAccepted{}, lookupErr
	} else if found {
		return existing, tx.Commit(ctx)
	}
	var state string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT state,revision FROM shadow_sessions WHERE id=$1 FOR UPDATE`, id).Scan(&state, &revision); errors.Is(err, pgx.ErrNoRows) {
		return generated.CommandAccepted{}, console.ErrNotFound
	} else if err != nil {
		return generated.CommandAccepted{}, err
	}
	if strconv.FormatInt(revision, 10) != body.ExpectedRevision {
		return generated.CommandAccepted{}, console.ErrConflict
	}
	now := store.clock.Now().UTC
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	if err = insertOwnerConsoleCommand(ctx, tx, commandID, principal, key, hash, "stop_shadow", "shadow_session", id, body.Reason, now, auditID, commandID); err != nil {
		return generated.CommandAccepted{}, err
	}
	next, valid := publicShadowStopTransition(state)
	if !valid {
		return generated.CommandAccepted{}, console.ErrConflict
	}
	if next != state {
		_, err = tx.Exec(ctx, `UPDATE shadow_sessions SET state=$2,revision=revision+1,entries_enabled=false,stopped_at=CASE WHEN $2='CANCELED' THEN $3 ELSE stopped_at END WHERE id=$1`, id, next, now)
		if err != nil {
			return generated.CommandAccepted{}, err
		}
	}
	accepted, err := completeOwnerConsoleCommand(ctx, tx, commandID, auditID, principal, "stop_shadow", id, hash, map[string]any{"shadow_session_id": id, "state": next}, now, commandID)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.CommandAccepted{}, err
	}
	return accepted, nil
}

func publicShadowStopTransition(state string) (string, bool) {
	switch state {
	case "QUEUED":
		return "CANCELED", true
	case "PAUSED", "RUNNING":
		return "CANCEL_REQUESTED", true
	case "CANCEL_REQUESTED", "CANCELED", "FAILED":
		return state, true
	default:
		return "", false
	}
}
