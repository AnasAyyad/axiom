package binance

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

var ethBTCTriggerLimits = []time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	150 * time.Millisecond,
	250 * time.Millisecond,
}

type ethBTCTriggerPoint struct {
	offset  uint64
	ordinal uint64
}

type ethBTCTriggerPolicyResult struct {
	Evaluations     int            `json:"evaluations"`
	Accepted        int            `json:"accepted"`
	AcceptedPercent float64        `json:"accepted_percent"`
	Rejections      map[string]int `json:"rejections"`
}

type ethBTCTriggerProbeReport struct {
	Region           string                                `json:"region"`
	Duration         string                                `json:"duration"`
	ConnectionID     string                                `json:"connection_id"`
	Generation       uint64                                `json:"generation"`
	ClockUncertainMS float64                               `json:"clock_uncertainty_ms"`
	Messages         map[string]int                        `json:"messages"`
	SequenceGaps     map[string]int                        `json:"sequence_gaps"`
	ETHBTCTriggers   int                                   `json:"ethbtc_triggers"`
	CurrentPolicy    ethBTCTriggerPolicyResult             `json:"current_interval_policy"`
	AsOfPolicies     map[string]*ethBTCTriggerPolicyResult `json:"same_exchange_asof_policies"`
}

// TestProductionPublicBinanceETHBTCTriggeredAsOfProbe compares the current
// generic coherent-view rule with a timing-only same-exchange as-of hypothesis.
// It is public-only, non-qualifying, and does not modify production behavior.
func TestProductionPublicBinanceETHBTCTriggeredAsOfProbe(t *testing.T) {
	if os.Getenv("AXIOM_BINANCE_ETHBTC_TRIGGER_LIVE") != "1" {
		t.Skip("AXIOM_BINANCE_ETHBTC_TRIGGER_LIVE=1 is required")
	}
	duration := ethBTCTriggerDuration(t)
	region := ethBTCTriggerRegion(t)
	ctx, cancel := context.WithTimeout(context.Background(), duration+combinedTriangleWarmupTimeout+15*time.Second)
	defer cancel()

	client, err := NewPublicClient(publicEndpointSet, &domain.SystemClock{})
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
	report := newETHBTCTriggerReport(region, duration, stream, health)
	versions, lastSequences := make(map[string]uint64, 3), make(map[string]uint64, 3)
	latest := make(map[string]ethBTCTriggerPoint, 3)
	var ordinal uint64
	var gapSeen bool
	var measurementStarted time.Time
	nextProgress := time.Now().Add(combinedTriangleProgressEvery)
	t.Logf("BINANCE_ETHBTC_TRIGGER_START region=%s duration=%s connection=%s generation=%d clock_uncertainty=%s",
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
			gapSeen = true
			key := runtimecore.MarketKey{Exchange: "binance", Instrument: depth.Instrument}
			if gapErr := views.RecordGap(runtimecore.ViewGap{Key: key, Generation: stream.Generation(),
				FirstMonotonicNanos: observed.ReceivedOffsetNanos,
				LastMonotonicNanos:  observed.ReceivedOffsetNanos, Reason: "ethbtc_trigger_probe_sequence_gap"}); gapErr != nil {
				t.Fatal(gapErr)
			}
		}
		lastSequences[symbol] = depth.LastSequence
		latest[symbol] = ethBTCTriggerPoint{offset: observed.ReceivedOffsetNanos, ordinal: ordinal}
		_, err = views.Publish(runtimecore.MarketViewInput{
			Key:         runtimecore.MarketKey{Exchange: "binance", Instrument: depth.Instrument},
			BookVersion: versions[symbol], ConnectionGeneration: stream.Generation(),
			ReceiveMonotonicNanos: observed.ReceivedOffsetNanos, ReceiveUTC: observed.ReceivedAt.UTC,
			IngestOrdinal: ordinal, ClockOffset: health.Offset, ClockUncertainty: health.Uncertainty,
			StateHash: depth.RawPayloadHash, CollectorInstance: "binance-ethbtc-trigger-probe",
			CollectorRegion: region,
		})
		if err != nil {
			t.Fatal(err)
		}
		if measurementStarted.IsZero() {
			if len(latest) < 3 {
				continue
			}
			measurementStarted = time.Now()
			nextProgress = measurementStarted.Add(combinedTriangleProgressEvery)
			t.Logf("BINANCE_ETHBTC_TRIGGER_WARMUP_COMPLETE messages=%v", report.Messages)
		}
		if symbol == "ETHBTC" {
			report.ETHBTCTriggers++
			trigger := runtimecore.AsOfTrigger{MonotonicNanos: observed.ReceivedOffsetNanos,
				IngestOrdinal: ordinal, UTC: observed.ReceivedAt.UTC}
			_, currentErr := views.CoherentAsOf(keys, trigger,
				runtimecore.InitialCoherentMarketDataCoherentPolicy())
			recordETHBTCTriggerResult(&report.CurrentPolicy, combinedTriangleRejection(currentErr))
			for _, limit := range ethBTCTriggerLimits {
				rejection := evaluateETHBTCTriggeredAsOf(latest,
					ethBTCTriggerPoint{offset: trigger.MonotonicNanos, ordinal: trigger.IngestOrdinal},
					health, gapSeen, limit)
				recordETHBTCTriggerResult(report.AsOfPolicies[limit.String()], rejection)
			}
		}
		now := time.Now()
		if !now.Before(nextProgress) {
			t.Logf("BINANCE_ETHBTC_TRIGGER_PROGRESS elapsed=%s triggers=%d current=%d asof=%s messages=%v gaps=%v",
				now.Sub(measurementStarted).Round(time.Second), report.ETHBTCTriggers,
				report.CurrentPolicy.Accepted, ethBTCTriggerAcceptedSummary(report.AsOfPolicies),
				report.Messages, report.SequenceGaps)
			nextProgress = nextProgress.Add(combinedTriangleProgressEvery)
		}
		if now.Sub(measurementStarted) >= duration {
			break
		}
	}
	finalizeETHBTCTriggerResult(&report.CurrentPolicy)
	for _, result := range report.AsOfPolicies {
		finalizeETHBTCTriggerResult(result)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BINANCE_ETHBTC_TRIGGER_RESULT %s", encoded)
}

func evaluateETHBTCTriggeredAsOf(latest map[string]ethBTCTriggerPoint, trigger ethBTCTriggerPoint,
	health TimeHealth, gapSeen bool, limit time.Duration,
) string {
	if health.Uncertainty > 100*time.Millisecond {
		return "uncertainty"
	}
	if gapSeen {
		return "gap"
	}
	if trigger.offset == 0 || trigger.ordinal == 0 || limit <= 0 {
		return "configuration"
	}
	minimum := trigger.offset
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		point, exists := latest[symbol]
		if !exists {
			return "missing"
		}
		if point.offset > trigger.offset || point.ordinal > trigger.ordinal {
			return "post_trigger"
		}
		if point.offset < minimum {
			minimum = point.offset
		}
	}
	if trigger.offset-minimum > uint64(limit.Nanoseconds()) {
		return "age_or_skew"
	}
	return ""
}

func newETHBTCTriggerReport(region string, duration time.Duration, stream ObservedStream,
	health TimeHealth,
) ethBTCTriggerProbeReport {
	report := ethBTCTriggerProbeReport{Region: region, Duration: duration.String(),
		ConnectionID: stream.ConnectionID(), Generation: stream.Generation(),
		ClockUncertainMS: durationMilliseconds(health.Uncertainty), Messages: make(map[string]int, 3),
		SequenceGaps: make(map[string]int, 3), CurrentPolicy: newETHBTCTriggerPolicyResult(),
		AsOfPolicies: make(map[string]*ethBTCTriggerPolicyResult, len(ethBTCTriggerLimits))}
	for _, limit := range ethBTCTriggerLimits {
		result := newETHBTCTriggerPolicyResult()
		report.AsOfPolicies[limit.String()] = &result
	}
	return report
}

func newETHBTCTriggerPolicyResult() ethBTCTriggerPolicyResult {
	return ethBTCTriggerPolicyResult{Rejections: make(map[string]int)}
}

func recordETHBTCTriggerResult(result *ethBTCTriggerPolicyResult, rejection string) {
	result.Evaluations++
	if rejection == "" {
		result.Accepted++
		return
	}
	result.Rejections[rejection]++
}

func finalizeETHBTCTriggerResult(result *ethBTCTriggerPolicyResult) {
	if result.Evaluations > 0 {
		result.AcceptedPercent = float64(result.Accepted) * 100 / float64(result.Evaluations)
	}
}

func ethBTCTriggerAcceptedSummary(results map[string]*ethBTCTriggerPolicyResult) string {
	encoded, _ := json.Marshal(map[string]int{"50ms": results["50ms"].Accepted,
		"100ms": results["100ms"].Accepted, "150ms": results["150ms"].Accepted,
		"250ms": results["250ms"].Accepted})
	return string(encoded)
}

func ethBTCTriggerDuration(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("AXIOM_BINANCE_ETHBTC_TRIGGER_DURATION")
	if value == "" {
		return combinedTriangleDefaultDuration
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 5*time.Second || duration > 5*time.Minute {
		t.Fatalf("AXIOM_BINANCE_ETHBTC_TRIGGER_DURATION must be between 5s and 5m: %q", value)
	}
	return duration
}

func ethBTCTriggerRegion(t *testing.T) string {
	t.Helper()
	value := os.Getenv("AXIOM_BINANCE_ETHBTC_TRIGGER_REGION")
	if value == "" {
		return "unspecified"
	}
	if len(value) > 128 {
		t.Fatal("AXIOM_BINANCE_ETHBTC_TRIGGER_REGION is too long")
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			t.Fatalf("AXIOM_BINANCE_ETHBTC_TRIGGER_REGION is invalid: %q", value)
		}
	}
	return value
}

func TestETHBTCTriggeredAsOfPolicy(t *testing.T) {
	trigger := ethBTCTriggerPoint{offset: uint64(time.Second), ordinal: 3}
	latest := map[string]ethBTCTriggerPoint{
		"BTCUSDT": {offset: trigger.offset - uint64(80*time.Millisecond), ordinal: 1},
		"ETHUSDT": {offset: trigger.offset - uint64(40*time.Millisecond), ordinal: 2},
		"ETHBTC":  trigger,
	}
	health := TimeHealth{Uncertainty: 5 * time.Millisecond}
	if got := evaluateETHBTCTriggeredAsOf(latest, trigger, health, false, 50*time.Millisecond); got != "age_or_skew" {
		t.Fatalf("50ms rejection = %q", got)
	}
	if got := evaluateETHBTCTriggeredAsOf(latest, trigger, health, false, 100*time.Millisecond); got != "" {
		t.Fatalf("100ms rejection = %q", got)
	}
	if got := evaluateETHBTCTriggeredAsOf(latest, trigger,
		TimeHealth{Uncertainty: 100*time.Millisecond + time.Nanosecond}, false, 100*time.Millisecond); got != "uncertainty" {
		t.Fatalf("uncertainty rejection = %q", got)
	}
	if got := evaluateETHBTCTriggeredAsOf(latest, trigger, health, true, 100*time.Millisecond); got != "gap" {
		t.Fatalf("gap rejection = %q", got)
	}
	latest["BTCUSDT"] = ethBTCTriggerPoint{offset: trigger.offset + 1, ordinal: trigger.ordinal + 1}
	if got := evaluateETHBTCTriggeredAsOf(latest, trigger, health, false, 100*time.Millisecond); got != "post_trigger" {
		t.Fatalf("post-trigger rejection = %q", got)
	}
}
