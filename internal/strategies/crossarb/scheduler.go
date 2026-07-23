package crossarb

import (
	"sort"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

// TimelinePhase keeps initial arrival, unknown verification, bounded retry,
// and protected unwind facts distinct.
type TimelinePhase string

// Timeline phases preserve arrival, verification, and retry ordering.
const (
	// PhaseArrival is the first simulated venue response.
	PhaseArrival TimelinePhase = "arrival"
	// PhaseVerification resolves an unknown response before any retry.
	PhaseVerification TimelinePhase = "verification"
	// PhaseRetry is the single bounded central-risk-authorized retry.
	PhaseRetry TimelinePhase = "retry"
)

// LegDirective is one deterministic simulated venue response. Input is quote
// for a buy and base for a sell; partial fills must provide an exact subset.
type LegDirective struct {
	State execution.OrderState
	Input domain.Quantity
}

// Timeline supplies immutable future public books and simulated responses.
type Timeline interface {
	MarketAt(exchange string, instrument domain.Instrument, offset uint64) (Market, error)
	DirectiveAt(exchange string, phase TimelinePhase, offset uint64) (LegDirective, error)
}

// LatencyDistribution is a reviewed deterministic empirical sample set.
type LatencyDistribution struct {
	Version           string
	BuySamplesNanos   []uint64
	SellSamplesNanos  []uint64
	SampleOrdinal     uint64
	VerificationNanos uint64
	RetryNanos        uint64
	RecoveryNanos     uint64
}

type scheduledLeg struct {
	index    int
	exchange string
	offset   uint64
}

func schedule(candidate Candidate, latency LatencyDistribution) ([]scheduledLeg, error) {
	if latency.Version == "" || len(latency.BuySamplesNanos) == 0 || len(latency.SellSamplesNanos) == 0 ||
		latency.VerificationNanos == 0 || latency.RetryNanos == 0 || latency.RecoveryNanos == 0 {
		return nil, strategyError("latency_distribution_invalid")
	}
	buyIndex := latency.SampleOrdinal % uint64(len(latency.BuySamplesNanos))
	sellIndex := latency.SampleOrdinal % uint64(len(latency.SellSamplesNanos))
	buyLatency := latency.BuySamplesNanos[buyIndex]
	sellLatency := latency.SellSamplesNanos[sellIndex]
	if buyLatency == 0 || sellLatency == 0 {
		return nil, strategyError("latency_distribution_invalid")
	}
	events := []scheduledLeg{
		{index: 0, exchange: candidate.BuyExchange, offset: candidate.DecisionOffsetNanos + buyLatency},
		{index: 1, exchange: candidate.SellExchange, offset: candidate.DecisionOffsetNanos + sellLatency},
	}
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].offset != events[right].offset {
			return events[left].offset < events[right].offset
		}
		return events[left].index < events[right].index
	})
	return events, nil
}
