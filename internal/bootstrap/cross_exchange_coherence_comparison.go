package bootstrap

import (
	"context"
	"errors"
	"sort"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
)

const crossExchangeMaximumSourceDelay = 250 * time.Millisecond

type crossExchangeCoherenceVerdict struct {
	PolicyVersion string `json:"policy_version"`
	Passed        bool   `json:"passed"`
	Reason        string `json:"reason,omitempty"`
	ViewID        string `json:"view_id,omitempty"`
}

type crossExchangeMemberTiming struct {
	Exchange                 string        `json:"exchange"`
	BookVersion              uint64        `json:"book_version"`
	ConnectionGeneration     uint64        `json:"connection_generation"`
	BookAge                  time.Duration `json:"book_age_nanos"`
	ExchangeTime             time.Time     `json:"exchange_time"`
	CorrectedReceiveEarliest time.Time     `json:"corrected_receive_earliest"`
	CorrectedReceiveLatest   time.Time     `json:"corrected_receive_latest"`
	SourceDelayMinimum       time.Duration `json:"source_delay_minimum_nanos"`
	SourceDelayMaximum       time.Duration `json:"source_delay_maximum_nanos"`
}

type crossExchangeCoherenceComparison struct {
	Trigger          exchangecontracts.BookCommit  `json:"trigger"`
	Decision         runtimecore.AsOfTrigger       `json:"decision"`
	StrictB2         crossExchangeCoherenceVerdict `json:"strict_b2"`
	Actionable       crossExchangeCoherenceVerdict `json:"actionable"`
	ReceiveSkew      time.Duration                 `json:"receive_skew_nanos"`
	CorrectedOverlap time.Duration                 `json:"corrected_overlap_nanos"`
	Members          []crossExchangeMemberTiming   `json:"members"`
}

type crossExchangeCoherenceStatistics struct {
	Comparisons      uint64                           `json:"comparisons"`
	StrictPasses     uint64                           `json:"strict_passes"`
	ActionablePasses uint64                           `json:"actionable_passes"`
	StrictRejections map[string]uint64                `json:"strict_rejections"`
	ActionRejections map[string]uint64                `json:"actionable_rejections"`
	Last             crossExchangeCoherenceComparison `json:"last"`
}

func newCrossExchangeCoherenceStatistics() crossExchangeCoherenceStatistics {
	return crossExchangeCoherenceStatistics{StrictRejections: make(map[string]uint64),
		ActionRejections: make(map[string]uint64)}
}

func (session *ownerConsoleCrossExchangeShadowSession) observeCrossExchangeComparison(
	comparison crossExchangeCoherenceComparison,
) {
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	if session.coherenceStats.StrictRejections == nil {
		session.coherenceStats = newCrossExchangeCoherenceStatistics()
	}
	session.coherenceStats.Comparisons++
	if comparison.StrictB2.Passed {
		session.coherenceStats.StrictPasses++
	} else {
		session.coherenceStats.StrictRejections[comparison.StrictB2.Reason]++
	}
	if comparison.Actionable.Passed {
		session.coherenceStats.ActionablePasses++
	} else {
		session.coherenceStats.ActionRejections[comparison.Actionable.Reason]++
	}
	session.coherenceStats.Last = comparison
}

func (session *ownerConsoleCrossExchangeShadowSession) crossExchangeCoherenceStatistics() crossExchangeCoherenceStatistics {
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	result := session.coherenceStats
	result.StrictRejections = cloneReasonCounts(result.StrictRejections)
	result.ActionRejections = cloneReasonCounts(result.ActionRejections)
	result.Last.Members = append([]crossExchangeMemberTiming(nil), result.Last.Members...)
	return result
}

func cloneReasonCounts(values map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(values))
	for reason, count := range values {
		result[reason] = count
	}
	return result
}

type fixedSandboxSagaMarketViewSource struct{ set SandboxSagaMarketViewSet }

// CaptureSandboxSagaMarketViews returns the immutable set supplied to the dual-policy comparison.
func (source fixedSandboxSagaMarketViewSource) CaptureSandboxSagaMarketViews(
	context.Context, []runtimecore.MarketKey, time.Time,
) (SandboxSagaMarketViewSet, error) {
	return source.set, nil
}

func compareCrossExchangeCapture(ctx context.Context, keys []runtimecore.MarketKey, now time.Time,
	trigger exchangecontracts.BookCommit, set SandboxSagaMarketViewSet,
) (validatedSandboxSagaMarketCapture, crossExchangeCoherenceComparison) {
	comparison := crossExchangeCoherenceComparison{Trigger: trigger, Decision: set.Trigger,
		StrictB2:   crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCoherentMarketDataCoherentPolicy().Version},
		Actionable: crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCrossExchangeActionablePolicy().Version}}
	reader, err := NewSandboxSagaMarketInputReader(fixedSandboxSagaMarketViewSource{set: set})
	if err != nil {
		comparison.StrictB2.Reason, comparison.Actionable.Reason = "capture_failure", "capture_failure"
		return validatedSandboxSagaMarketCapture{}, comparison
	}
	strict, strictErr := reader.capture(ctx, keys, now)
	comparison.StrictB2 = coherenceVerdict(comparison.StrictB2.PolicyVersion, strict.coherent, strictErr)
	actionable, actionableErr := reader.captureCrossExchangeActionable(ctx, keys, now)
	comparison.Actionable = coherenceVerdict(comparison.Actionable.PolicyVersion, actionable.coherent, actionableErr)
	comparison.Members, comparison.ReceiveSkew, comparison.CorrectedOverlap, err =
		crossExchangeTimingEvidence(set)
	if err != nil && comparison.Actionable.Passed {
		comparison.Actionable.Passed = false
		comparison.Actionable.ViewID = ""
		comparison.Actionable.Reason = err.Error()
	}
	return strict, comparison
}

func coherenceVerdict(version string, view runtimecore.CoherentView, err error) crossExchangeCoherenceVerdict {
	verdict := crossExchangeCoherenceVerdict{PolicyVersion: version}
	if err == nil {
		verdict.Passed, verdict.ViewID = true, view.Identity()
		return verdict
	}
	var failure *runtimecore.Error
	if errors.As(err, &failure) {
		verdict.Reason = failure.Scope
	} else {
		verdict.Reason = "capture_failure"
	}
	return verdict
}

func crossExchangeTimingEvidence(set SandboxSagaMarketViewSet) (
	[]crossExchangeMemberTiming, time.Duration, time.Duration, error,
) {
	if len(set.Members) != 2 || set.Trigger.MonotonicNanos == 0 {
		return nil, 0, 0, errors.New("timing_evidence_missing")
	}
	members := append([]SandboxSagaMarketMember(nil), set.Members...)
	sort.Slice(members, func(left, right int) bool { return members[left].View.Exchange() < members[right].View.Exchange() })
	result := make([]crossExchangeMemberTiming, 0, 2)
	minimumReceive, maximumReceive := members[0].View.Observation().ReceivedOffsetNanos,
		members[0].View.Observation().ReceivedOffsetNanos
	latestStart, earliestEnd := time.Time{}, time.Time{}
	for _, member := range members {
		view, clock := member.View, member.Clock
		observation := view.Observation()
		if observation.ExchangeTime.IsZero() || observation.ExchangeTime.Location() != time.UTC || !clock.Eligible {
			return result, 0, 0, errors.New("exchange_time_missing")
		}
		if observation.ReceivedOffsetNanos > set.Trigger.MonotonicNanos {
			return result, 0, 0, errors.New("post_trigger")
		}
		start := observation.ReceivedAt.UTC.Add(clock.Offset - clock.Uncertainty)
		end := observation.ReceivedAt.UTC.Add(clock.Offset + clock.Uncertainty)
		minimumDelay, maximumDelay := start.Sub(observation.ExchangeTime), end.Sub(observation.ExchangeTime)
		if maximumDelay < 0 {
			return result, 0, 0, errors.New("exchange_time_future")
		}
		if maximumDelay > crossExchangeMaximumSourceDelay {
			return result, 0, 0, errors.New("source_delay")
		}
		result = append(result, crossExchangeMemberTiming{Exchange: view.Exchange(), BookVersion: view.Version(),
			ConnectionGeneration: view.Generation(),
			BookAge:              time.Duration(set.Trigger.MonotonicNanos - observation.ReceivedOffsetNanos),
			ExchangeTime:         observation.ExchangeTime, CorrectedReceiveEarliest: start,
			CorrectedReceiveLatest: end, SourceDelayMinimum: minimumDelay, SourceDelayMaximum: maximumDelay})
		if observation.ReceivedOffsetNanos < minimumReceive {
			minimumReceive = observation.ReceivedOffsetNanos
		}
		if observation.ReceivedOffsetNanos > maximumReceive {
			maximumReceive = observation.ReceivedOffsetNanos
		}
		if latestStart.IsZero() || start.After(latestStart) {
			latestStart = start
		}
		if earliestEnd.IsZero() || end.Before(earliestEnd) {
			earliestEnd = end
		}
	}
	return result, time.Duration(maximumReceive - minimumReceive), earliestEnd.Sub(latestStart), nil
}

func sameBookCommitView(commit exchangecontracts.BookCommit, view marketdata.BookView) bool {
	observation := view.Observation()
	return commit.Validate() == nil && commit.Exchange == view.Exchange() && commit.Instrument == view.Instrument() &&
		commit.ConnectionGeneration == view.Generation() && commit.BookVersion == view.Version() &&
		commit.IngestOrdinal == observation.IngestOrdinal &&
		commit.ReceivedOffsetNanos == observation.ReceivedOffsetNanos &&
		commit.PublishedOffsetNanos == observation.PublishedOffsetNanos
}
