package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A11ConsoleStore reads authoritative console projections and owns durable commands.
type A11ConsoleStore struct {
	pool   *pgxpool.Pool
	cursor console.CursorCodec
	clock  domain.Clock
}

// NewA11ConsoleStore constructs the PostgreSQL-backed A11 service boundary.
func NewA11ConsoleStore(pool *pgxpool.Pool, cursorKey []byte, clock domain.Clock) (*A11ConsoleStore, error) {
	if pool == nil || clock == nil {
		return nil, fmt.Errorf("a11_console_dependencies_missing")
	}
	codec, err := console.NewCursorCodec(cursorKey)
	if err != nil {
		return nil, err
	}
	return &A11ConsoleStore{pool: pool, cursor: codec, clock: clock}, nil
}

// SystemStatus returns current durable risk, shadow, and incident state.
func (store *A11ConsoleStore) SystemStatus(ctx context.Context) (generated.SystemStatus, error) {
	var riskState string
	_ = store.pool.QueryRow(ctx, `SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1`).Scan(&riskState)
	if riskState == "" {
		riskState = "PAUSED"
	}
	if riskState == "NORMAL" || riskState == "CAUTIOUS" {
		riskState = "RESUMED"
	}
	var activeID *string
	var shadowState string
	_ = store.pool.QueryRow(ctx, `SELECT id,state FROM shadow_sessions WHERE state IN ('QUEUED','RUNNING','PAUSED','CANCEL_REQUESTED') ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&activeID, &shadowState)
	var incidents int
	if err := store.pool.QueryRow(ctx, `SELECT count(*)::integer FROM incidents WHERE state <> 'resolved' AND severity='critical'`).Scan(&incidents); err != nil {
		return generated.SystemStatus{}, err
	}
	now := store.clock.Now().UTC
	var outboxRevision int64
	_ = store.pool.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM outbox_events`).Scan(&outboxRevision)
	revision := strconv.FormatInt(outboxRevision, 10)
	mode, environment := generated.SystemStatusExecutionMode("shadow"), "production_public"
	strategy := generated.SystemStatusStrategyActivation("unavailable")
	var trendCount int
	_ = store.pool.QueryRow(ctx, `SELECT count(*) FROM strategy_versions WHERE id='trend-v1a-1'`).Scan(&trendCount)
	if trendCount == 1 {
		strategy = generated.SystemStatusStrategyActivation("trend.v1a.1")
	}
	binance, _ := store.BinanceHealth(ctx)
	binanceState, engineState := string(binance.WebsocketState), shadowState
	if engineState == "" {
		engineState = "READY_PAUSED"
	}
	lifecycle := generated.SystemStatusLifecycleState("READY_PAUSED")
	if shadowState == "RUNNING" && riskState == "RESUMED" && binanceState == "healthy" {
		lifecycle = generated.SystemStatusLifecycleState("RUNNING")
	} else if binanceState == "stale" || binanceState == "unavailable" {
		lifecycle = generated.SystemStatusLifecycleState("DEGRADED")
	}
	return generated.SystemStatus{Release: generated.SystemStatusRelease("V1A"), Phase: generated.SystemStatusPhase("A11"), Role: "api",
		LifecycleState: lifecycle, StrategyActivation: strategy,
		RealTradingEnabled: generated.SystemStatusRealTradingEnabled(false), ExecutionMode: &mode, Environment: &environment,
		RiskState: ptr(generated.SystemStatusRiskState(riskState)), CriticalIncidents: &incidents, ActiveResourceId: activeID,
		ServerTime: &now, Revision: &revision, EngineState: &engineState, BinanceState: &binanceState}, nil
}

// BinanceHealth derives a fail-closed public-only view from durable recorder evidence.
func (store *A11ConsoleStore) BinanceHealth(ctx context.Context) (generated.BinanceHealth, error) {
	now := store.clock.Now().UTC
	var ended time.Time
	var revision int64
	err := store.pool.QueryRow(ctx, `SELECT segment.ended_at,segment.last_ordinal FROM market_data_segments segment JOIN exchanges exchange ON exchange.id=segment.exchange_id WHERE exchange.id='binance' AND exchange.environment='production_public' AND segment.state='ready' ORDER BY segment.ended_at DESC,segment.id DESC LIMIT 1`).Scan(&ended, &revision)
	websocketState, bookState := generated.BinanceHealthWebsocketState("stale"), generated.BinanceHealthBookState("stale")
	recorderState := generated.BinanceHealthRecorderState("unavailable")
	if err == nil {
		recorderState = generated.BinanceHealthRecorderState("healthy")
		if now.Sub(ended.UTC()) <= 5*time.Minute {
			websocketState, bookState = generated.BinanceHealthWebsocketState("healthy"), generated.BinanceHealthBookState("healthy")
		} else {
			recorderState = generated.BinanceHealthRecorderState("degraded")
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return generated.BinanceHealth{}, err
	}
	capabilities := []string{"public_metadata", "public_server_time", "public_trades", "public_candles", "public_order_book"}
	return generated.BinanceHealth{Environment: generated.ProductionPublic, PublicOnly: generated.BinanceHealthPublicOnlyTrue,
		Capabilities: &capabilities, WebsocketState: websocketState, BookState: bookState, RecorderState: recorderState,
		ObservedAt: now, Revision: strconv.FormatInt(revision, 10)}, nil
}

// Risk returns the latest global policy and state without inventing recovery readiness.
func (store *A11ConsoleStore) Risk(ctx context.Context) (generated.RiskStatus, error) {
	var state, reason string
	var occurred time.Time
	err := store.pool.QueryRow(ctx, `SELECT next_state,reason_code,occurred_at FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1`).Scan(&state, &reason, &occurred)
	if errors.Is(err, pgx.ErrNoRows) {
		state, reason = "PAUSED", "startup_paused"
	} else if err != nil {
		return generated.RiskStatus{}, err
	}
	var revision, policyVersion int64
	if err = store.pool.QueryRow(ctx, `SELECT revision,updated_at FROM api_entity_revisions WHERE entity_type='risk' AND entity_id='global'`).Scan(&revision, &occurred); err != nil {
		return generated.RiskStatus{}, err
	}
	_ = store.pool.QueryRow(ctx, `SELECT version FROM risk_policies WHERE scope_kind='global' ORDER BY version DESC LIMIT 1`).Scan(&policyVersion)
	var critical, blockers int
	if err = store.pool.QueryRow(ctx, `SELECT
      (SELECT count(*)::integer FROM incidents WHERE state<>'resolved' AND severity='critical'),
      (SELECT count(*)::integer FROM reconciliation_cases WHERE state IN ('open','quarantined'))+
      (SELECT count(*)::integer FROM quarantined_scopes WHERE released_at IS NULL)+
      (SELECT count(*)::integer FROM orders WHERE state='unknown')+
      CASE WHEN NOT EXISTS (
        SELECT 1 FROM startup_recovery_attempts attempt
        WHERE attempt.state='ready_paused' AND
          (SELECT count(*) FROM startup_recovery_evidence evidence WHERE evidence.attempt_id=attempt.id)=14
      ) THEN 1 ELSE 0 END+
      CASE WHEN NOT EXISTS (
        SELECT 1 FROM market_data_segments segment JOIN exchanges exchange ON exchange.id=segment.exchange_id
        WHERE exchange.id='binance' AND exchange.environment='production_public' AND segment.state='ready' AND segment.ended_at >= $1
      ) THEN 1 ELSE 0 END`, store.clock.Now().UTC.Add(-5*time.Minute)).Scan(&critical, &blockers); err != nil {
		return generated.RiskStatus{}, err
	}
	reasons := []string{reason}
	return generated.RiskStatus{State: generated.RiskStatusState(state), PolicyVersion: strconv.FormatInt(policyVersion, 10),
		Revision: strconv.FormatInt(revision, 10), UpdatedAt: occurred.UTC(), ReasonCodes: &reasons,
		Contributors: []generated.RiskContributor{}, RecoveryReady: state == "PAUSED" && critical == 0 && blockers == 0, UnresolvedCritical: &critical}, nil
}

// Trend returns the latest immutable Trend definition and parameters.
func (store *A11ConsoleStore) Trend(ctx context.Context) (generated.TrendStatus, error) {
	var id, promotion string
	var version int64
	err := store.pool.QueryRow(ctx, `SELECT sv.id,sv.version,sv.promotion_status FROM strategy_versions sv JOIN strategy_definitions sd ON sd.id=sv.strategy_id WHERE sd.name='trend' ORDER BY sv.version DESC LIMIT 1`).Scan(&id, &version, &promotion)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.TrendStatus{}, console.ErrNotFound
	}
	if err != nil {
		return generated.TrendStatus{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT parameter_name,decimal_value,unit,coalesce(cadence,''),coalesce(mutability,'immutable_per_run') FROM strategy_parameters WHERE strategy_version_id=$1 ORDER BY parameter_name`, id)
	if err != nil {
		return generated.TrendStatus{}, err
	}
	defer rows.Close()
	parameters := make([]generated.TrendParameter, 0)
	for rows.Next() {
		var item generated.TrendParameter
		var mutability string
		if err = rows.Scan(&item.Id, &item.Value, &item.Unit, &item.Cadence, &mutability); err != nil {
			return generated.TrendStatus{}, err
		}
		item.Mutability = generated.TrendParameterMutability(mutability)
		parameters = append(parameters, item)
	}
	_ = promotion
	return generated.TrendStatus{Version: generated.TrendV1a1, Revision: strconv.FormatInt(version, 10),
		Timeframe: generated.N4h, Health: generated.Paused, Parameters: parameters,
		EvidenceMaturity: generated.TrendStatusEvidenceMaturityLocalTierB,
		Viability:        ptr(generated.TrendStatusViabilityUndetermined)}, rows.Err()
}

func ptr[T any](value T) *T { return &value }

var _ console.ReadService = (*A11ConsoleStore)(nil)
