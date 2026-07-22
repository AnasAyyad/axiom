package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

const initialB2CoherentPolicyVersion = "axiom.coherent-view-policy.v1"

// CoherentPolicy is an immutable versioned cross-market eligibility policy.
type CoherentPolicy struct {
	Version                 string        `json:"version"`
	MaximumBookAge          time.Duration `json:"maximum_book_age_nanos"`
	MaximumInterBookSkew    time.Duration `json:"maximum_inter_book_skew_nanos"`
	MaximumClockUncertainty time.Duration `json:"maximum_clock_uncertainty_nanos"`
}

// InitialB2CoherentPolicy returns the exact initial B2 limits.
func InitialB2CoherentPolicy() CoherentPolicy {
	return CoherentPolicy{Version: initialB2CoherentPolicyVersion, MaximumBookAge: 250 * time.Millisecond,
		MaximumInterBookSkew: 250 * time.Millisecond, MaximumClockUncertainty: 100 * time.Millisecond}
}

// AsOfTrigger identifies the deterministic decision boundary.
type AsOfTrigger struct {
	MonotonicNanos uint64    `json:"monotonic_nanos"`
	IngestOrdinal  uint64    `json:"ingest_ordinal"`
	UTC            time.Time `json:"utc"`
}

// ViewGap is an unresolved connection-generation interval that fails closed.
type ViewGap struct {
	Key                 MarketKey `json:"key"`
	Generation          uint64    `json:"generation"`
	FirstMonotonicNanos uint64    `json:"first_monotonic_nanos"`
	LastMonotonicNanos  uint64    `json:"last_monotonic_nanos"`
	Reason              string    `json:"reason"`
}

// CoherentView is one immutable deterministic as-of join result.
type CoherentView struct {
	identity string
	policy   CoherentPolicy
	trigger  AsOfTrigger
	members  []ViewReference
}

// Identity returns the canonical cross-market-view identity.
func (view CoherentView) Identity() string { return view.identity }

// VersionVectorHash returns the canonical member-vector SHA-256.
func (view CoherentView) VersionVectorHash() string { return view.identity }

// Policy returns the immutable policy used for eligibility.
func (view CoherentView) Policy() CoherentPolicy { return view.policy }

// Trigger returns the deterministic as-of boundary.
func (view CoherentView) Trigger() AsOfTrigger { return view.trigger }

// Members returns a defensive canonical-order copy.
func (view CoherentView) Members() []ViewReference {
	return append([]ViewReference(nil), view.members...)
}

// RecordGap marks one active generation ineligible until a later generation is activated.
func (views *MarketViews) RecordGap(gap ViewGap) error {
	if !validMarketKey(gap.Key) || gap.Generation == 0 || gap.FirstMonotonicNanos == 0 ||
		gap.LastMonotonicNanos < gap.FirstMonotonicNanos || !validViewLabel(gap.Reason) {
		return runtimeError("market_view_gap_rejected", "identity")
	}
	identity := marketKeyString(gap.Key)
	views.mutex.Lock()
	defer views.mutex.Unlock()
	if views.activeGenerations[identity] != gap.Generation {
		return runtimeError("market_view_gap_rejected", identity)
	}
	prior := views.gaps[identity]
	if len(prior) > 0 && gap.FirstMonotonicNanos <= prior[len(prior)-1].LastMonotonicNanos {
		return runtimeError("market_view_gap_rejected", "overlap")
	}
	views.gaps[identity] = append(prior, gap)
	return nil
}

// CoherentAsOf selects the latest committed eligible view per requested key.
func (views *MarketViews) CoherentAsOf(
	keys []MarketKey,
	trigger AsOfTrigger,
	policy CoherentPolicy,
) (CoherentView, error) {
	if len(keys) < 2 || !validTrigger(trigger) || !validCoherentPolicy(policy) {
		return CoherentView{}, runtimeError("coherent_view_rejected", "configuration")
	}
	views.mutex.RLock()
	defer views.mutex.RUnlock()
	members := make([]ViewReference, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		identity := marketKeyString(key)
		if !validMarketKey(key) {
			return CoherentView{}, runtimeError("coherent_view_rejected", "identity")
		}
		if _, duplicate := seen[identity]; duplicate {
			return CoherentView{}, runtimeError("coherent_view_rejected", "duplicate")
		}
		seen[identity] = struct{}{}
		view, err := views.eligibleAsOf(identity, trigger, policy)
		if err != nil {
			return CoherentView{}, err
		}
		members = append(members, referenceForView(view))
	}
	sort.Slice(members, func(left, right int) bool {
		return lessMarketKey(members[left].Key, members[right].Key)
	})
	if err := validateCoherentMembers(members, policy); err != nil {
		return CoherentView{}, err
	}
	encoded, _ := json.Marshal(members)
	digest := sha256.Sum256(encoded)
	identity := hex.EncodeToString(digest[:])
	return CoherentView{identity: identity, policy: policy, trigger: trigger, members: members}, nil
}

func (views *MarketViews) eligibleAsOf(
	identity string,
	trigger AsOfTrigger,
	policy CoherentPolicy,
) (MarketView, error) {
	view, err := views.selectAsOf(identity, trigger)
	if err != nil {
		return MarketView{}, err
	}
	if view.ConnectionGeneration() != views.activeGenerations[identity] {
		return MarketView{}, runtimeError("coherent_view_rejected", "generation")
	}
	if views.hasUnresolvedGap(identity, view.ConnectionGeneration(), trigger.MonotonicNanos) {
		return MarketView{}, runtimeError("coherent_view_rejected", "gap")
	}
	if trigger.MonotonicNanos-view.ReceiveMonotonicNanos() > uint64(policy.MaximumBookAge.Nanoseconds()) {
		return MarketView{}, runtimeError("coherent_view_rejected", "stale")
	}
	if view.ClockUncertainty() > policy.MaximumClockUncertainty {
		return MarketView{}, runtimeError("coherent_view_rejected", "uncertainty")
	}
	return view, nil
}

// RestoreCoherentView validates immutable persisted header and member evidence.
func RestoreCoherentView(
	identity string,
	policy CoherentPolicy,
	trigger AsOfTrigger,
	members []ViewReference,
) (CoherentView, error) {
	if len(identity) != sha256.Size*2 || len(members) < 2 || !validTrigger(trigger) || !validCoherentPolicy(policy) {
		return CoherentView{}, runtimeError("coherent_view_restore_rejected", "configuration")
	}
	if _, err := hex.DecodeString(identity); err != nil {
		return CoherentView{}, runtimeError("coherent_view_restore_rejected", "identity")
	}
	copyMembers := append([]ViewReference(nil), members...)
	for index, member := range copyMembers {
		input := MarketViewInput(member)
		if validateMarketView(input) != nil || (index > 0 && !lessMarketKey(copyMembers[index-1].Key, member.Key)) {
			return CoherentView{}, runtimeError("coherent_view_restore_rejected", "member")
		}
	}
	if err := validateCoherentMembers(copyMembers, policy); err != nil {
		return CoherentView{}, runtimeError("coherent_view_restore_rejected", "eligibility")
	}
	for _, member := range copyMembers {
		if member.ReceiveMonotonicNanos > trigger.MonotonicNanos ||
			trigger.MonotonicNanos-member.ReceiveMonotonicNanos > uint64(policy.MaximumBookAge.Nanoseconds()) ||
			member.ClockUncertainty > policy.MaximumClockUncertainty ||
			member.IngestOrdinal > trigger.IngestOrdinal {
			return CoherentView{}, runtimeError("coherent_view_restore_rejected", "eligibility")
		}
	}
	encoded, _ := json.Marshal(copyMembers)
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != identity {
		return CoherentView{}, runtimeError("coherent_view_restore_rejected", "hash")
	}
	return CoherentView{identity: identity, policy: policy, trigger: trigger, members: copyMembers}, nil
}

func (views *MarketViews) selectAsOf(identity string, trigger AsOfTrigger) (MarketView, error) {
	history := views.history[identity]
	for index := len(history) - 1; index >= 0; index-- {
		view := history[index]
		if view.ReceiveMonotonicNanos() <= trigger.MonotonicNanos && view.IngestOrdinal() <= trigger.IngestOrdinal {
			return view, nil
		}
	}
	if len(history) > 0 {
		return MarketView{}, runtimeError("coherent_view_rejected", "post_trigger")
	}
	return MarketView{}, runtimeError("coherent_view_rejected", "missing")
}

func (views *MarketViews) hasUnresolvedGap(identity string, generation, trigger uint64) bool {
	for _, gap := range views.gaps[identity] {
		if gap.Generation == generation && gap.FirstMonotonicNanos <= trigger {
			return true
		}
	}
	return false
}

func validateCoherentMembers(members []ViewReference, policy CoherentPolicy) error {
	minimumReceive, maximumReceive := members[0].ReceiveMonotonicNanos, members[0].ReceiveMonotonicNanos
	latestStart := members[0].ReceiveUTC.Add(members[0].ClockOffset - members[0].ClockUncertainty)
	earliestEnd := members[0].ReceiveUTC.Add(members[0].ClockOffset + members[0].ClockUncertainty)
	for _, member := range members[1:] {
		if member.ReceiveMonotonicNanos < minimumReceive {
			minimumReceive = member.ReceiveMonotonicNanos
		}
		if member.ReceiveMonotonicNanos > maximumReceive {
			maximumReceive = member.ReceiveMonotonicNanos
		}
		start := member.ReceiveUTC.Add(member.ClockOffset - member.ClockUncertainty)
		end := member.ReceiveUTC.Add(member.ClockOffset + member.ClockUncertainty)
		if start.After(latestStart) {
			latestStart = start
		}
		if end.Before(earliestEnd) {
			earliestEnd = end
		}
	}
	if maximumReceive-minimumReceive > uint64(policy.MaximumInterBookSkew.Nanoseconds()) {
		return runtimeError("coherent_view_rejected", "skew")
	}
	if latestStart.After(earliestEnd) {
		return runtimeError("coherent_view_rejected", "interval")
	}
	return nil
}

func validTrigger(trigger AsOfTrigger) bool {
	return trigger.MonotonicNanos > 0 && trigger.IngestOrdinal > 0 && !trigger.UTC.IsZero() &&
		trigger.UTC.Location() == time.UTC
}

func validCoherentPolicy(policy CoherentPolicy) bool {
	return validViewLabel(policy.Version) && policy.MaximumBookAge > 0 && policy.MaximumInterBookSkew > 0 &&
		policy.MaximumClockUncertainty > 0
}
