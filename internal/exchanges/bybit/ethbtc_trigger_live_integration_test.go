package bybit

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

var bybitTriggerLimits = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond,
	150 * time.Millisecond, 250 * time.Millisecond}

type bybitTriggerPoint struct{ offset, ordinal uint64 }
type bybitTriggerPolicyResult struct {
	Evaluations     int            `json:"evaluations"`
	Accepted        int            `json:"accepted"`
	AcceptedPercent float64        `json:"accepted_percent"`
	Rejections      map[string]int `json:"rejections"`
}
type bybitTriggerReport struct {
	Region              string                               `json:"region"`
	Duration            string                               `json:"duration"`
	ConnectionIDs       map[string]string                    `json:"connection_ids"`
	Generations         map[string]uint64                    `json:"generations"`
	ClockUncertainMS    float64                              `json:"clock_uncertainty_ms"`
	Messages            map[string]int                       `json:"messages"`
	SnapshotResets      map[string]int                       `json:"snapshot_resets"`
	SequenceRegressions map[string]int                       `json:"sequence_regressions"`
	ETHBTCTriggers      int                                  `json:"ethbtc_triggers"`
	CurrentPolicy       bybitTriggerPolicyResult             `json:"current_interval_policy"`
	AsOfPolicies        map[string]*bybitTriggerPolicyResult `json:"same_exchange_asof_policies"`
}
type bybitProbeStream struct {
	symbol     string
	instrument domain.Instrument
	stream     ObservedStream
}
type bybitProbeReceive struct {
	symbol   string
	observed exchangecontracts.ObservedStreamEvent
	err      error
}

// TestProductionPublicBybitETHBTCTriggeredAsOfProbe applies the Binance
// timing hypothesis to Bybit. It is public-only and cannot place orders.
func TestProductionPublicBybitETHBTCTriggeredAsOfProbe(t *testing.T) {
	if os.Getenv("AXIOM_BYBIT_ETHBTC_TRIGGER_LIVE") != "1" {
		t.Skip("AXIOM_BYBIT_ETHBTC_TRIGGER_LIVE=1 is required")
	}
	duration, region := bybitTriggerDuration(t), bybitTriggerRegion(t)
	ctx, cancel := context.WithTimeout(context.Background(), duration+45*time.Second)
	defer cancel()
	client, err := NewPublicClient(publicEndpointSet, &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	health := warmBybitTriggerClock(t, ctx, client)
	streams, keys := openBybitTriggerStreams(t, ctx, client)
	defer closeBybitTriggerStreams(streams)

	views := runtimecore.NewMarketViews()
	report := newBybitTriggerReport(region, duration, health)
	for _, source := range streams {
		key := runtimecore.MarketKey{Exchange: "bybit", Instrument: source.instrument}
		if err = views.ActivateGeneration(key, source.stream.Generation()); err != nil {
			t.Fatal(err)
		}
		report.ConnectionIDs[source.symbol] = source.stream.ConnectionID()
		report.Generations[source.symbol] = source.stream.Generation()
	}
	events := make(chan bybitProbeReceive, 32)
	for _, source := range streams {
		go receiveBybitProbe(ctx, source, events)
		go heartbeatBybitProbe(ctx, source, events)
	}
	latest := make(map[string]bybitTriggerPoint, 3)
	versions, sequences := make(map[string]uint64, 3), make(map[string]uint64, 3)
	var ordinal uint64
	var sequenceDefect bool
	var started time.Time
	nextProgress := time.Now().Add(5 * time.Second)
	t.Logf("BYBIT_ETHBTC_TRIGGER_START region=%s duration=%s clock_uncertainty=%s",
		region, duration, health.Uncertainty)
	for {
		result := <-events
		if result.err != nil {
			t.Fatalf("%s stream failed: %v", result.symbol, result.err)
		}
		observed := result.observed
		if observed.Event.Kind == exchangecontracts.StreamLifecycle {
			continue
		}
		if observed.Event.Kind != exchangecontracts.StreamDepth {
			t.Fatalf("%s stream returned unexpected event: %s", result.symbol, observed.Event.Kind)
		}
		instrument, sequence, stateHash, snapshot := bybitDepthIdentity(t, observed)
		symbol := instrument.Symbol()
		if symbol != result.symbol {
			t.Fatalf("stream %s returned %s", result.symbol, symbol)
		}
		report.Messages[symbol]++
		if previous := sequences[symbol]; previous != 0 {
			if snapshot {
				report.SnapshotResets[symbol]++
			} else if sequence <= previous {
				report.SequenceRegressions[symbol]++
				sequenceDefect = true
			}
		}
		sequences[symbol] = sequence
		ordinal++
		versions[symbol]++
		latest[symbol] = bybitTriggerPoint{offset: observed.ReceivedOffsetNanos, ordinal: ordinal}
		_, err = views.Publish(runtimecore.MarketViewInput{
			Key:         runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument},
			BookVersion: versions[symbol], ConnectionGeneration: observed.ConnectionGeneration,
			ReceiveMonotonicNanos: observed.ReceivedOffsetNanos, ReceiveUTC: observed.ReceivedAt.UTC,
			IngestOrdinal: ordinal, ClockOffset: health.Offset, ClockUncertainty: health.Uncertainty,
			StateHash: stateHash, CollectorInstance: "bybit-ethbtc-trigger-probe", CollectorRegion: region,
		})
		if err != nil {
			t.Fatal(err)
		}
		if started.IsZero() {
			if len(latest) < 3 {
				continue
			}
			started, nextProgress = time.Now(), time.Now().Add(5*time.Second)
			t.Logf("BYBIT_ETHBTC_TRIGGER_WARMUP_COMPLETE messages=%v", report.Messages)
		}
		if symbol == "ETHBTC" {
			report.ETHBTCTriggers++
			trigger := runtimecore.AsOfTrigger{MonotonicNanos: observed.ReceivedOffsetNanos,
				IngestOrdinal: ordinal, UTC: observed.ReceivedAt.UTC}
			_, currentErr := views.CoherentAsOf(keys, trigger,
				runtimecore.InitialCoherentMarketDataCoherentPolicy())
			recordBybitTriggerResult(&report.CurrentPolicy, bybitCoherenceRejection(currentErr))
			for _, limit := range bybitTriggerLimits {
				rejection := evaluateBybitTriggeredAsOf(latest,
					bybitTriggerPoint{offset: trigger.MonotonicNanos, ordinal: trigger.IngestOrdinal},
					health, sequenceDefect, limit)
				recordBybitTriggerResult(report.AsOfPolicies[limit.String()], rejection)
			}
		}
		now := time.Now()
		if !now.Before(nextProgress) {
			t.Logf("BYBIT_ETHBTC_TRIGGER_PROGRESS elapsed=%s triggers=%d current=%d asof=%s messages=%v resets=%v regressions=%v",
				now.Sub(started).Round(time.Second), report.ETHBTCTriggers, report.CurrentPolicy.Accepted,
				bybitAcceptedSummary(report.AsOfPolicies), report.Messages,
				report.SnapshotResets, report.SequenceRegressions)
			nextProgress = nextProgress.Add(5 * time.Second)
		}
		if now.Sub(started) >= duration {
			break
		}
	}
	finalizeBybitTriggerResult(&report.CurrentPolicy)
	for _, result := range report.AsOfPolicies {
		finalizeBybitTriggerResult(result)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BYBIT_ETHBTC_TRIGGER_RESULT %s", encoded)
}

func openBybitTriggerStreams(t *testing.T, ctx context.Context, client *PublicClient) ([]bybitProbeStream, []runtimecore.MarketKey) {
	t.Helper()
	pairs := [][2]domain.AssetSymbol{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}}
	streams := make([]bybitProbeStream, 0, 3)
	keys := make([]runtimecore.MarketKey, 0, 3)
	for _, pair := range pairs {
		instrument, err := domain.NewSpotInstrument(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		stream, err := client.SubscribeObserved(ctx, exchangecontracts.StreamRequest{
			Instrument: instrument, Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth}})
		if err != nil {
			closeBybitTriggerStreams(streams)
			t.Fatal(err)
		}
		streams = append(streams, bybitProbeStream{symbol: instrument.Symbol(), instrument: instrument, stream: stream})
		keys = append(keys, runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument})
	}
	return streams, keys
}

func receiveBybitProbe(ctx context.Context, source bybitProbeStream, events chan<- bybitProbeReceive) {
	for {
		observed, err := source.stream.ReceiveObserved(ctx)
		select {
		case events <- bybitProbeReceive{symbol: source.symbol, observed: observed, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func heartbeatBybitProbe(ctx context.Context, source bybitProbeStream, events chan<- bybitProbeReceive) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := source.stream.Ping(ctx); err != nil {
				select {
				case events <- bybitProbeReceive{symbol: source.symbol, err: err}:
				case <-ctx.Done():
				}
				return
			}
		}
	}
}

func bybitDepthIdentity(t *testing.T, observed exchangecontracts.ObservedStreamEvent) (domain.Instrument, uint64, string, bool) {
	t.Helper()
	if snapshot := observed.Event.Snapshot; snapshot != nil {
		return snapshot.Instrument, snapshot.LastSequence, snapshot.RawPayloadHash, true
	}
	if depth := observed.Event.Depth; depth != nil {
		return depth.Instrument, depth.LastSequence, depth.RawPayloadHash, false
	}
	t.Fatal("depth event has no snapshot or update")
	return domain.Instrument{}, 0, "", false
}

func evaluateBybitTriggeredAsOf(latest map[string]bybitTriggerPoint, trigger bybitTriggerPoint,
	health ClockHealth, sequenceDefect bool, limit time.Duration) string {
	if health.Uncertainty > 100*time.Millisecond {
		return "uncertainty"
	}
	if sequenceDefect {
		return "sequence"
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

func warmBybitTriggerClock(t *testing.T, ctx context.Context, client *PublicClient) ClockHealth {
	t.Helper()
	var health ClockHealth
	for sample := 1; sample <= 12; sample++ {
		var err error
		health, err = client.SampleServerTime(ctx)
		if err != nil {
			t.Fatalf("clock sample %d failed: %v", sample, err)
		}
		if sample == 1 || sample%3 == 0 {
			t.Logf("BYBIT_ETHBTC_TRIGGER_CLOCK sample=%d offset=%s uncertainty=%s client_eligible=%t policy_100ms=%t",
				sample, health.Offset, health.Uncertainty, health.Eligible, health.Uncertainty <= 100*time.Millisecond)
		}
	}
	return health
}

func newBybitTriggerReport(region string, duration time.Duration, health ClockHealth) bybitTriggerReport {
	report := bybitTriggerReport{Region: region, Duration: duration.String(),
		ConnectionIDs: make(map[string]string, 3), Generations: make(map[string]uint64, 3),
		ClockUncertainMS: float64(health.Uncertainty) / float64(time.Millisecond),
		Messages:         make(map[string]int, 3), SnapshotResets: make(map[string]int, 3),
		SequenceRegressions: make(map[string]int, 3), CurrentPolicy: newBybitTriggerPolicyResult(),
		AsOfPolicies: make(map[string]*bybitTriggerPolicyResult, len(bybitTriggerLimits))}
	for _, limit := range bybitTriggerLimits {
		result := newBybitTriggerPolicyResult()
		report.AsOfPolicies[limit.String()] = &result
	}
	return report
}

func newBybitTriggerPolicyResult() bybitTriggerPolicyResult {
	return bybitTriggerPolicyResult{Rejections: make(map[string]int)}
}
func recordBybitTriggerResult(result *bybitTriggerPolicyResult, rejection string) {
	result.Evaluations++
	if rejection == "" {
		result.Accepted++
		return
	}
	result.Rejections[rejection]++
}
func finalizeBybitTriggerResult(result *bybitTriggerPolicyResult) {
	if result.Evaluations > 0 {
		result.AcceptedPercent = float64(result.Accepted) * 100 / float64(result.Evaluations)
	}
}
func bybitAcceptedSummary(results map[string]*bybitTriggerPolicyResult) string {
	encoded, _ := json.Marshal(map[string]int{"50ms": results["50ms"].Accepted,
		"100ms": results["100ms"].Accepted, "150ms": results["150ms"].Accepted,
		"250ms": results["250ms"].Accepted})
	return string(encoded)
}
func bybitCoherenceRejection(err error) string {
	if err == nil {
		return ""
	}
	var failure *runtimecore.Error
	if errors.As(err, &failure) && failure.Code == "coherent_view_rejected" {
		return failure.Scope
	}
	return "unexpected"
}

func bybitTriggerDuration(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("AXIOM_BYBIT_ETHBTC_TRIGGER_DURATION")
	if value == "" {
		return 30 * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 5*time.Second || duration > 5*time.Minute {
		t.Fatalf("AXIOM_BYBIT_ETHBTC_TRIGGER_DURATION must be between 5s and 5m: %q", value)
	}
	return duration
}
func bybitTriggerRegion(t *testing.T) string {
	t.Helper()
	value := os.Getenv("AXIOM_BYBIT_ETHBTC_TRIGGER_REGION")
	if value == "" {
		return "unspecified"
	}
	if len(value) > 128 {
		t.Fatal("AXIOM_BYBIT_ETHBTC_TRIGGER_REGION is too long")
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			t.Fatalf("AXIOM_BYBIT_ETHBTC_TRIGGER_REGION is invalid: %q", value)
		}
	}
	return value
}
func closeBybitTriggerStreams(streams []bybitProbeStream) {
	for _, source := range streams {
		_ = source.stream.Close()
	}
}

func TestBybitETHBTCTriggeredAsOfPolicy(t *testing.T) {
	trigger := bybitTriggerPoint{offset: uint64(time.Second), ordinal: 3}
	latest := map[string]bybitTriggerPoint{
		"BTCUSDT": {offset: trigger.offset - uint64(80*time.Millisecond), ordinal: 1},
		"ETHUSDT": {offset: trigger.offset - uint64(40*time.Millisecond), ordinal: 2}, "ETHBTC": trigger}
	health := ClockHealth{Uncertainty: 5 * time.Millisecond}
	if got := evaluateBybitTriggeredAsOf(latest, trigger, health, false, 50*time.Millisecond); got != "age_or_skew" {
		t.Fatalf("50ms rejection = %q", got)
	}
	if got := evaluateBybitTriggeredAsOf(latest, trigger, health, false, 100*time.Millisecond); got != "" {
		t.Fatalf("100ms rejection = %q", got)
	}
	if got := evaluateBybitTriggeredAsOf(latest, trigger, ClockHealth{Uncertainty: 100*time.Millisecond + time.Nanosecond}, false, 100*time.Millisecond); got != "uncertainty" {
		t.Fatalf("uncertainty rejection = %q", got)
	}
	if got := evaluateBybitTriggeredAsOf(latest, trigger, health, true, 100*time.Millisecond); got != "sequence" {
		t.Fatalf("sequence rejection = %q", got)
	}
	latest["BTCUSDT"] = bybitTriggerPoint{offset: trigger.offset + 1, ordinal: trigger.ordinal + 1}
	if got := evaluateBybitTriggeredAsOf(latest, trigger, health, false, 100*time.Millisecond); got != "post_trigger" {
		t.Fatalf("post-trigger rejection = %q", got)
	}
}
