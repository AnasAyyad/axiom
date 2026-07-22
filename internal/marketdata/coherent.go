package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

const maximumCoherentClockAge = 30 * time.Second

// CoherentInput converts one healthy immutable book and bounded clock sample
// into the complete evidence required by the B2 coherent-view authority.
func CoherentInput(
	view BookView,
	clock exchangecontracts.ClockHealth,
	collectorInstance, collectorRegion string,
) (runtimecore.MarketViewInput, error) {
	observation := view.Observation()
	receivedAt := observation.ReceivedAt.UTC
	if view.Health() != HealthHealthy || view.Version() == 0 || view.Generation() == 0 ||
		observation.Validate() != nil || !clock.Eligible || clock.ObservedAt.IsZero() ||
		clock.ObservedAt.Location() != time.UTC || clock.Uncertainty < 0 || receivedAt.Before(clock.ObservedAt) ||
		receivedAt.Sub(clock.ObservedAt) > maximumCoherentClockAge {
		return runtimecore.MarketViewInput{}, marketError("coherent_view_evidence_invalid")
	}
	canonical, err := view.MarshalJSON()
	if err != nil {
		return runtimecore.MarketViewInput{}, marketError("coherent_view_hash_failed")
	}
	digest := sha256.Sum256(canonical)
	return runtimecore.MarketViewInput{
		Key:         runtimecore.MarketKey{Exchange: view.Exchange(), Instrument: view.Instrument()},
		BookVersion: view.Version(), ConnectionGeneration: view.Generation(),
		ReceiveMonotonicNanos: observation.ReceivedOffsetNanos, ReceiveUTC: receivedAt,
		IngestOrdinal: observation.IngestOrdinal, ClockOffset: clock.Offset,
		ClockUncertainty: clock.Uncertainty, StateHash: hex.EncodeToString(digest[:]),
		CollectorInstance: collectorInstance, CollectorRegion: collectorRegion,
	}, nil
}
