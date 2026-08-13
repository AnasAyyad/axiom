package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
)

func TestCrossExchangeActionablePublicProbe(t *testing.T) {
	run := crossExchangeProbeConfiguration(t)
	components := newCrossExchangeProbeComponents(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startCrossExchangeProbeCollectors(ctx, components)
	run.warmupStarted = time.Now().UTC()
	if !awaitCrossExchangeProbeReady(ctx, components, 2*time.Minute) {
		cancel()
		awaitCrossExchangeProbeCollectors(t, done)
		t.Fatal("public collectors did not become timing-eligible within two minutes")
	}
	runCrossExchangeProbe(t, ctx, cancel, done, components, run)
}

type crossExchangeProbeRun struct {
	duration      time.Duration
	output        string
	region        string
	commit        string
	warmupStarted time.Time
}

func crossExchangeProbeConfiguration(t *testing.T) crossExchangeProbeRun {
	t.Helper()
	if os.Getenv("AXIOM_CROSS_EXCHANGE_PROBE_PUBLIC") != "1" {
		t.Skip("set AXIOM_CROSS_EXCHANGE_PROBE_PUBLIC=1 to run the public-only diagnostic")
	}
	run := crossExchangeProbeRun{duration: crossExchangeProbeDuration(t), output: crossExchangeProbeOutput(t),
		region: os.Getenv("AXIOM_CROSS_EXCHANGE_PROBE_REGION"), commit: os.Getenv("AXIOM_CROSS_EXCHANGE_PROBE_COMMIT")}
	if run.region == "" || len(run.commit) != 40 {
		t.Fatal("probe region and exact source commit are required")
	}
	return run
}

func runCrossExchangeProbe(t *testing.T, ctx context.Context, cancel context.CancelFunc, done <-chan error,
	components crossExchangeProbeComponents, run crossExchangeProbeRun,
) {
	t.Helper()
	started := time.Now().UTC()
	samplesPath := filepath.Join(run.output, "samples.ndjson")
	samples, err := os.OpenFile(samplesPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(samples)
	summary := newCrossExchangeProbeSummary(run.region, run.commit, run.duration, run.warmupStarted, started)
	updates := mergePublicShadowMarketUpdates(ctx, components.collectorList())
	deadline := time.NewTimer(run.duration)
	progress := time.NewTicker(time.Minute)
	defer deadline.Stop()
	defer progress.Stop()
	for {
		select {
		case <-deadline.C:
			cancel()
			awaitCrossExchangeProbeCollectors(t, done)
			if err = samples.Close(); err != nil {
				t.Fatal(err)
			}
			summary.finish(components, time.Now().UTC())
			writeCrossExchangeProbeSummary(t, run.output, summary)
			t.Logf("probe complete: samples=%d strict=%d actionable=%d output=%s",
				summary.Samples, summary.StrictPasses, summary.ActionablePasses, run.output)
			return
		case <-progress.C:
			t.Logf("probe progress: elapsed=%s samples=%d strict=%d actionable=%d",
				time.Since(started).Round(time.Second), summary.Samples, summary.StrictPasses, summary.ActionablePasses)
		case trigger := <-updates:
			if !components.session.consumeCrossExchangeTrigger(trigger) {
				summary.DuplicateTriggers++
				continue
			}
			comparison := components.compare(ctx, trigger, time.Now().UTC())
			if err = encoder.Encode(comparison); err != nil {
				t.Fatal(err)
			}
			summary.observe(comparison)
			if summary.Samples > 200_000 {
				t.Fatal("probe sample bound exceeded")
			}
		}
	}
}

type crossExchangeProbeComponents struct {
	session *ownerConsoleCrossExchangeShadowSession
	keys    []runtimecore.MarketKey
}

func newCrossExchangeProbeComponents(t *testing.T) crossExchangeProbeComponents {
	t.Helper()
	configuration := config.DefaultMultiStrategyConfiguration()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	keys := []runtimecore.MarketKey{{Exchange: "binance", Instrument: instrument},
		{Exchange: "bybit", Instrument: instrument}}
	clients, collectors := newCrossExchangeProbeCollectors(t, configuration, instrument, keys)
	maximum, _ := domain.ParseQuantity("1000000")
	session := &ownerConsoleCrossExchangeShadowSession{
		claim:   postgresProbeClaim(configuration),
		clients: clients, collectors: collectors,
		metadata:       make(map[runtimecore.MarketKey]domain.InstrumentMetadata, 2),
		maximum:        map[runtimecore.MarketKey]domain.Quantity{keys[0]: maximum, keys[1]: maximum},
		lastTrigger:    make(map[string]exchangecontracts.BookCommit, 2),
		coherenceStats: newCrossExchangeCoherenceStatistics(),
	}
	metadataContext, cancelMetadata := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelMetadata()
	for _, key := range keys {
		records, metadataErr := session.clients[key.Exchange].Instruments(metadataContext, []domain.Instrument{instrument})
		if metadataErr != nil || len(records) != 1 {
			t.Fatalf("%s metadata: %v", key.Exchange, metadataErr)
		}
		session.metadata[key] = records[0].Metadata
	}
	return crossExchangeProbeComponents{session: session, keys: keys}
}

func newCrossExchangeProbeCollectors(t *testing.T, configuration config.Configuration,
	instrument domain.Instrument, keys []runtimecore.MarketKey,
) (map[string]shadowPublicClient, map[runtimecore.MarketKey]shadowPublicCollector) {
	t.Helper()
	venues := make(map[string]config.ExchangeConfiguration, 2)
	for _, venue := range configuration.PublicExchanges() {
		venues[venue.ID] = venue
	}
	clock, monotonic := &domain.SystemClock{}, exchangecontracts.NewProcessMonotonicSource()
	binanceClient, err := binance.NewRecorderPublicClientWithMonotonic(venues["binance"].EndpointSet, clock, monotonic)
	if err != nil {
		t.Fatalf("binance public client: %v", err)
	}
	bybitClient, err := bybit.NewPublicClientWithMonotonic(venues["bybit"].EndpointSet, clock, monotonic)
	if err != nil {
		t.Fatalf("bybit public client: %v", err)
	}
	recorder := &crossExchangeProbeRecorder{}
	binanceConfig := binance.DefaultCollectorConfig(instrument)
	binanceConfig.BookDepth, binanceConfig.QueueCapacity = 1000, 16384
	binanceCollector, err := binance.NewInstrumentCollector(binanceConfig, binanceClient, recorder, clock)
	if err != nil {
		t.Fatalf("binance collector: %v", err)
	}
	bybitConfig := bybit.DefaultCollectorConfig(instrument)
	bybitConfig.BookDepth, bybitConfig.QueueCapacity = 1000, 16384
	bybitCollector, err := bybit.NewInstrumentCollector(bybitConfig, bybitClient, recorder, clock)
	if err != nil {
		t.Fatalf("bybit collector: %v", err)
	}
	return map[string]shadowPublicClient{"binance": binanceClient, "bybit": bybitClient},
		map[runtimecore.MarketKey]shadowPublicCollector{keys[0]: binanceCollector, keys[1]: bybitCollector}
}

func postgresProbeClaim(configuration config.Configuration) postgresstore.PublicShadowClaim {
	return postgresstore.PublicShadowClaim{ID: "cross-exchange-public-probe",
		StrategyID: "cross-exchange-arbitrage-1-0-0", StrategyVersion: "cross-exchange-arbitrage@1.0.0",
		ExchangeID: "binance", InstrumentID: "BTCUSDT", Configuration: configuration}
}

func (components crossExchangeProbeComponents) collectorList() []shadowPublicCollector {
	return []shadowPublicCollector{components.session.collectors[components.keys[0]],
		components.session.collectors[components.keys[1]]}
}

func (components crossExchangeProbeComponents) compare(ctx context.Context,
	trigger exchangecontracts.BookCommit, now time.Time,
) crossExchangeCoherenceComparison {
	source := ownerConsoleCrossExchangeMarketSource{session: components.session, trigger: trigger}
	set, err := source.CaptureSandboxSagaMarketViews(ctx, components.keys, now)
	if err != nil {
		return crossExchangeCoherenceComparison{Trigger: trigger,
			Strict: crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCoherentMarketDataCoherentPolicy().Version,
				Reason: "capture_failure"},
			Actionable: crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCrossExchangeActionablePolicy().Version,
				Reason: "capture_failure"}}
	}
	_, comparison := compareCrossExchangeCapture(ctx, components.keys, now, trigger, set)
	return comparison
}

func startCrossExchangeProbeCollectors(ctx context.Context, components crossExchangeProbeComponents) <-chan error {
	done := make(chan error, 2)
	for _, collector := range components.collectorList() {
		go func(value shadowPublicCollector) { done <- value.Run(ctx) }(collector)
	}
	return done
}

func awaitCrossExchangeProbeReady(ctx context.Context, components crossExchangeProbeComponents,
	timeout time.Duration,
) bool {
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			ready := true
			for _, key := range components.keys {
				collector := components.session.collectors[key]
				view, err := collectorBook(collector, key)
				ready = ready && err == nil && collector.HealthSnapshot().Eligible &&
					!view.Observation().ExchangeTime.IsZero()
			}
			if ready {
				return true
			}
		}
	}
}

func awaitCrossExchangeProbeCollectors(t *testing.T, done <-chan error) {
	t.Helper()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("collector stopped with error: %v", err)
		}
	}
}

type crossExchangeProbeRecorder struct{ ordinal atomic.Uint64 }

func (recorder *crossExchangeProbeRecorder) RecordPublicRaw(_ context.Context,
	value exchangecontracts.PublicRawRecord,
) (exchangecontracts.StreamRecordToken, error) {
	digest := sha256.Sum256(value.Raw)
	return exchangecontracts.StreamRecordToken{IngestOrdinal: recorder.ordinal.Add(1), PayloadHash: digest,
		ReceivedAt: value.ReceivedAt, MonotonicOffsetNanos: value.MonotonicOffsetNanos,
		ConnectionGeneration: value.ConnectionGeneration}, nil
}

func (*crossExchangeProbeRecorder) RecordPublicCanonical(context.Context,
	exchangecontracts.PublicCanonicalRecord,
) error {
	return nil
}
func (*crossExchangeProbeRecorder) RecordSourceGap(context.Context, exchangecontracts.SourceGap) error {
	return nil
}

type crossExchangeProbeSummary struct {
	Schema              string                                               `json:"schema_version"`
	Region              string                                               `json:"region"`
	SourceCommit        string                                               `json:"source_commit"`
	Formal              bool                                                 `json:"formal"`
	CredentialsUsed     bool                                                 `json:"credentials_used"`
	StartedAt           time.Time                                            `json:"started_at"`
	EndedAt             time.Time                                            `json:"ended_at"`
	Warmup              time.Duration                                        `json:"warmup_nanos"`
	RequestedDuration   time.Duration                                        `json:"requested_duration_nanos"`
	Samples             uint64                                               `json:"samples"`
	DuplicateTriggers   uint64                                               `json:"duplicate_triggers"`
	StrictPasses        uint64                                               `json:"strict_passes"`
	ActionablePasses    uint64                                               `json:"actionable_passes"`
	StrictRejections    map[string]uint64                                    `json:"strict_rejections"`
	ActionRejections    map[string]uint64                                    `json:"actionable_rejections"`
	Triggers            map[string]uint64                                    `json:"triggers"`
	FinalHealth         map[string]exchangecontracts.CollectorHealthSnapshot `json:"final_health"`
	ReceiveSkewP50      time.Duration                                        `json:"receive_skew_p50_nanos"`
	ReceiveSkewP95      time.Duration                                        `json:"receive_skew_p95_nanos"`
	ReceiveSkewP99      time.Duration                                        `json:"receive_skew_p99_nanos"`
	CorrectedOverlapP50 time.Duration                                        `json:"corrected_overlap_p50_nanos"`
	CorrectedOverlapP95 time.Duration                                        `json:"corrected_overlap_p95_nanos"`
	BookAgeP95          time.Duration                                        `json:"book_age_p95_nanos"`
	BookAgeP99          time.Duration                                        `json:"book_age_p99_nanos"`
	SourceDelayP95      time.Duration                                        `json:"source_delay_p95_nanos"`
	SourceDelayP99      time.Duration                                        `json:"source_delay_p99_nanos"`
	skews               []int64
	overlaps            []int64
	bookAges            []int64
	sourceDelays        []int64
}

func newCrossExchangeProbeSummary(region, commit string, duration time.Duration,
	warmup, started time.Time,
) *crossExchangeProbeSummary {
	return &crossExchangeProbeSummary{Schema: "axiom.cross-exchange-coherence-probe.v1", Region: region,
		SourceCommit: commit, StartedAt: started, Warmup: started.Sub(warmup), RequestedDuration: duration,
		StrictRejections: make(map[string]uint64), ActionRejections: make(map[string]uint64),
		Triggers: make(map[string]uint64), FinalHealth: make(map[string]exchangecontracts.CollectorHealthSnapshot)}
}

func (summary *crossExchangeProbeSummary) observe(comparison crossExchangeCoherenceComparison) {
	summary.Samples++
	summary.Triggers[comparison.Trigger.Exchange]++
	if comparison.Strict.Passed {
		summary.StrictPasses++
	} else {
		summary.StrictRejections[comparison.Strict.Reason]++
	}
	if comparison.Actionable.Passed {
		summary.ActionablePasses++
	} else {
		summary.ActionRejections[comparison.Actionable.Reason]++
	}
	if len(comparison.Members) == 2 {
		summary.skews = append(summary.skews, comparison.ReceiveSkew.Nanoseconds())
		summary.overlaps = append(summary.overlaps, comparison.CorrectedOverlap.Nanoseconds())
		for _, member := range comparison.Members {
			summary.bookAges = append(summary.bookAges, member.BookAge.Nanoseconds())
			summary.sourceDelays = append(summary.sourceDelays, member.SourceDelayMaximum.Nanoseconds())
		}
	}
}

func (summary *crossExchangeProbeSummary) finish(components crossExchangeProbeComponents, ended time.Time) {
	summary.EndedAt = ended
	for _, key := range components.keys {
		summary.FinalHealth[key.Exchange] = components.session.collectors[key].HealthSnapshot()
	}
	summary.ReceiveSkewP50 = time.Duration(probePercentile(summary.skews, 50))
	summary.ReceiveSkewP95 = time.Duration(probePercentile(summary.skews, 95))
	summary.ReceiveSkewP99 = time.Duration(probePercentile(summary.skews, 99))
	summary.CorrectedOverlapP50 = time.Duration(probePercentile(summary.overlaps, 50))
	summary.CorrectedOverlapP95 = time.Duration(probePercentile(summary.overlaps, 95))
	summary.BookAgeP95 = time.Duration(probePercentile(summary.bookAges, 95))
	summary.BookAgeP99 = time.Duration(probePercentile(summary.bookAges, 99))
	summary.SourceDelayP95 = time.Duration(probePercentile(summary.sourceDelays, 95))
	summary.SourceDelayP99 = time.Duration(probePercentile(summary.sourceDelays, 99))
}

func probePercentile(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(left, right int) bool { return copyValues[left] < copyValues[right] })
	index := (len(copyValues)*percentile + 99) / 100
	if index > 0 {
		index--
	}
	return copyValues[index]
}

func crossExchangeProbeDuration(t *testing.T) time.Duration {
	t.Helper()
	duration, err := time.ParseDuration(os.Getenv("AXIOM_CROSS_EXCHANGE_PROBE_DURATION"))
	if err != nil || duration < 10*time.Second || duration > 20*time.Minute {
		t.Fatal("probe duration must be between 10 seconds and 20 minutes")
	}
	return duration
}

func crossExchangeProbeOutput(t *testing.T) string {
	t.Helper()
	output := os.Getenv("AXIOM_CROSS_EXCHANGE_PROBE_OUTPUT")
	if output == "" || !filepath.IsAbs(output) {
		t.Fatal("an absolute probe output directory is required")
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	return output
}

func writeCrossExchangeProbeSummary(t *testing.T, output string, summary *crossExchangeProbeSummary) {
	t.Helper()
	payload, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "summary.json"), append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprintf("region=%s samples=%d strict=%d actionable=%d strict_rate=%.2f actionable_rate=%.2f\n",
		summary.Region, summary.Samples, summary.StrictPasses, summary.ActionablePasses,
		probeRate(summary.StrictPasses, summary.Samples), probeRate(summary.ActionablePasses, summary.Samples))
	if err = os.WriteFile(filepath.Join(output, "report.txt"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
}

func probeRate(passes, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(passes) * 100 / float64(total)
}
