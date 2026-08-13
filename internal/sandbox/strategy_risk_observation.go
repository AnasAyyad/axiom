package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
)

// StrategyRiskObservation is the complete, non-secret central-risk input
// bound to one account snapshot and public market view. Every field is
// explicit: absent loss, exposure, health, or timing facts must make an
// automatic strategy wait instead of being interpreted as healthy.
type StrategyRiskObservation struct {
	StrategySessionID                                                  SessionID
	StrategyRevision                                                   uint64
	AccountID                                                          AccountID
	AccountEpoch                                                       uint64
	SnapshotHash                                                       string
	MarketHash                                                         string
	Instrument                                                         string
	PolicyID                                                           string
	PolicyVersion                                                      uint64
	PolicyHash                                                         string
	AccountDrawdown                                                    domain.Percent
	UTCDayLoss                                                         domain.Percent
	Rolling24HourLoss                                                  domain.Percent
	StrategyLoss                                                       domain.Percent
	AssetExposure                                                      domain.Percent
	CombinedExposure                                                   domain.Percent
	ExchangeExposure                                                   domain.Percent
	Reserve                                                            domain.Percent
	ReservedCapital                                                    domain.Percent
	Spread                                                             domain.Percent
	Slippage                                                           domain.Percent
	OpenOrders                                                         uint32
	BookAge                                                            time.Duration
	QueueLag                                                           time.Duration
	ClockDrift                                                         time.Duration
	QualityScore                                                       uint8
	Gap, StaleData, ReconciliationFault, AccountingFault, UnknownOrder bool
	PersistenceFault, DiskFault, APIError, LeaseLost                   bool
	ObservedAt                                                         time.Time
}

// StrategyRiskObservationSource is deliberately independent from account and
// public-market readers. Implementations must join authoritative performance,
// reconciliation, health, and policy projections; they may not substitute
// zero losses or false health faults when a required input is unavailable.
type StrategyRiskObservationSource interface {
	StrategyRiskObservation(
		context.Context,
		StrategySessionWork,
		AccountSnapshot,
		StrategyMarketInput,
		StrategyRiskFacts,
		time.Time,
	) (StrategyRiskObservation, error)
}

// StrategyRiskObservationRecorder persists one already-complete observation
// under the exact fenced engine lease. Recording evidence does not authorize
// an order or replace central risk evaluation.
type StrategyRiskObservationRecorder interface {
	RecordStrategyRiskObservation(
		context.Context,
		string,
		uint64,
		StrategySessionWork,
		AccountSnapshot,
		StrategyMarketInput,
		StrategyRiskFacts,
		StrategyRiskObservation,
		time.Time,
	) error
}

// ValidFor proves the observation describes the exact decision-time account
// and instrument. It permits restrictive values, including zero capacity or
// active faults; central risk is responsible for turning those facts into the
// appropriate rejection or circuit-breaker action.
func (observation StrategyRiskObservation) ValidFor(
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	now time.Time,
) error {
	if work.ValidAt(now) != nil || snapshot.Validate() != nil || facts.ValidFor(work, snapshot, now) != nil ||
		market.Instrument.Symbol() != work.Instrument ||
		(singleVenueStrategy(work.Strategy) && len(market.Candles) == 0) ||
		observation.StrategySessionID != work.SessionID ||
		observation.StrategyRevision != work.StrategyRevision ||
		observation.AccountID != work.Account.ID || observation.AccountEpoch != work.Account.Epoch ||
		observation.SnapshotHash != snapshot.SnapshotHash || !strategyRiskHash(observation.SnapshotHash) ||
		observation.MarketHash != StrategyMarketEvidenceHash(market) || !strategyRiskHash(observation.MarketHash) ||
		observation.Instrument != work.Instrument || observation.PolicyID != facts.PolicyID ||
		observation.PolicyVersion != facts.PolicyVersion || observation.PolicyHash != facts.PolicyHash ||
		observation.ObservedAt.IsZero() ||
		observation.ObservedAt.Location() != time.UTC || observation.ObservedAt.After(now) ||
		now.Sub(observation.ObservedAt) > 250*time.Millisecond ||
		snapshot.ObservedAt.After(observation.ObservedAt) ||
		observation.ObservedAt.Sub(snapshot.ObservedAt) > 250*time.Millisecond ||
		market.ObservedAt.UTC.IsZero() || market.ObservedAt.UTC.Location() != time.UTC ||
		market.ObservedAt.UTC.After(observation.ObservedAt) ||
		observation.ObservedAt.Sub(market.ObservedAt.UTC) > 250*time.Millisecond ||
		facts.ObservedAt.After(observation.ObservedAt) ||
		observation.ObservedAt.Sub(facts.ObservedAt) > 250*time.Millisecond ||
		observation.BookAge < 0 || observation.QueueLag < 0 || observation.QualityScore > 100 {
		return contractError("strategy_risk_observation_invalid")
	}
	return nil
}

// StrategyMarketEvidenceHash binds the exact metadata, book, and finalized
// candle payloads used for one evaluation without retaining those payloads in
// the private-account risk projection.
func StrategyMarketEvidenceHash(market StrategyMarketInput) string {
	if market.Instrument.Symbol() == "" || !strategyRiskHash(market.Metadata.RawPayloadHash) ||
		!strategyRiskHash(market.Book.RawPayloadHash) {
		return ""
	}
	intervals := make([]string, 0, len(market.Candles))
	for interval := range market.Candles {
		intervals = append(intervals, interval)
	}
	sort.Strings(intervals)
	parts := []string{market.Instrument.Symbol(), market.Metadata.RawPayloadHash, market.Book.RawPayloadHash,
		market.ObservedAt.UTC.Format(time.RFC3339Nano), fmt.Sprintf("%d", market.ObservedAt.Sequence)}
	for _, interval := range intervals {
		items := market.Candles[interval]
		if len(items) == 0 {
			return ""
		}
		parts = append(parts, interval)
		for _, item := range items {
			if !strategyRiskHash(item.RawPayloadHash) {
				return ""
			}
			parts = append(parts, item.RawPayloadHash)
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// EvidenceHash is the stable identity of every persisted risk input. It is
// calculated only from redacted measurements and immutable references.
func (observation StrategyRiskObservation) EvidenceHash() string {
	parts := []string{
		string(observation.StrategySessionID), fmt.Sprintf("%d", observation.StrategyRevision),
		string(observation.AccountID), fmt.Sprintf("%d", observation.AccountEpoch),
		observation.SnapshotHash, observation.MarketHash, observation.Instrument,
		observation.PolicyID, fmt.Sprintf("%d", observation.PolicyVersion), observation.PolicyHash,
		observation.AccountDrawdown.String(), observation.UTCDayLoss.String(),
		observation.Rolling24HourLoss.String(), observation.StrategyLoss.String(),
		observation.AssetExposure.String(), observation.CombinedExposure.String(),
		observation.ExchangeExposure.String(), observation.Reserve.String(),
		observation.ReservedCapital.String(), observation.Spread.String(), observation.Slippage.String(),
		fmt.Sprintf("%d", observation.OpenOrders), fmt.Sprintf("%d", observation.BookAge),
		fmt.Sprintf("%d", observation.QueueLag), fmt.Sprintf("%d", observation.ClockDrift),
		fmt.Sprintf("%d", observation.QualityScore),
		fmt.Sprintf("%t", observation.Gap), fmt.Sprintf("%t", observation.StaleData),
		fmt.Sprintf("%t", observation.ReconciliationFault), fmt.Sprintf("%t", observation.AccountingFault),
		fmt.Sprintf("%t", observation.UnknownOrder), fmt.Sprintf("%t", observation.PersistenceFault),
		fmt.Sprintf("%t", observation.DiskFault), fmt.Sprintf("%t", observation.APIError),
		fmt.Sprintf("%t", observation.LeaseLost),
		observation.ObservedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// RiskObservations converts the complete snapshot into the central risk
// package representation. Callers must validate first, which ensures every
// pointer expected by risk.Engine is populated from explicit facts.
func (observation StrategyRiskObservation) RiskObservations() risk.Observations {
	return risk.Observations{
		AccountDrawdown: &observation.AccountDrawdown, UTCDayLoss: &observation.UTCDayLoss,
		Rolling24HourLoss: &observation.Rolling24HourLoss, StrategyLoss: &observation.StrategyLoss,
		AssetExposure: &observation.AssetExposure, CombinedExposure: &observation.CombinedExposure,
		ExchangeExposure: &observation.ExchangeExposure, Reserve: &observation.Reserve,
		ReservedCapital: &observation.ReservedCapital, Spread: &observation.Spread,
		Slippage: &observation.Slippage, OpenOrders: &observation.OpenOrders,
		BookAge: &observation.BookAge, QueueLag: &observation.QueueLag,
		ClockDrift: &observation.ClockDrift, QualityScore: &observation.QualityScore,
		Health: risk.HealthInputs{Gap: &observation.Gap, StaleData: &observation.StaleData,
			ReconciliationFault: &observation.ReconciliationFault, AccountingFault: &observation.AccountingFault,
			UnknownOrder: &observation.UnknownOrder, PersistenceFault: &observation.PersistenceFault,
			DiskFault: &observation.DiskFault, APIError: &observation.APIError, LeaseLost: &observation.LeaseLost},
	}
}
