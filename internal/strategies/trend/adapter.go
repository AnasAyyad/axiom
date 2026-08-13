package trend

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

// Adapter maps the pure Trend decision contract to the shared strategy execution strategy boundary.
type Adapter struct {
	evaluator  *Evaluator
	mutex      sync.Mutex
	seen       map[string]struct{}
	candidates map[string]Candidate
	decisions  map[uint64]Decision
}

// NewAdapter constructs an idempotent mode-independent strategy execution strategy adapter.
func NewAdapter(evaluator *Evaluator) (*Adapter, error) {
	if evaluator == nil {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	return &Adapter{evaluator: evaluator, seen: make(map[string]struct{}),
		candidates: make(map[string]Candidate), decisions: make(map[uint64]Decision)}, nil
}

// Evaluate decodes immutable canonical input and returns only accepted changes.
func (adapter *Adapter) Evaluate(ctx context.Context, event replay.Event) (backtest.Candidate, error) {
	if ctx == nil || event.Ordinal == 0 || event.LogicalTime == 0 || len(event.Canonical) == 0 {
		return backtest.Candidate{}, trendError(ReasonCandleFinality)
	}
	var input Input
	if err := json.Unmarshal(event.Canonical, &input); err != nil {
		return backtest.Candidate{}, trendError(ReasonCandleFinality)
	}
	if input.Ordinal != event.Ordinal || input.LogicalTime != event.LogicalTime {
		return backtest.Candidate{}, trendError(ReasonCandleOrder)
	}
	decision, err := adapter.evaluator.Evaluate(input)
	if err != nil {
		return backtest.Candidate{}, err
	}
	adapter.mutex.Lock()
	adapter.decisions[event.Ordinal] = decision
	if decision.Candidate == nil {
		adapter.mutex.Unlock()
		return backtest.Candidate{}, trendError(decision.ReasonCode)
	}
	decisionKey := decision.ID.String()
	if _, duplicate := adapter.seen[decisionKey]; duplicate {
		adapter.mutex.Unlock()
		return backtest.Candidate{}, trendError(ReasonDuplicateDecision)
	}
	adapter.seen[decisionKey] = struct{}{}
	adapter.candidates[decisionKey] = *decision.Candidate
	adapter.decisions[event.Ordinal] = decision
	payload, err := adapter.portfolioCandidate(input, decision)
	if err != nil {
		delete(adapter.seen, decisionKey)
		delete(adapter.candidates, decisionKey)
		adapter.mutex.Unlock()
		return backtest.Candidate{}, err
	}
	adapter.mutex.Unlock()
	return backtest.Candidate{Ordinal: event.Ordinal, Payload: payload}, nil
}

// DecisionEvidence returns the exact complete decision associated with a
// canonical input. It is separate from Candidate because a no-action outcome,
// exit cooldown, and position-protection facts are not order fields.
func (adapter *Adapter) DecisionEvidence(event replay.Event) (json.RawMessage, error) {
	if adapter == nil || event.Ordinal == 0 {
		return nil, trendError(ReasonCandleOrder)
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	decision, exists := adapter.decisions[event.Ordinal]
	if !exists || decision.Ordinal != event.Ordinal {
		return nil, trendError(ReasonDuplicateDecision)
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	return payload, nil
}

// Candidate returns a defensive copy of the exact accepted desired change.
func (adapter *Adapter) Candidate(decisionID domain.DecisionID) (Candidate, bool) {
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	candidate, ok := adapter.candidates[decisionID.String()]
	return candidate, ok
}

func (adapter *Adapter) portfolioCandidate(input Input, decision Decision) (json.RawMessage, error) {
	payload, err := adapter.portfolioCandidateValue(input, decision)
	if err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

func (adapter *Adapter) portfolioCandidateValue(input Input, decision Decision) (portfolio.Candidate, error) {
	candidate := decision.Candidate
	funds, liquidity, err := reservationIDs(decision.ID.String())
	if err != nil {
		return portfolio.Candidate{}, err
	}
	score, _ := domain.ParsePnL("0")
	reserved := moneyFromNotional(candidate.Notional)
	if candidate.Side == domain.SideBuy {
		fee, feeErr := domain.CalculateFee(candidate.Notional, input.Sizing.EntryFeeRate, 18)
		if feeErr != nil {
			return portfolio.Candidate{}, trendError(ReasonInvalidSizing)
		}
		reserved, err = reserved.AddFee(fee)
		if err != nil {
			return portfolio.Candidate{}, trendError(ReasonInvalidSizing)
		}
	}
	payload := portfolio.Candidate{ID: decision.ID.Value(), Strategy: portfolio.TrendStrategy,
		Instrument: candidate.Instrument, Side: candidate.Side,
		Quantity: candidate.Quantity, Notional: reserved, Score: score,
		ScoreComponents: []portfolio.ScoreComponent{{Name: "trend_stressed_edge", Value: score}},
		BaseEligibility: input.Evidence.AssetEligibilityVersion, QuoteEligibility: input.Evidence.AssetEligibilityVersion,
		LiquidityDomain: input.Sizing.LiquidityDomain, LiquidityReservation: liquidity,
		FundsReservation: funds, Fence: runtimecore.FencingToken(input.Sizing.FencingToken)}
	return payload, nil
}

func reservationIDs(decisionID string) (domain.ReservationID, domain.ReservationID, error) {
	digest := sha256.Sum256([]byte(decisionID))
	suffix := hex.EncodeToString(digest[:8])
	funds, err := domain.NewReservationID("trend-funds-" + suffix)
	if err != nil {
		return domain.ReservationID{}, domain.ReservationID{}, err
	}
	liquidity, err := domain.NewReservationID("trend-liquidity-" + suffix)
	return funds, liquidity, err
}

func moneyFromNotional(value domain.Notional) domain.Money {
	result, _ := domain.ParseMoney(value.String())
	return result
}

var _ backtest.Strategy = (*Adapter)(nil)
var _ backtest.DecisionEvidenceSource = (*Adapter)(nil)
