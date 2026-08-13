package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
)

// StrategySagaRiskMember binds one independently fenced account observation
// to the exact public view and durable facts used by a multi-leg decision. It
// contains no engine owner, fence, adapter, or submission capability.
type StrategySagaRiskMember struct {
	Work        StrategySessionWork
	Snapshot    AccountSnapshot
	Market      StrategyMarketInput
	Facts       StrategyRiskFacts
	Observation StrategyRiskObservation
}

// StrategySagaRiskProjectionMember carries all non-secret durable inputs from
// which the persistence boundary derives one account observation. The peer
// engine lease is deliberately absent; the store must verify it directly.
type StrategySagaRiskProjectionMember struct {
	Admission StrategySessionAdmission
	Snapshot  AccountSnapshot
	Market    StrategyMarketInput
	Facts     StrategyRiskFacts
}

// StrategySagaRiskObservationProjector atomically projects every independently
// leased account and returns only a conservative credential-free aggregate.
// The supplied lease belongs solely to the coordinator account.
type StrategySagaRiskObservationProjector interface {
	ProjectStrategySagaRiskInputs(
		context.Context,
		StrategySessionExecutionLease,
		StrategySessionWork,
		[]StrategySagaRiskProjectionMember,
		time.Time,
	) (*StrategySagaRiskInputs, error)
}

// StrategySagaRiskEvidence preserves the independently derived identity for
// every account contributing to one conservative aggregate risk decision.
type StrategySagaRiskEvidence struct {
	AccountID       AccountID
	AccountEpoch    uint64
	Exchange        Exchange
	SnapshotHash    string
	MarketHash      string
	PolicyHash      string
	ObservationHash string
}

type strategySagaRiskAggregate struct {
	AccountDrawdown, UTCDayLoss, Rolling24HourLoss, StrategyLoss   domain.Percent
	AssetExposure, CombinedExposure, ExchangeExposure              domain.Percent
	Reserve, ReservedCapital, Spread, Slippage                     domain.Percent
	OpenOrders                                                     uint32
	BookAge, QueueLag, ClockDrift                                  time.Duration
	QualityScore                                                   uint8
	Gap, StaleData, ReconciliationFault, AccountingFault           bool
	UnknownOrder, PersistenceFault, DiskFault, APIError, LeaseLost bool
}

// StrategySagaRiskInputs is one immutable, credential-free central-risk input
// for a Triangular or paired Cross-Exchange candidate. Aggregation is
// intentionally conservative: additive capacity is summed, adverse values
// use the worst member, reserve and quality use the weakest member, and every
// health fault is retained.
type StrategySagaRiskInputs struct {
	aggregate  strategySagaRiskAggregate
	policy     risk.Policy
	evaluated  time.Time
	evidence   []StrategySagaRiskEvidence
	evidenceID string
}

// NewStrategySagaRiskInputs validates all account evidence at the same exact
// decision instant and constructs a fail-closed aggregate. Cross-Exchange
// requires one Binance and one Bybit member; Triangular requires exactly one
// account and never fabricates a second risk scope.
func NewStrategySagaRiskInputs(
	members []StrategySagaRiskMember,
	now time.Time,
) (*StrategySagaRiskInputs, error) {
	if now.IsZero() || now.Location() != time.UTC || len(members) == 0 {
		return nil, contractError("strategy_saga_risk_inputs_invalid")
	}
	want, validStrategy := strategySagaRiskMemberCount(members[0].Work.Strategy)
	if !validStrategy || len(members) != want {
		return nil, contractError("strategy_saga_risk_inputs_invalid")
	}
	zero, err := domain.ParsePercent("0")
	if err != nil {
		return nil, contractError("strategy_saga_risk_inputs_invalid")
	}
	result := newStrategySagaRiskInputs(now, zero)
	seenAccounts := make(map[AccountID]struct{}, want)
	seenExchanges := make(map[Exchange]struct{}, want)
	for index, member := range members {
		if err := result.addSagaRiskMember(member, members[0], index, now,
			seenAccounts, seenExchanges); err != nil {
			return nil, err
		}
	}
	if !validSagaRiskExchanges(want, seenExchanges) {
		return nil, contractError("strategy_saga_risk_inputs_invalid")
	}
	result.finalizeSagaRiskEvidence(members[0].Work)
	if result.evidenceID == "" {
		return nil, contractError("strategy_saga_risk_inputs_invalid")
	}
	return result, nil
}

func strategySagaRiskMemberCount(strategy string) (int, bool) {
	switch strategy {
	case StrategyCrossExchangeArbitrage:
		return 2, true
	case StrategyTriangular:
		return 1, true
	default:
		return 0, false
	}
}

func newStrategySagaRiskInputs(now time.Time, zero domain.Percent) *StrategySagaRiskInputs {
	return &StrategySagaRiskInputs{evaluated: now, aggregate: strategySagaRiskAggregate{
		AccountDrawdown: zero, UTCDayLoss: zero, Rolling24HourLoss: zero,
		StrategyLoss: zero, AssetExposure: zero, CombinedExposure: zero,
		ExchangeExposure: zero, Reserve: zero, ReservedCapital: zero,
		Spread: zero, Slippage: zero, QualityScore: 100,
	}}
}

func (inputs *StrategySagaRiskInputs) addSagaRiskMember(member, baseline StrategySagaRiskMember,
	index int, now time.Time, seenAccounts map[AccountID]struct{}, seenExchanges map[Exchange]struct{},
) error {
	if !validSagaRiskMember(member, baseline.Work, now) ||
		!registerSagaRiskMember(member.Work, seenAccounts, seenExchanges) {
		return contractError("strategy_saga_risk_inputs_invalid")
	}
	if index == 0 {
		inputs.policy = member.Facts.Policy
		inputs.aggregate.Reserve = member.Observation.Reserve
	} else if member.Facts.PolicyHash != baseline.Facts.PolicyHash ||
		!reflect.DeepEqual(member.Facts.Policy, inputs.policy) {
		return contractError("strategy_saga_risk_inputs_invalid")
	}
	if inputs.addSagaRiskCapacity(member.Observation) != nil {
		return contractError("strategy_saga_risk_inputs_invalid")
	}
	inputs.mergeSagaRiskAdverse(member.Observation)
	inputs.mergeSagaRiskHealth(member.Observation)
	inputs.appendSagaRiskEvidence(member)
	return nil
}

func validSagaRiskMember(member StrategySagaRiskMember, baseline StrategySessionWork, now time.Time) bool {
	return member.Work.ValidAt(now) == nil && sameSagaRiskTopology(baseline, member.Work) &&
		member.Observation.ValidFor(member.Work, member.Snapshot, member.Market, member.Facts, now) == nil &&
		member.Observation.ObservedAt.Equal(now) && risk.ValidatePolicy(member.Facts.Policy) == nil &&
		member.Facts.Policy.State == risk.StateNormal
}

func registerSagaRiskMember(work StrategySessionWork, seenAccounts map[AccountID]struct{},
	seenExchanges map[Exchange]struct{},
) bool {
	if _, duplicate := seenAccounts[work.Account.ID]; duplicate {
		return false
	}
	if _, duplicate := seenExchanges[work.Account.Exchange]; duplicate {
		return false
	}
	seenAccounts[work.Account.ID] = struct{}{}
	seenExchanges[work.Account.Exchange] = struct{}{}
	return true
}

func (inputs *StrategySagaRiskInputs) addSagaRiskCapacity(observation StrategyRiskObservation) error {
	var err error
	inputs.aggregate.AssetExposure, err = inputs.aggregate.AssetExposure.Add(observation.AssetExposure)
	if err == nil {
		inputs.aggregate.CombinedExposure, err = inputs.aggregate.CombinedExposure.Add(observation.CombinedExposure)
	}
	if err == nil {
		inputs.aggregate.ExchangeExposure, err = inputs.aggregate.ExchangeExposure.Add(observation.ExchangeExposure)
	}
	if err == nil {
		inputs.aggregate.ReservedCapital, err = inputs.aggregate.ReservedCapital.Add(observation.ReservedCapital)
	}
	if err != nil || uint64(inputs.aggregate.OpenOrders)+uint64(observation.OpenOrders) > uint64(^uint32(0)) {
		return contractError("strategy_saga_risk_inputs_invalid")
	}
	inputs.aggregate.OpenOrders += observation.OpenOrders
	return nil
}

func (inputs *StrategySagaRiskInputs) mergeSagaRiskAdverse(observation StrategyRiskObservation) {
	maxPercent(&inputs.aggregate.AccountDrawdown, observation.AccountDrawdown)
	maxPercent(&inputs.aggregate.UTCDayLoss, observation.UTCDayLoss)
	maxPercent(&inputs.aggregate.Rolling24HourLoss, observation.Rolling24HourLoss)
	maxPercent(&inputs.aggregate.StrategyLoss, observation.StrategyLoss)
	maxPercent(&inputs.aggregate.Spread, observation.Spread)
	maxPercent(&inputs.aggregate.Slippage, observation.Slippage)
	if observation.Reserve.Compare(inputs.aggregate.Reserve) < 0 {
		inputs.aggregate.Reserve = observation.Reserve
	}
	if observation.BookAge > inputs.aggregate.BookAge {
		inputs.aggregate.BookAge = observation.BookAge
	}
	if observation.QueueLag > inputs.aggregate.QueueLag {
		inputs.aggregate.QueueLag = observation.QueueLag
	}
	clock := absoluteSagaRiskDuration(observation.ClockDrift)
	if clock > inputs.aggregate.ClockDrift {
		inputs.aggregate.ClockDrift = clock
	}
	if observation.QualityScore < inputs.aggregate.QualityScore {
		inputs.aggregate.QualityScore = observation.QualityScore
	}
}

func (inputs *StrategySagaRiskInputs) mergeSagaRiskHealth(observation StrategyRiskObservation) {
	inputs.aggregate.Gap = inputs.aggregate.Gap || observation.Gap
	inputs.aggregate.StaleData = inputs.aggregate.StaleData || observation.StaleData
	inputs.aggregate.ReconciliationFault = inputs.aggregate.ReconciliationFault || observation.ReconciliationFault
	inputs.aggregate.AccountingFault = inputs.aggregate.AccountingFault || observation.AccountingFault
	inputs.aggregate.UnknownOrder = inputs.aggregate.UnknownOrder || observation.UnknownOrder
	inputs.aggregate.PersistenceFault = inputs.aggregate.PersistenceFault || observation.PersistenceFault
	inputs.aggregate.DiskFault = inputs.aggregate.DiskFault || observation.DiskFault
	inputs.aggregate.APIError = inputs.aggregate.APIError || observation.APIError
	inputs.aggregate.LeaseLost = inputs.aggregate.LeaseLost || observation.LeaseLost
}

func (inputs *StrategySagaRiskInputs) appendSagaRiskEvidence(member StrategySagaRiskMember) {
	inputs.evidence = append(inputs.evidence, StrategySagaRiskEvidence{
		AccountID: member.Work.Account.ID, AccountEpoch: member.Work.Account.Epoch,
		Exchange: member.Work.Account.Exchange, SnapshotHash: member.Snapshot.SnapshotHash,
		MarketHash: member.Observation.MarketHash, PolicyHash: member.Observation.PolicyHash,
		ObservationHash: member.Observation.EvidenceHash(),
	})
}

func validSagaRiskExchanges(want int, exchanges map[Exchange]struct{}) bool {
	if want != 2 {
		return true
	}
	_, hasBinance := exchanges[ExchangeBinance]
	_, hasBybit := exchanges[ExchangeBybit]
	return hasBinance && hasBybit
}

func (inputs *StrategySagaRiskInputs) finalizeSagaRiskEvidence(work StrategySessionWork) {
	sort.Slice(inputs.evidence, func(left, right int) bool {
		if inputs.evidence[left].Exchange != inputs.evidence[right].Exchange {
			return inputs.evidence[left].Exchange < inputs.evidence[right].Exchange
		}
		return inputs.evidence[left].AccountID < inputs.evidence[right].AccountID
	})
	inputs.evidenceID = sagaRiskEvidenceHash(work, inputs.policy, inputs.evidence, inputs.evaluated)
}

// Current implements the central risk observation-provider boundary without
// any storage or network access between allocation and approval.
func (inputs *StrategySagaRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	if inputs == nil || inputs.evidenceID == "" || inputs.evaluated.IsZero() ||
		risk.ValidatePolicy(inputs.policy) != nil {
		return risk.Observations{}, nil, time.Time{}, fmt.Errorf("sandbox_strategy_saga_risk_inputs_unavailable")
	}
	value := inputs.aggregate
	return risk.Observations{
		AccountDrawdown: &value.AccountDrawdown, UTCDayLoss: &value.UTCDayLoss,
		Rolling24HourLoss: &value.Rolling24HourLoss, StrategyLoss: &value.StrategyLoss,
		AssetExposure: &value.AssetExposure, CombinedExposure: &value.CombinedExposure,
		ExchangeExposure: &value.ExchangeExposure, Reserve: &value.Reserve,
		ReservedCapital: &value.ReservedCapital, Spread: &value.Spread, Slippage: &value.Slippage,
		OpenOrders: &value.OpenOrders, BookAge: &value.BookAge, QueueLag: &value.QueueLag,
		ClockDrift: &value.ClockDrift, QualityScore: &value.QualityScore,
		Health: risk.HealthInputs{
			Gap: &value.Gap, StaleData: &value.StaleData,
			ReconciliationFault: &value.ReconciliationFault, AccountingFault: &value.AccountingFault,
			UnknownOrder: &value.UnknownOrder, PersistenceFault: &value.PersistenceFault,
			DiskFault: &value.DiskFault, APIError: &value.APIError, LeaseLost: &value.LeaseLost,
		},
	}, []risk.Policy{inputs.policy}, inputs.evaluated, nil
}

// Evidence returns a defensive account-evidence vector and its aggregate
// identity for durable decision attribution.
func (inputs *StrategySagaRiskInputs) Evidence() ([]StrategySagaRiskEvidence, string) {
	if inputs == nil {
		return nil, ""
	}
	return append([]StrategySagaRiskEvidence(nil), inputs.evidence...), inputs.evidenceID
}

func sameSagaRiskTopology(expected, actual StrategySessionWork) bool {
	return actual.SessionID == expected.SessionID && actual.Strategy == expected.Strategy &&
		actual.Instrument == expected.Instrument && actual.ConfigurationID == expected.ConfigurationID &&
		actual.ConfigurationHash == expected.ConfigurationHash && actual.StrategySetHash == expected.StrategySetHash &&
		actual.SessionRevision == expected.SessionRevision && actual.StrategyRevision == expected.StrategyRevision &&
		actual.ArmID == expected.ArmID && actual.ArmRevision == expected.ArmRevision &&
		actual.StartedAt.Equal(expected.StartedAt) && actual.ArmExpiresAt.Equal(expected.ArmExpiresAt)
}

func maxPercent(current *domain.Percent, candidate domain.Percent) {
	if candidate.Compare(*current) > 0 {
		*current = candidate
	}
}

func absoluteSagaRiskDuration(value time.Duration) time.Duration {
	if value == time.Duration(-1<<63) {
		return time.Duration(1<<63 - 1)
	}
	if value < 0 {
		return -value
	}
	return value
}

func sagaRiskEvidenceHash(
	work StrategySessionWork,
	policy risk.Policy,
	evidence []StrategySagaRiskEvidence,
	now time.Time,
) string {
	parts := []string{string(work.SessionID), work.Strategy, work.Instrument,
		fmt.Sprintf("%d", work.StrategyRevision), policy.ID, fmt.Sprintf("%d", policy.Version),
		now.Format(time.RFC3339Nano)}
	for _, item := range evidence {
		if item.AccountID == "" || item.AccountEpoch == 0 || item.Exchange == "" ||
			!strategyRiskHash(item.SnapshotHash) || !strategyRiskHash(item.MarketHash) ||
			!strategyRiskHash(item.PolicyHash) || !strategyRiskHash(item.ObservationHash) {
			return ""
		}
		parts = append(parts, string(item.Exchange), string(item.AccountID),
			fmt.Sprintf("%d", item.AccountEpoch), item.SnapshotHash, item.MarketHash,
			item.PolicyHash, item.ObservationHash)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

var _ risk.ObservationProvider = (*StrategySagaRiskInputs)(nil)
