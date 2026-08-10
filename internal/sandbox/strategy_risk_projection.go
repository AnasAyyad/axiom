package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
)

// Durable strategy-risk projection purposes and accounting states.
const (
	StrategyRiskValuationBaseline  = "baseline"
	StrategyRiskValuationEvaluated = "evaluated"
	StrategyRiskAccountingNoFills  = "no_fills"
	StrategyRiskAccountingComplete = "complete"
)

// StrategyRiskValuation is the exact, immutable valuation evidence behind one
// automatic sandbox central-risk observation. Functional-currency values are
// derived from an exchange-authoritative account snapshot, a liquidation-side
// public mark, and the journal-rebuildable strategy accounting projection.
type StrategyRiskValuation struct {
	Purpose                     string
	StrategySessionID           SessionID
	StrategyRevision            uint64
	AccountID                   AccountID
	AccountEpoch                uint64
	Instrument                  string
	SnapshotHash                string
	MarketHash                  string
	PolicyID                    string
	PolicyVersion               uint64
	PolicyHash                  string
	AccountingState             string
	AccountingEvidenceHash      string
	AccountingProjectionHash    string
	MarkPrice                   domain.Price
	AccountEquity               domain.Money
	VolatileAssetValue          domain.Money
	CombinedVolatileValue       domain.Money
	CommittedBuyValue           domain.Money
	ExchangeRiskValue           domain.Money
	ReserveValue                domain.Money
	ReservedValue               domain.Money
	StrategyPositionQuantity    domain.Balance
	StrategyPositionValue       domain.Money
	StrategyTotalCost           domain.Money
	StrategyRealizedPnL         domain.PnL
	StrategyUnrealizedPnL       domain.PnL
	StrategyTotalPnL            domain.PnL
	AccountPeakEquity           domain.Money
	UTCDayBaselineEquity        domain.Money
	Rolling24HourBaselineEquity domain.Money
	StrategyPeakPnL             domain.PnL
	OpenOrders                  uint32
	Slippage                    domain.Percent
	ReconciliationID            string
	ReconciliationHash          string
	StorageRevision             uint64
	StorageObservedAt           time.Time
	EngineStartupCycle          uint64
	AdmissionHash               string
	ObservedAt                  time.Time
}

// StrategyRiskObservationProjector creates and persists an observation under
// the exact engine fence. Implementations may return an unavailable error after
// durably initializing the first valuation baseline; callers must wait.
type StrategyRiskObservationProjector interface {
	ProjectStrategyRiskObservation(
		context.Context,
		StrategySessionExecutionLease,
		StrategySessionAdmission,
		AccountSnapshot,
		StrategyMarketInput,
		StrategyRiskFacts,
		time.Time,
	) (StrategyRiskObservation, error)
}

// ValidFor binds every exact valuation and operational reference to the same
// decision instant. It rejects unsupported optimistic zero states.
func (valuation StrategyRiskValuation) ValidFor(
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	admission StrategySessionAdmission,
	now time.Time,
) error {
	zeros := newStrategyRiskZeros()
	if !valuation.validIdentity(work, snapshot, market, facts, admission, now) ||
		!valuation.validAmounts(zeros) || !valuation.validProvenance(admission, now) {
		return contractError("strategy_risk_valuation_invalid")
	}
	expectedExchange, err := valuation.CombinedVolatileValue.Add(valuation.CommittedBuyValue)
	if err != nil || expectedExchange.Compare(valuation.ExchangeRiskValue) != 0 {
		return contractError("strategy_risk_valuation_invalid")
	}
	switch valuation.Purpose {
	case StrategyRiskValuationBaseline, StrategyRiskValuationEvaluated:
	default:
		return contractError("strategy_risk_valuation_invalid")
	}
	switch valuation.AccountingState {
	case StrategyRiskAccountingNoFills:
		if !valuation.validEmptyAccounting(zeros) {
			return contractError("strategy_risk_valuation_invalid")
		}
	case StrategyRiskAccountingComplete:
		if !strategyRiskHash(valuation.AccountingProjectionHash) ||
			valuation.AccountingProjectionHash != valuation.AccountingEvidenceHash {
			return contractError("strategy_risk_valuation_invalid")
		}
	default:
		return contractError("strategy_risk_valuation_invalid")
	}
	return nil
}

type strategyRiskZeros struct {
	money   domain.Money
	balance domain.Balance
	pnl     domain.PnL
	percent domain.Percent
	price   domain.Price
}

func newStrategyRiskZeros() strategyRiskZeros {
	money, _ := domain.ParseMoney("0")
	balance, _ := domain.ParseBalance("0")
	pnl, _ := domain.ParsePnL("0")
	percent, _ := domain.ParsePercent("0")
	price, _ := domain.ParsePrice("0")
	return strategyRiskZeros{money: money, balance: balance, pnl: pnl, percent: percent, price: price}
}

func (valuation StrategyRiskValuation) validIdentity(work StrategySessionWork, snapshot AccountSnapshot,
	market StrategyMarketInput, facts StrategyRiskFacts, admission StrategySessionAdmission, now time.Time,
) bool {
	return work.ValidAt(now) == nil && snapshot.Validate() == nil &&
		facts.ValidFor(work, snapshot, now) == nil && admission.Valid() == nil &&
		admission.Work == work && admission.ApprovedAt.Equal(now) &&
		valuation.StrategySessionID == work.SessionID && valuation.StrategyRevision == work.StrategyRevision &&
		valuation.AccountID == work.Account.ID && valuation.AccountEpoch == work.Account.Epoch &&
		valuation.Instrument == work.Instrument && valuation.SnapshotHash == snapshot.SnapshotHash &&
		valuation.MarketHash == StrategyMarketEvidenceHash(market) &&
		valuation.PolicyID == facts.PolicyID && valuation.PolicyVersion == facts.PolicyVersion &&
		valuation.PolicyHash == facts.PolicyHash
}

func (valuation StrategyRiskValuation) validAmounts(zero strategyRiskZeros) bool {
	pnlTotal, err := valuation.StrategyRealizedPnL.Add(valuation.StrategyUnrealizedPnL)
	return strategyRiskHash(valuation.AccountingEvidenceHash) && valuation.MarkPrice.Compare(zero.price) > 0 &&
		valuation.AccountEquity.Compare(zero.money) > 0 && valuation.VolatileAssetValue.Compare(zero.money) >= 0 &&
		valuation.CombinedVolatileValue.Compare(valuation.VolatileAssetValue) >= 0 &&
		valuation.CommittedBuyValue.Compare(zero.money) >= 0 &&
		valuation.ExchangeRiskValue.Compare(valuation.CombinedVolatileValue) >= 0 &&
		valuation.ReserveValue.Compare(valuation.AccountEquity) <= 0 &&
		valuation.ReservedValue.Compare(valuation.AccountEquity) <= 0 &&
		valuation.StrategyPositionQuantity.Compare(zero.balance) >= 0 &&
		valuation.StrategyPositionValue.Compare(valuation.VolatileAssetValue) <= 0 &&
		valuation.StrategyTotalCost.Compare(zero.money) >= 0 && err == nil &&
		pnlTotal.Compare(valuation.StrategyTotalPnL) == 0 &&
		valuation.AccountPeakEquity.Compare(valuation.AccountEquity) >= 0 &&
		valuation.UTCDayBaselineEquity.Compare(zero.money) > 0 &&
		valuation.Rolling24HourBaselineEquity.Compare(zero.money) > 0 &&
		valuation.StrategyPeakPnL.Compare(valuation.StrategyTotalPnL) >= 0 &&
		valuation.Slippage.Compare(zero.percent) >= 0
}

func (valuation StrategyRiskValuation) validProvenance(admission StrategySessionAdmission, now time.Time) bool {
	return valuation.ReconciliationID != "" && strategyRiskHash(valuation.ReconciliationHash) &&
		valuation.StorageRevision > 0 && valuation.EngineStartupCycle == admission.StartupCycle &&
		valuation.AdmissionHash == StrategyRiskAdmissionHash(admission) &&
		!valuation.StorageObservedAt.IsZero() && valuation.StorageObservedAt.Location() == time.UTC &&
		!valuation.StorageObservedAt.After(now) && now.Sub(valuation.StorageObservedAt) <= 30*time.Second &&
		!valuation.ObservedAt.IsZero() && valuation.ObservedAt.Location() == time.UTC && valuation.ObservedAt.Equal(now)
}

func (valuation StrategyRiskValuation) validEmptyAccounting(zero strategyRiskZeros) bool {
	return valuation.AccountingProjectionHash == "" &&
		valuation.StrategyPositionQuantity.Compare(zero.balance) == 0 &&
		valuation.StrategyPositionValue.Compare(zero.money) == 0 &&
		valuation.StrategyTotalCost.Compare(zero.money) == 0 &&
		valuation.StrategyRealizedPnL.Compare(zero.pnl) == 0 &&
		valuation.StrategyUnrealizedPnL.Compare(zero.pnl) == 0 &&
		valuation.StrategyTotalPnL.Compare(zero.pnl) == 0
}

// Observation converts exact valuation evidence into conservative percentages
// and the complete mandatory central-risk input. Only evaluated rows can
// authorize this conversion; baseline rows deliberately make the caller wait.
func (valuation StrategyRiskValuation) Observation(
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	admission StrategySessionAdmission,
	now time.Time,
) (StrategyRiskObservation, error) {
	if valuation.Purpose != StrategyRiskValuationEvaluated ||
		valuation.ValidFor(work, snapshot, market, facts, admission, now) != nil ||
		len(market.Book.Bids) == 0 || len(market.Book.Asks) == 0 {
		return StrategyRiskObservation{}, contractError("strategy_risk_valuation_unavailable")
	}
	values, err := valuation.observationPercentages()
	if err != nil {
		return StrategyRiskObservation{}, contractError("strategy_risk_valuation_invalid")
	}
	timing, err := strategyRiskObservationTiming(market, admission, now)
	if err != nil {
		return StrategyRiskObservation{}, err
	}
	observation := StrategyRiskObservation{
		StrategySessionID: work.SessionID, StrategyRevision: work.StrategyRevision,
		AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: snapshot.SnapshotHash, MarketHash: valuation.MarketHash, Instrument: work.Instrument,
		PolicyID: facts.PolicyID, PolicyVersion: facts.PolicyVersion, PolicyHash: facts.PolicyHash,
		AccountDrawdown: values.accountDrawdown, UTCDayLoss: values.utcDayLoss,
		Rolling24HourLoss: values.rollingLoss, StrategyLoss: values.strategyLoss,
		AssetExposure: values.assetExposure, CombinedExposure: values.combinedExposure,
		ExchangeExposure: values.exchangeExposure, Reserve: values.reserve, ReservedCapital: values.reserved,
		Spread: timing.spread, Slippage: valuation.Slippage, OpenOrders: valuation.OpenOrders,
		BookAge: timing.bookAge, QueueLag: timing.queueLag, ClockDrift: timing.clockDrift, QualityScore: 100,
		ObservedAt: now,
	}
	if observation.ValidFor(work, snapshot, market, facts, now) != nil {
		return StrategyRiskObservation{}, contractError("strategy_risk_valuation_invalid")
	}
	return observation, nil
}

type strategyRiskObservationPercentages struct {
	accountDrawdown, utcDayLoss, rollingLoss, strategyLoss domain.Percent
	assetExposure, combinedExposure, exchangeExposure      domain.Percent
	reserve, reserved                                      domain.Percent
}

func (valuation StrategyRiskValuation) observationPercentages() (strategyRiskObservationPercentages, error) {
	accountLoss, accountErr := riskMoneyLoss(valuation.AccountPeakEquity, valuation.AccountEquity)
	dayLoss, dayErr := riskMoneyLoss(valuation.UTCDayBaselineEquity, valuation.AccountEquity)
	rollingLoss, rollingErr := riskMoneyLoss(valuation.Rolling24HourBaselineEquity, valuation.AccountEquity)
	strategyLoss, strategyErr := riskPnLLoss(valuation.StrategyPeakPnL, valuation.StrategyTotalPnL)
	if accountErr != nil || dayErr != nil || rollingErr != nil || strategyErr != nil {
		return strategyRiskObservationPercentages{}, contractError("strategy_risk_valuation_invalid")
	}
	percent := func(value, basis domain.Money) (domain.Percent, error) {
		return domain.CalculateConservativePercent(value, basis, 18)
	}
	result := strategyRiskObservationPercentages{}
	var err error
	result.accountDrawdown, err = percent(accountLoss, valuation.AccountPeakEquity)
	if err == nil {
		result.utcDayLoss, err = percent(dayLoss, valuation.UTCDayBaselineEquity)
	}
	if err == nil {
		result.rollingLoss, err = percent(rollingLoss, valuation.Rolling24HourBaselineEquity)
	}
	if err == nil {
		result.strategyLoss, err = percent(strategyLoss, valuation.AccountEquity)
	}
	if err == nil {
		result.assetExposure, err = percent(valuation.VolatileAssetValue, valuation.AccountEquity)
	}
	if err == nil {
		result.combinedExposure, err = percent(valuation.CombinedVolatileValue, valuation.AccountEquity)
	}
	if err == nil {
		result.exchangeExposure, err = percent(valuation.ExchangeRiskValue, valuation.AccountEquity)
	}
	if err == nil {
		result.reserve, err = percent(valuation.ReserveValue, valuation.AccountEquity)
	}
	if err == nil {
		result.reserved, err = percent(valuation.ReservedValue, valuation.AccountEquity)
	}
	return result, err
}

type strategyRiskTiming struct {
	spread                        domain.Percent
	bookAge, queueLag, clockDrift time.Duration
}

func strategyRiskObservationTiming(market StrategyMarketInput, admission StrategySessionAdmission,
	now time.Time,
) (strategyRiskTiming, error) {
	spread, err := domain.CalculateRelativeSpread(market.Book.Bids[0].Price, market.Book.Asks[0].Price, 18)
	result := strategyRiskTiming{spread: spread, bookAge: now.Sub(market.Book.ReceivedAt.UTC),
		queueLag: now.Sub(market.ObservedAt.UTC), clockDrift: admission.Eligibility.ClockOffset}
	if result.clockDrift < 0 {
		result.clockDrift = -result.clockDrift
	}
	if err != nil || result.bookAge < 0 || result.queueLag < 0 ||
		!admission.Eligibility.BookHealthy || !admission.Eligibility.BookFresh ||
		!admission.Eligibility.BookEligible || !admission.Eligibility.ClockEligible ||
		admission.Safety.CanSubmitEntry() != nil {
		return strategyRiskTiming{}, contractError("strategy_risk_valuation_invalid")
	}
	return result, nil
}

// EvidenceHash commits to the exact valuation, baselines, and non-secret
// operational references used to derive a central-risk observation.
func (valuation StrategyRiskValuation) EvidenceHash() string {
	parts := []string{
		valuation.Purpose, string(valuation.StrategySessionID), fmt.Sprintf("%d", valuation.StrategyRevision),
		string(valuation.AccountID), fmt.Sprintf("%d", valuation.AccountEpoch), valuation.Instrument,
		valuation.SnapshotHash, valuation.MarketHash, valuation.PolicyID,
		fmt.Sprintf("%d", valuation.PolicyVersion), valuation.PolicyHash, valuation.AccountingState,
		valuation.AccountingEvidenceHash, valuation.AccountingProjectionHash, valuation.MarkPrice.String(),
		valuation.AccountEquity.String(), valuation.VolatileAssetValue.String(),
		valuation.CombinedVolatileValue.String(), valuation.CommittedBuyValue.String(),
		valuation.ExchangeRiskValue.String(), valuation.ReserveValue.String(), valuation.ReservedValue.String(),
		valuation.StrategyPositionQuantity.String(), valuation.StrategyPositionValue.String(),
		valuation.StrategyTotalCost.String(), valuation.StrategyRealizedPnL.String(),
		valuation.StrategyUnrealizedPnL.String(), valuation.StrategyTotalPnL.String(),
		valuation.AccountPeakEquity.String(), valuation.UTCDayBaselineEquity.String(),
		valuation.Rolling24HourBaselineEquity.String(), valuation.StrategyPeakPnL.String(),
		fmt.Sprintf("%d", valuation.OpenOrders), valuation.Slippage.String(), valuation.ReconciliationID,
		valuation.ReconciliationHash, fmt.Sprintf("%d", valuation.StorageRevision),
		valuation.StorageObservedAt.Format(time.RFC3339Nano), fmt.Sprintf("%d", valuation.EngineStartupCycle),
		valuation.AdmissionHash, valuation.ObservedAt.Format(time.RFC3339Nano),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// StrategyRiskAdmissionHash is a redacted identity of the exact admission
// proof. It contains no credential, signature, or exchange payload.
func StrategyRiskAdmissionHash(admission StrategySessionAdmission) string {
	if admission.Valid() != nil {
		return ""
	}
	values := []string{
		string(admission.Work.SessionID), string(admission.Work.Account.ID),
		fmt.Sprintf("%d", admission.Work.Account.Epoch), fmt.Sprintf("%d", admission.Work.StrategyRevision),
		admission.Work.Instrument, admission.Arm.ID, fmt.Sprintf("%d", admission.Arm.Revision),
		fmt.Sprintf("%d", admission.StartupCycle), admission.Eligibility.Exchange,
		admission.Eligibility.Instrument, admission.Eligibility.ObservedAt.Format(time.RFC3339Nano),
		fmt.Sprintf("%t", admission.Eligibility.BookHealthy), fmt.Sprintf("%t", admission.Eligibility.BookFresh),
		fmt.Sprintf("%t", admission.Eligibility.ClockEligible), fmt.Sprintf("%d", admission.Eligibility.ClockOffset),
		fmt.Sprintf("%t", admission.Safety.ReconciliationClean),
		fmt.Sprintf("%t", admission.Safety.EvidenceHealthy), fmt.Sprintf("%t", admission.Safety.LeaseHeld),
		admission.ApprovedAt.Format(time.RFC3339Nano),
	}
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func riskMoneyLoss(baseline, current domain.Money) (domain.Money, error) {
	zero, _ := domain.ParseMoney("0")
	if current.Compare(baseline) >= 0 {
		return zero, nil
	}
	return baseline.Subtract(current)
}

func riskPnLLoss(peak, current domain.PnL) (domain.Money, error) {
	zero, _ := domain.ParseMoney("0")
	if current.Compare(peak) >= 0 {
		return zero, nil
	}
	difference, err := peak.Subtract(current)
	if err != nil {
		return domain.Money{}, err
	}
	return domain.ParseMoney(difference.String())
}
