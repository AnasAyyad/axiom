package crossarb

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	runtimecore "axiom/internal/runtime"
)

// ValidateCoherentBooks proves that executable depth is exactly the complete
// two-member B2 as-of vector, not merely a similarly named later snapshot.
func ValidateCoherentBooks(
	view runtimecore.CoherentView,
	markets []Market,
	decisionOffset uint64,
	configuration Configuration,
) error {
	policy := view.Policy()
	members := view.Members()
	trigger := view.Trigger()
	if view.Identity() == "" || len(members) != 2 || len(markets) != 2 ||
		trigger.MonotonicNanos != decisionOffset ||
		policy.MaximumBookAge > configuration.MaximumBookAge ||
		policy.MaximumInterBookSkew > configuration.MaximumInterBookSkew ||
		policy.MaximumClockUncertainty > configuration.MaximumClockUncertainty {
		return strategyError("coherent_view_invalid")
	}
	seen := map[string]bool{"binance": false, "bybit": false}
	region := ""
	for _, member := range members {
		market, ok := matchingMarket(member, markets)
		if !ok || seen[member.Key.Exchange] || !market.Book.Eligible(decisionOffset, configuration.MaximumBookAge) ||
			!exactBookReference(member, market) {
			return strategyError("coherent_member_mismatch")
		}
		if region == "" {
			region = member.CollectorRegion
		} else if member.CollectorRegion != region {
			return strategyError("coherent_region_incomparable")
		}
		seen[member.Key.Exchange] = true
	}
	if !seen["binance"] || !seen["bybit"] {
		return strategyError("coherent_membership_invalid")
	}
	return nil
}

func matchingMarket(reference runtimecore.ViewReference, markets []Market) (Market, bool) {
	for _, market := range markets {
		if market.Book.Exchange() == reference.Key.Exchange &&
			market.Book.Instrument() == reference.Key.Instrument {
			return market, true
		}
	}
	return Market{}, false
}

func exactBookReference(reference runtimecore.ViewReference, market Market) bool {
	book := market.Book
	observation := book.Observation()
	canonical, err := book.MarshalJSON()
	if err != nil {
		return false
	}
	digest := sha256.Sum256(canonical)
	return reference.BookVersion == book.Version() &&
		reference.ConnectionGeneration == book.Generation() &&
		reference.ReceiveMonotonicNanos == observation.ReceivedOffsetNanos &&
		reference.ReceiveUTC.Equal(observation.ReceivedAt.UTC) &&
		reference.IngestOrdinal == observation.IngestOrdinal &&
		reference.StateHash == hex.EncodeToString(digest[:]) &&
		reference.CollectorInstance != "" && reference.CollectorRegion != "" &&
		reference.ReceiveUTC.Location() == time.UTC
}
