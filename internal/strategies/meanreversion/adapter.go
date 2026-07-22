package meanreversion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	runtimecore "axiom/internal/runtime"
)

// Adapter maps pure B3 decisions to the shared strategy boundary.
type Adapter struct {
	evaluator  *Evaluator
	mutex      sync.Mutex
	seen       map[string]struct{}
	candidates map[string]Candidate
}

// NewAdapter constructs an idempotent mode-independent adapter.
func NewAdapter(evaluator *Evaluator) (*Adapter, error) {
	if evaluator == nil {
		return nil, strategyError(ReasonInvalidConfiguration)
	}
	return &Adapter{evaluator: evaluator, seen: make(map[string]struct{}),
		candidates: make(map[string]Candidate)}, nil
}

// Evaluate decodes immutable canonical input and returns only accepted changes.
func (adapter *Adapter) Evaluate(ctx context.Context, event replay.Event) (backtest.Candidate, error) {
	if ctx == nil || event.Ordinal == 0 || event.LogicalTime == 0 || len(event.Canonical) == 0 {
		return backtest.Candidate{}, strategyError(ReasonCandleFinality)
	}
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil || input.Ordinal != event.Ordinal ||
		input.LogicalTime != event.LogicalTime {
		return backtest.Candidate{}, strategyError(ReasonCandleOrder)
	}
	decision, err := adapter.evaluator.Evaluate(input)
	if err != nil {
		return backtest.Candidate{}, err
	}
	if decision.Candidate == nil {
		return backtest.Candidate{}, strategyError(decision.ReasonCode)
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	key := decision.ID.String()
	if _, duplicate := adapter.seen[key]; duplicate {
		return backtest.Candidate{}, strategyError(ReasonDuplicateDecision)
	}
	adapter.seen[key] = struct{}{}
	adapter.candidates[key] = *decision.Candidate
	payload, err := adapter.portfolioCandidate(input, decision)
	if err != nil {
		delete(adapter.seen, key)
		delete(adapter.candidates, key)
		return backtest.Candidate{}, err
	}
	return backtest.Candidate{Ordinal: event.Ordinal, Payload: payload}, nil
}

// Candidate returns the exact accepted desired change for planning.
func (adapter *Adapter) Candidate(decisionID domain.DecisionID) (Candidate, bool) {
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	candidate, ok := adapter.candidates[decisionID.String()]
	return candidate, ok
}

func (adapter *Adapter) portfolioCandidate(input Input, decision Decision) (json.RawMessage, error) {
	candidate := decision.Candidate
	funds, liquidity, err := reservationIDs(decision.ID.String())
	if err != nil {
		return nil, err
	}
	score, _ := domain.ParsePnL("0")
	reserved := moneyFromNotional(candidate.Notional)
	if candidate.Side == domain.SideBuy {
		fee, feeErr := domain.CalculateFee(candidate.Notional, input.Sizing.EntryFeeRate, 18)
		if feeErr != nil {
			return nil, strategyError(ReasonInvalidSizing)
		}
		reserved, err = reserved.AddFee(fee)
		if err != nil {
			return nil, strategyError(ReasonInvalidSizing)
		}
	}
	payload := portfolio.Candidate{ID: decision.ID.Value(), Strategy: portfolio.V1BMeanReversionStrategy,
		Instrument: candidate.Instrument, Side: candidate.Side, Quantity: candidate.Quantity, Notional: reserved,
		Score: score, ScoreComponents: []portfolio.ScoreComponent{{Name: "mean_reversion_stressed_deviation", Value: score}},
		BaseEligibility: input.Evidence.AssetEligibilityVersion, QuoteEligibility: input.Evidence.AssetEligibilityVersion,
		LiquidityDomain: input.Sizing.LiquidityDomain, LiquidityReservation: liquidity,
		FundsReservation: funds, Fence: runtimecore.FencingToken(input.Sizing.FencingToken)}
	return json.Marshal(payload)
}

func reservationIDs(decisionID string) (domain.ReservationID, domain.ReservationID, error) {
	digest := sha256.Sum256([]byte(decisionID))
	suffix := hex.EncodeToString(digest[:8])
	funds, err := domain.NewReservationID("mean-reversion-funds-" + suffix)
	if err != nil {
		return domain.ReservationID{}, domain.ReservationID{}, err
	}
	liquidity, err := domain.NewReservationID("mean-reversion-liquidity-" + suffix)
	return funds, liquidity, err
}

func moneyFromNotional(value domain.Notional) domain.Money {
	result, _ := domain.ParseMoney(value.String())
	return result
}

var _ backtest.Strategy = (*Adapter)(nil)
