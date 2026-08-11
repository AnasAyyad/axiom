package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/replay"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"

	"github.com/jackc/pgx/v5"
)

func observeCombinedShadowBook(processor *evaluationMarketProcessor, event replay.Event) error {
	var stream exchangecontracts.StreamEvent
	if json.Unmarshal(event.Canonical, &stream) == nil && stream.Kind != "" {
		if stream.Snapshot != nil {
			return processor.replaceBook(*stream.Snapshot, event.Ordinal, event.LogicalTime)
		}
		if stream.Depth != nil {
			return processor.applyDepth(*stream.Depth, event.Ordinal, event.LogicalTime)
		}
		return nil
	}
	var snapshot exchangecontracts.BookSnapshot
	if json.Unmarshal(event.Canonical, &snapshot) == nil && snapshot.Exchange != "" && snapshot.Instrument.Base != "" {
		return processor.replaceBook(snapshot, event.Ordinal, event.LogicalTime)
	}
	return nil
}

func (engine *evaluationCombinedShadowEngine) observeCombinedFills(runtime *evaluationCombinedShadowRuntime,
	member *evaluationCombinedShadowMember, event replay.Event, result backtest.EventResult,
	consumption map[string]domain.Quantity) error {
	var orders []execution.Order
	if len(result.Orders) == 0 {
		return fmt.Errorf("evaluation_shadow_orders_invalid")
	}
	if json.Unmarshal(result.Orders, &orders) == nil {
		return observeCombinedOrders(runtime, member.id, event, orders, consumption)
	}
	results, canonicalHash, err := combinedShadowMultilegResults(member.strategy, result.ExecutionEvents)
	if err != nil {
		return err
	}
	for index, value := range results {
		fillID := fmt.Sprintf("%s:%s:%d", canonicalHash, member.id, index)
		if err = consumeCombinedArbitrageFill(runtime, member.id, fillID, value, consumption); err != nil {
			return err
		}
	}
	return nil
}

func observeCombinedOrders(runtime *evaluationCombinedShadowRuntime, memberID string, event replay.Event,
	orders []execution.Order, consumption map[string]domain.Quantity) error {
	for _, order := range orders {
		if len(order.Fills) == 0 {
			continue
		}
		exchange, err := combinedShadowOrderExchange(event)
		if err != nil {
			return err
		}
		key := combinedShadowLiquidityKey(exchange, order.Identity.Instrument, order.Identity.Side)
		quantity := consumption[key]
		if quantity.String() == "" {
			quantity, _ = domain.ParseQuantity("0")
		}
		for _, fill := range order.Fills {
			owner, duplicate := runtime.seenFills[fill.ID.String()]
			if duplicate && owner != memberID {
				return fmt.Errorf("evaluation_shadow_duplicate_fill")
			}
			if duplicate {
				continue
			}
			quantity, err = quantity.Add(fill.Quantity)
			if err != nil {
				return fmt.Errorf("evaluation_shadow_liquidity_overflow")
			}
			runtime.seenFills[fill.ID.String()] = memberID
		}
		consumption[key] = quantity
	}
	return nil
}

func combinedShadowMultilegResults(strategy evaluation.Strategy,
	payload json.RawMessage) ([]arbitrage.Result, string, error) {
	results := make([]arbitrage.Result, 0, 4)
	canonicalHash := ""
	switch strategy {
	case evaluation.StrategyTriangular:
		var simulation triangular.SimulationResult
		if json.Unmarshal(payload, &simulation) != nil {
			return nil, "", fmt.Errorf("evaluation_shadow_multileg_evidence_invalid")
		}
		results = append(results, simulation.Legs...)
		if simulation.Recovery.Leg != nil {
			results = append(results, *simulation.Recovery.Leg)
		}
		canonicalHash = simulation.CanonicalHash
	case evaluation.StrategyCross:
		var simulation crossarb.SimulationResult
		if json.Unmarshal(payload, &simulation) != nil {
			return nil, "", fmt.Errorf("evaluation_shadow_multileg_evidence_invalid")
		}
		if simulation.ActualBuy != nil {
			results = append(results, *simulation.ActualBuy)
		}
		if simulation.ActualSell != nil {
			results = append(results, *simulation.ActualSell)
		}
		canonicalHash = simulation.CanonicalHash
	default:
		return nil, "", fmt.Errorf("evaluation_shadow_orders_invalid")
	}
	return results, canonicalHash, nil
}

func consumeCombinedArbitrageFill(runtime *evaluationCombinedShadowRuntime, memberID, fillID string,
	result arbitrage.Result, consumption map[string]domain.Quantity) error {
	if fillID == "" || result.Instrument.Base == "" || !evaluationShadowExchange(result.Exchange) {
		return fmt.Errorf("evaluation_shadow_multileg_fill_invalid")
	}
	if owner, duplicate := runtime.seenFills[fillID]; duplicate {
		if owner != memberID {
			return fmt.Errorf("evaluation_shadow_duplicate_fill")
		}
		return nil
	}
	key := combinedShadowLiquidityKey(result.Exchange, result.Instrument, result.Side)
	quantity := consumption[key]
	if quantity.String() == "" {
		quantity, _ = domain.ParseQuantity("0")
	}
	quantity, err := quantity.Add(result.TradeQuantity)
	if err != nil {
		return fmt.Errorf("evaluation_shadow_liquidity_overflow")
	}
	consumption[key] = quantity
	runtime.seenFills[fillID] = memberID
	return nil
}

func validateCombinedLiquidity(processor *evaluationMarketProcessor,
	consumption map[string]domain.Quantity) error {
	zero, _ := domain.ParseQuantity("0")
	for key, used := range consumption {
		parts := strings.Split(key, ":")
		if len(parts) != 3 || !evaluationShadowExchange(parts[0]) || parts[1] == "" ||
			(parts[2] != string(domain.SideBuy) && parts[2] != string(domain.SideSell)) {
			return fmt.Errorf("evaluation_shadow_liquidity_key_invalid")
		}
		if used.Compare(zero) <= 0 {
			continue
		}
		available := zero
		for _, book := range processor.books {
			if !book.valid || book.exchange != parts[0] || book.instrument.Symbol() != parts[1] {
				continue
			}
			bid, ask, ok := book.best()
			if !ok {
				continue
			}
			level := ask
			if parts[2] == string(domain.SideSell) {
				level = bid
			}
			var err error
			available, err = available.Add(level.Quantity)
			if err != nil {
				return fmt.Errorf("evaluation_shadow_liquidity_overflow")
			}
		}
		if available.Compare(used) < 0 {
			return fmt.Errorf("evaluation_shadow_shared_liquidity_exceeded")
		}
	}
	return nil
}

func combinedShadowOrderExchange(event replay.Event) (string, error) {
	var candle exchangecontracts.Candle
	if json.Unmarshal(event.Canonical, &candle) == nil && candle.Interval != "" &&
		candle.Instrument.Base != "" && evaluationShadowExchange(string(candle.Exchange)) {
		return string(candle.Exchange), nil
	}
	var stream exchangecontracts.StreamEvent
	if json.Unmarshal(event.Canonical, &stream) == nil && stream.Candle != nil &&
		stream.Candle.Interval != "" && stream.Candle.Instrument.Base != "" &&
		evaluationShadowExchange(string(stream.Candle.Exchange)) {
		return string(stream.Candle.Exchange), nil
	}
	return "", fmt.Errorf("evaluation_shadow_fill_exchange_invalid")
}

func combinedShadowLiquidityKey(exchange string, instrument domain.Instrument, side domain.Side) string {
	return exchange + ":" + instrument.Symbol() + ":" + string(side)
}

func evaluationShadowExchange(exchange string) bool {
	return exchange == "binance" || exchange == "bybit"
}

func (engine *evaluationCombinedShadowEngine) persistDecision(ctx context.Context, campaignID string,
	member *evaluationCombinedShadowMember, event replay.Event, result backtest.EventResult) error {
	payload, err := json.Marshal(map[string]any{"decision": result.Decision, "orders": result.Orders,
		"execution_events": result.ExecutionEvents, "balances": result.Balances, "simulation_only": true})
	if err != nil {
		return err
	}
	decisionHash := sha256.Sum256(result.Decision)
	resultHash := sha256.Sum256(payload)
	occurredAt := engine.clock.Now().UTC
	if event.LogicalTime > 0 && event.LogicalTime <= uint64(^uint64(0)>>1) {
		candidate := time.Unix(0, int64(event.LogicalTime)).UTC()
		if candidate.Year() >= 2020 && candidate.Year() <= 2100 {
			occurredAt = candidate
		}
	}
	tag, err := engine.pool.Exec(ctx, `INSERT INTO evaluation_shadow_decisions(campaign_id,member_id,
input_ordinal,strategy_id,decision_hash,result_hash,canonical_payload,occurred_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (campaign_id,member_id,input_ordinal) DO NOTHING`,
		campaignID, member.id, int64(event.Ordinal), string(member.strategy), decisionHash[:], resultHash[:], payload, occurredAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var priorDecision, priorResult []byte
		if err = engine.pool.QueryRow(ctx, `SELECT decision_hash,result_hash FROM evaluation_shadow_decisions
WHERE campaign_id=$1 AND member_id=$2 AND input_ordinal=$3`, campaignID, member.id,
			int64(event.Ordinal)).Scan(&priorDecision, &priorResult); err != nil ||
			!equalBytes(priorDecision, decisionHash[:]) || !equalBytes(priorResult, resultHash[:]) {
			return fmt.Errorf("evaluation_shadow_determinism_conflict")
		}
	}
	return nil
}

func (engine *evaluationCombinedShadowEngine) persistRuntime(ctx context.Context, campaignID string,
	runtime *evaluationCombinedShadowRuntime, inputHash [32]byte) error {
	now := engine.clock.Now().UTC
	memberSnapshots, activeExposure, err := engine.persistShadowMembers(ctx, campaignID, runtime, now)
	if err != nil {
		return err
	}
	if activeExposure.Cmp(big.NewRat(8_000, 1)) > 0 {
		return fmt.Errorf("evaluation_shadow_shared_capital_exceeded")
	}
	sort.Slice(memberSnapshots, func(left, right int) bool {
		return memberSnapshots[left]["member_id"].(string) < memberSnapshots[right]["member_id"].(string)
	})
	checkpoint, err := json.Marshal(map[string]any{"schema_version": "axiom.evaluation-shadow-checkpoint.v1",
		"campaign_id": campaignID, "last_processed_ordinal": runtime.lastOrdinal,
		"protected_reserve_micros": int64(2_000_000_000), "member_ceiling_micros": int64(2_000_000_000),
		"members": memberSnapshots, "input_manifest_hash": hex.EncodeToString(inputHash[:]),
		"simulation_only": true})
	if err != nil {
		return err
	}
	return engine.persistShadowCheckpoint(ctx, campaignID, runtime.lastOrdinal, checkpoint, inputHash, now)
}

func (engine *evaluationCombinedShadowEngine) persistShadowMembers(ctx context.Context, campaignID string,
	runtime *evaluationCombinedShadowRuntime, now time.Time) ([]map[string]any, *big.Rat, error) {
	memberSnapshots := make([]map[string]any, 0, len(runtime.members))
	activeExposure := new(big.Rat)
	for _, member := range runtime.members {
		if member.failed {
			continue
		}
		snapshot, exposure, err := engine.persistShadowMember(ctx, campaignID, runtime.lastOrdinal, member, now)
		if err != nil {
			return nil, nil, err
		}
		activeExposure.Add(activeExposure, exposure)
		memberSnapshots = append(memberSnapshots, snapshot)
	}
	return memberSnapshots, activeExposure, nil
}

func (engine *evaluationCombinedShadowEngine) persistShadowMember(ctx context.Context, campaignID string,
	lastOrdinal uint64, member *evaluationCombinedShadowMember, now time.Time) (map[string]any, *big.Rat, error) {
	metrics := member.processor.Metrics()
	metricsPayload, err := json.Marshal(metrics)
	if err != nil {
		return nil, nil, err
	}
	exposureValue := metrics.ByStrategy["maximum_exposure"]
	if exposureValue == "" {
		exposureValue = metrics.Exposure
	}
	exposure, ok := new(big.Rat).SetString(exposureValue)
	if !ok || exposure.Sign() < 0 || exposure.Cmp(big.NewRat(2_000, 1)) > 0 {
		return nil, nil, fmt.Errorf("evaluation_shadow_member_ceiling_exceeded")
	}
	resultPayload, _ := json.Marshal(map[string]any{"member_id": member.id,
		"last_processed_ordinal": lastOrdinal, "metrics": metrics})
	resultHash := sha256.Sum256(resultPayload)
	if _, err = engine.pool.Exec(ctx, `UPDATE evaluation_shadow_member_checkpoints SET state='RUNNING',
last_processed_ordinal=$3,metrics_payload=$4,result_hash=$5,updated_at=$6
WHERE campaign_id=$1 AND member_id=$2 AND state='RUNNING'`, campaignID, member.id,
		int64(lastOrdinal), metricsPayload, resultHash[:], now); err != nil {
		return nil, nil, err
	}
	if _, err = engine.pool.Exec(ctx, `UPDATE evaluation_campaign_members SET state='RUNNING',
metrics_payload=$3,result_hash=$4,linked_run_id=$5,updated_at=$6
WHERE campaign_id=$1 AND id=$2 AND mode='shadow'`, campaignID, member.id, metricsPayload,
		resultHash[:], "combined-shadow:"+campaignID, now); err != nil {
		return nil, nil, err
	}
	snapshot := map[string]any{"member_id": member.id, "strategy_id": member.strategy,
		"result_hash": hex.EncodeToString(resultHash[:]), "metrics": metrics}
	return snapshot, exposure, nil
}

func (engine *evaluationCombinedShadowEngine) persistShadowCheckpoint(ctx context.Context, campaignID string,
	lastOrdinal uint64, checkpoint []byte, inputHash [32]byte, now time.Time) error {
	checkpointHash := sha256.Sum256(checkpoint)
	var valid int64
	if err := engine.pool.QueryRow(ctx, `SELECT valid_shadow_seconds FROM evaluation_campaigns WHERE id=$1`,
		campaignID).Scan(&valid); err != nil {
		return err
	}
	_, err := engine.pool.Exec(ctx, `UPDATE evaluation_shadow_sessions SET last_processed_ordinal=$2,
valid_seconds=$3,checkpoint_payload=$4,checkpoint_hash=$5,input_manifest_hash=$6,updated_at=$7
WHERE campaign_id=$1 AND state IN ('RUNNING','PAUSED_RECOVERABLE')`, campaignID,
		int64(lastOrdinal), valid, checkpoint, checkpointHash[:], inputHash[:], now)
	return err
}

func (engine *evaluationCombinedShadowEngine) failMember(ctx context.Context, campaignID, memberID string,
	cause error) error {
	reason := "STRATEGY_RUNTIME_FAILED"
	if cause == nil {
		reason = "STRATEGY_RUNTIME_INVALID"
	}
	now := engine.clock.Now().UTC
	tx, err := engine.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `UPDATE evaluation_shadow_member_checkpoints SET state='FAILED',reason_code=$3,
updated_at=$4 WHERE campaign_id=$1 AND member_id=$2 AND state='RUNNING'`, campaignID, memberID,
		reason, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE evaluation_campaign_members SET state='FAILED',verdict='BLOCKED',
reason_code=$3,updated_at=$4 WHERE campaign_id=$1 AND id=$2 AND mode='shadow'`, campaignID, memberID,
		reason, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
