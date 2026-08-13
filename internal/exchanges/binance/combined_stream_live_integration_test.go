package binance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

const (
	combinedTriangleDefaultDuration = 30 * time.Second
	combinedTriangleWarmupTimeout   = 30 * time.Second
	combinedTriangleProgressEvery   = 5 * time.Second
)

type combinedTriangleProbeReport struct {
	Region           string         `json:"region"`
	Duration         string         `json:"duration"`
	ConnectionID     string         `json:"connection_id"`
	Generation       uint64         `json:"generation"`
	ClockOffsetMS    float64        `json:"clock_offset_ms"`
	ClockUncertainMS float64        `json:"clock_uncertainty_ms"`
	Messages         map[string]int `json:"messages"`
	SequenceGaps     map[string]int `json:"sequence_gaps"`
	Evaluations      int            `json:"evaluations"`
	Accepted         int            `json:"accepted"`
	AcceptedPercent  float64        `json:"accepted_percent"`
	Rejections       map[string]int `json:"rejections"`
}

// TestProductionPublicBinanceCombinedTriangleCoherenceProbe is an explicitly
// enabled, public-data-only experiment. It measures one combined WebSocket and
// makes no qualification, profitability, fill, or atomic-snapshot claim.
func TestProductionPublicBinanceCombinedTriangleCoherenceProbe(t *testing.T) {
	if os.Getenv("AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE") != "1" {
		t.Skip("AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE=1 is required")
	}
	duration := combinedTriangleProbeDuration(t)
	region := combinedTriangleProbeRegion(t)
	ctx, cancel := context.WithTimeout(context.Background(), duration+combinedTriangleWarmupTimeout+15*time.Second)
	defer cancel()

	clock := &domain.SystemClock{}
	client, err := NewPublicClient(publicEndpointSet, clock)
	if err != nil {
		t.Fatal(err)
	}
	health := warmCombinedTriangleClock(t, ctx, client)
	requests, keys := combinedTriangleRequests(t)
	stream, err := client.SubscribeCombinedObserved(ctx, requests)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	views := runtimecore.NewMarketViews()
	for _, key := range keys {
		if err = views.ActivateGeneration(key, stream.Generation()); err != nil {
			t.Fatal(err)
		}
	}
	report := combinedTriangleProbeReport{Region: region, Duration: duration.String(),
		ConnectionID: stream.ConnectionID(), Generation: stream.Generation(),
		ClockOffsetMS: durationMilliseconds(health.Offset), ClockUncertainMS: durationMilliseconds(health.Uncertainty),
		Messages: make(map[string]int, 3), SequenceGaps: make(map[string]int, 3), Rejections: make(map[string]int)}
	versions, lastSequences := make(map[string]uint64, 3), make(map[string]uint64, 3)
	var ordinal uint64
	var measurementStarted time.Time
	nextProgress := time.Now().Add(combinedTriangleProgressEvery)
	t.Logf("BINANCE_COMBINED_TRIANGLE_START region=%s duration=%s connection=%s generation=%d clock_uncertainty=%s",
		region, duration, stream.ConnectionID(), stream.Generation(), health.Uncertainty)

	for {
		observed, receiveErr := stream.ReceiveObserved(ctx)
		if receiveErr != nil {
			t.Fatalf("combined stream receive failed: %v", receiveErr)
		}
		if observed.Event.Kind != exchangecontracts.StreamDepth || observed.Event.Depth == nil {
			t.Fatalf("combined stream returned unexpected event: %s", observed.Event.Kind)
		}
		depth := observed.Event.Depth
		symbol := depth.Instrument.Symbol()
		report.Messages[symbol]++
		ordinal++
		versions[symbol]++
		if previous := lastSequences[symbol]; previous != 0 &&
			!(depth.FirstSequence <= previous+1 && previous+1 <= depth.LastSequence) {
			report.SequenceGaps[symbol]++
			key := runtimecore.MarketKey{Exchange: "binance", Instrument: depth.Instrument}
			if gapErr := views.RecordGap(runtimecore.ViewGap{Key: key, Generation: stream.Generation(),
				FirstMonotonicNanos: observed.ReceivedOffsetNanos,
				LastMonotonicNanos:  observed.ReceivedOffsetNanos, Reason: "combined_probe_sequence_gap"}); gapErr != nil {
				t.Fatal(gapErr)
			}
		}
		lastSequences[symbol] = depth.LastSequence
		_, err = views.Publish(runtimecore.MarketViewInput{
			Key:         runtimecore.MarketKey{Exchange: "binance", Instrument: depth.Instrument},
			BookVersion: versions[symbol], ConnectionGeneration: stream.Generation(),
			ReceiveMonotonicNanos: observed.ReceivedOffsetNanos, ReceiveUTC: observed.ReceivedAt.UTC,
			IngestOrdinal: ordinal, ClockOffset: health.Offset, ClockUncertainty: health.Uncertainty,
			StateHash: depth.RawPayloadHash, CollectorInstance: "binance-combined-triangle-probe",
			CollectorRegion: region,
		})
		if err != nil {
			t.Fatal(err)
		}
		if measurementStarted.IsZero() {
			if len(versions) < 3 {
				continue
			}
			measurementStarted = time.Now()
			nextProgress = measurementStarted.Add(combinedTriangleProgressEvery)
			t.Logf("BINANCE_COMBINED_TRIANGLE_WARMUP_COMPLETE messages=%v", report.Messages)
		}

		report.Evaluations++
		_, coherentErr := views.CoherentAsOf(keys, runtimecore.AsOfTrigger{
			MonotonicNanos: observed.ReceivedOffsetNanos, IngestOrdinal: ordinal, UTC: observed.ReceivedAt.UTC,
		}, runtimecore.InitialCoherentMarketDataCoherentPolicy())
		if coherentErr == nil {
			report.Accepted++
		} else {
			report.Rejections[combinedTriangleRejection(coherentErr)]++
		}
		now := time.Now()
		if !now.Before(nextProgress) {
			t.Logf("BINANCE_COMBINED_TRIANGLE_PROGRESS elapsed=%s evaluations=%d accepted=%d rejections=%v messages=%v gaps=%v",
				now.Sub(measurementStarted).Round(time.Second), report.Evaluations, report.Accepted,
				report.Rejections, report.Messages, report.SequenceGaps)
			nextProgress = nextProgress.Add(combinedTriangleProgressEvery)
		}
		if now.Sub(measurementStarted) >= duration {
			break
		}
	}
	if report.Evaluations > 0 {
		report.AcceptedPercent = float64(report.Accepted) * 100 / float64(report.Evaluations)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BINANCE_COMBINED_TRIANGLE_RESULT %s", encoded)
}

func combinedTriangleProbeDuration(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("AXIOM_BINANCE_COMBINED_TRIANGLE_DURATION")
	if value == "" {
		return combinedTriangleDefaultDuration
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 5*time.Second || duration > 5*time.Minute {
		t.Fatalf("AXIOM_BINANCE_COMBINED_TRIANGLE_DURATION must be between 5s and 5m: %q", value)
	}
	return duration
}

func combinedTriangleProbeRegion(t *testing.T) string {
	t.Helper()
	region := os.Getenv("AXIOM_BINANCE_COMBINED_TRIANGLE_REGION")
	if region == "" {
		return "unspecified"
	}
	if len(region) > 128 {
		t.Fatalf("AXIOM_BINANCE_COMBINED_TRIANGLE_REGION is too long")
	}
	for _, character := range region {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			t.Fatalf("AXIOM_BINANCE_COMBINED_TRIANGLE_REGION is invalid: %q", region)
		}
	}
	return region
}

func warmCombinedTriangleClock(t *testing.T, ctx context.Context, client *PublicClient) TimeHealth {
	t.Helper()
	var health TimeHealth
	for sample := 1; sample <= 12; sample++ {
		var err error
		health, err = client.SampleServerTime(ctx)
		if err != nil {
			t.Fatalf("clock sample %d failed: %v", sample, err)
		}
		if sample == 1 || sample%3 == 0 {
			t.Logf("BINANCE_COMBINED_TRIANGLE_CLOCK sample=%d offset=%s uncertainty=%s client_eligible=%t policy_100ms=%t",
				sample, health.Offset, health.Uncertainty, health.Eligible, health.Uncertainty <= 100*time.Millisecond)
		}
	}
	return health
}

func combinedTriangleRequests(t *testing.T) ([]exchangecontracts.StreamRequest, []runtimecore.MarketKey) {
	t.Helper()
	assets := [][2]domain.AssetSymbol{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}}
	requests := make([]exchangecontracts.StreamRequest, 0, len(assets))
	keys := make([]runtimecore.MarketKey, 0, len(assets))
	for _, pair := range assets {
		instrument, err := domain.NewSpotInstrument(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, exchangecontracts.StreamRequest{Instrument: instrument,
			Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth}})
		keys = append(keys, runtimecore.MarketKey{Exchange: "binance", Instrument: instrument})
	}
	return requests, keys
}

func combinedTriangleRejection(err error) string {
	var failure *runtimecore.Error
	if errors.As(err, &failure) && failure.Code == "coherent_view_rejected" {
		return failure.Scope
	}
	return "unexpected"
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
