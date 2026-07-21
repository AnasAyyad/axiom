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
func (store *A11ConsoleStore) CreateShadow(ctx context.Context, principal authentication.Principal, key string, body generated.ShadowSessionRequest) (generated.ShadowSessionResource, error) {
	_, hash, err := a11CommandPayload(body)
	if err != nil {
		return generated.ShadowSessionResource{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	dedupe := a11Dedupe(principal.UserID, key)
	var existingID, existingHash string
	err = tx.QueryRow(ctx, `SELECT ss.id,cr.payload_hash FROM shadow_sessions ss JOIN command_requests cr ON cr.id=ss.command_id WHERE cr.deduplication_key=$1`, dedupe).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != hash {
			return generated.ShadowSessionResource{}, console.ErrIdempotencyConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return generated.ShadowSessionResource{}, err
		}
		return store.Shadow(ctx, existingID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return generated.ShadowSessionResource{}, err
	}
	strategyVersionID := a11StrategyVersionID(string(body.StrategyVersion))
	if ready, checkErr := store.a11ShadowReady(ctx, tx, body, strategyVersionID); checkErr != nil || !ready {
		if checkErr != nil {
			return generated.ShadowSessionResource{}, checkErr
		}
		return generated.ShadowSessionResource{}, console.ErrPrecondition
	}
	now := store.clock.Now().UTC
	sessionID, _ := a11Identifier("shadow")
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	if err = insertA11Command(ctx, tx, commandID, principal, key, hash, "create_shadow", "shadow_session", sessionID, "start production-public simulation", now, auditID, commandID); err != nil {
		return generated.ShadowSessionResource{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO shadow_sessions(id,command_id,state,revision,public_exchange,simulation_only,entries_enabled,portfolio_id,configuration_id,strategy_version_id,created_at,exchange_id) VALUES($1,$2,'QUEUED',1,'binance-production-public',true,false,$3,$4,$5,$6,'binance')`, sessionID, commandID, body.PortfolioId, body.ConfigurationId, strategyVersionID, now); err != nil {
		return generated.ShadowSessionResource{}, a11ConstraintError(err)
	}
	if _, err = completeA11Command(ctx, tx, commandID, auditID, principal, "create_shadow", sessionID, hash, map[string]any{"shadow_session_id": sessionID, "state": "QUEUED", "simulation_only": true, "public_only": true}, now, commandID); err != nil {
		return generated.ShadowSessionResource{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.ShadowSessionResource{}, err
	}
	return store.Shadow(ctx, sessionID)
}

func (store *A11ConsoleStore) a11ShadowReady(ctx context.Context, tx pgx.Tx, body generated.ShadowSessionRequest, strategyVersionID string) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM portfolios WHERE id=$1) AND
		EXISTS(SELECT 1 FROM configuration_versions WHERE id=$2) AND
		EXISTS(SELECT 1 FROM strategy_versions WHERE id=$3) AND
		EXISTS(SELECT 1 FROM instrument_metadata_versions metadata JOIN exchanges exchange ON exchange.id=metadata.exchange_id
		  WHERE exchange.id='binance' AND exchange.environment='production_public') AND
		EXISTS(SELECT 1 FROM market_data_segments segment JOIN exchanges exchange ON exchange.id=segment.exchange_id
		  WHERE exchange.id='binance' AND exchange.environment='production_public' AND segment.state='ready'
		    AND segment.event_type IN ('candle','mixed_public') AND segment.ended_at >= $4) AND
		EXISTS(SELECT 1 FROM startup_recovery_attempts attempt WHERE attempt.state='ready_paused' AND
		  (SELECT count(*) FROM startup_recovery_evidence evidence WHERE evidence.attempt_id=attempt.id)=14) AND
		NOT EXISTS(SELECT 1 FROM circuit_breaker_events WHERE breaker_kind='disk_failure')`,
		body.PortfolioId, body.ConfigurationId, strategyVersionID, store.clock.Now().UTC.Add(-5*time.Hour)).Scan(&ready)
	return ready, err
}

// StopShadow records and applies an idempotent graceful stop request.
func (store *A11ConsoleStore) StopShadow(ctx context.Context, principal authentication.Principal, id, key string, body generated.RevisionCommandRequest) (generated.CommandAccepted, error) {
	_, hash, err := a11CommandPayload(map[string]any{"id": id, "body": body})
	if err != nil || body.Reason == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if existing, found, lookupErr := lookupA11Command(ctx, tx, principal.UserID, key, hash); lookupErr != nil {
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
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	if err = insertA11Command(ctx, tx, commandID, principal, key, hash, "stop_shadow", "shadow_session", id, body.Reason, now, auditID, commandID); err != nil {
		return generated.CommandAccepted{}, err
	}
	next, valid := a11ShadowStopTransition(state)
	if !valid {
		return generated.CommandAccepted{}, console.ErrConflict
	}
	if next != state {
		_, err = tx.Exec(ctx, `UPDATE shadow_sessions SET state=$2,revision=revision+1,entries_enabled=false,stopped_at=CASE WHEN $2='CANCELED' THEN $3 ELSE stopped_at END WHERE id=$1`, id, next, now)
		if err != nil {
			return generated.CommandAccepted{}, err
		}
	}
	accepted, err := completeA11Command(ctx, tx, commandID, auditID, principal, "stop_shadow", id, hash, map[string]any{"shadow_session_id": id, "state": next}, now, commandID)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.CommandAccepted{}, err
	}
	return accepted, nil
}

func a11ShadowStopTransition(state string) (string, bool) {
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
