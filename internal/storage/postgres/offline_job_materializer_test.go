package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/replay"
)

func TestOwnerConsoleOfflineRequestRequiresCompleteWindowAndSeed(t *testing.T) {
	valid := []byte(`{"configuration_id":"configuration-a","dataset_id":"dataset-a","research_generation_id":"generation-a","root_seed_hash":"` +
		strings.Repeat("a", 64) + `","strategy_version":"trend-following@1.0.0"}`)
	request, err := decodeOwnerConsoleOfflineRequest("backtest", valid)
	if err != nil || request.DatasetID != "dataset-a" {
		t.Fatalf("valid request = %#v %v", request, err)
	}
	var window map[string]any
	_ = json.Unmarshal(valid, &window)
	window["first_ordinal"] = "10"
	incomplete, _ := json.Marshal(window)
	if _, err = decodeOwnerConsoleOfflineRequest("replay", incomplete); err == nil {
		t.Fatal("incomplete replay window accepted")
	}
	window["last_ordinal"] = "11"
	window["incident_id"] = "incident-a"
	window["speed"] = "accelerated"
	replay, _ := json.Marshal(window)
	decoded, err := decodeOwnerConsoleOfflineRequest("replay", replay)
	if err != nil || decoded.IncidentID == nil || *decoded.IncidentID != "incident-a" {
		t.Fatalf("incident replay request = %#v %v", decoded, err)
	}
	window["unknown"] = true
	unknown, _ := json.Marshal(window)
	if _, err = decodeOwnerConsoleOfflineRequest("replay", unknown); err == nil {
		t.Fatal("unknown replay request field accepted by worker")
	}
}

func TestOwnerConsoleOfflineRequestAcceptsOnlyInstalledStrategyRuntimes(t *testing.T) {
	mean := []byte(`{"configuration_id":"configuration-b","dataset_id":"dataset-b","research_generation_id":"generation-b","root_seed_hash":"` +
		strings.Repeat("b", 64) + `","strategy_version":"mean-reversion@1.0.0"}`)
	request, err := decodeOwnerConsoleOfflineRequest("backtest", mean)
	if err != nil || request.StrategyVersion != "mean-reversion@1.0.0" {
		t.Fatalf("mean-reversion request = %#v %v", request, err)
	}
	for _, version := range []string{"triangular-arbitrage@1.0.0", "cross-exchange-arbitrage@1.0.0"} {
		payload := []byte(`{"configuration_id":"configuration-multileg","dataset_id":"dataset-multileg","research_generation_id":"generation-multileg","root_seed_hash":"` +
			strings.Repeat("d", 64) + `","strategy_version":"` + version + `"}`)
		request, err = decodeOwnerConsoleOfflineRequest("backtest", payload)
		if err != nil || request.StrategyVersion != version {
			t.Fatalf("multileg request version=%s request=%#v error=%v", version, request, err)
		}
	}
	unknown := []byte(`{"configuration_id":"configuration-c","dataset_id":"dataset-c","research_generation_id":"generation-c","root_seed_hash":"` +
		strings.Repeat("c", 64) + `","strategy_version":"triangular-arbitrage@1.0.1"}`)
	if _, err = decodeOwnerConsoleOfflineRequest("backtest", unknown); err == nil {
		t.Fatal("uninstalled strategy runtime was accepted")
	}
}

func TestOwnerConsoleRunManifestRequiresCleanEmbeddedBuildIdentity(t *testing.T) {
	originalCommit, originalDirty := buildinfo.Commit, buildinfo.Dirty
	originalGoSum, originalPNPM := buildinfo.GoSumHash, buildinfo.PNPMLockHash
	t.Cleanup(func() {
		buildinfo.Commit, buildinfo.Dirty = originalCommit, originalDirty
		buildinfo.GoSumHash, buildinfo.PNPMLockHash = originalGoSum, originalPNPM
	})
	hash := strings.Repeat("a", 64)
	buildinfo.Commit, buildinfo.Dirty = strings.Repeat("b", 40), "false"
	buildinfo.GoSumHash, buildinfo.PNPMLockHash = hash, hash
	request := ownerConsoleOfflineRequest{ConfigurationID: "configuration-a", DatasetID: "dataset-a",
		ResearchGenerationID: "generation-a", RootSeedHash: hash, StrategyVersion: "trend-following@1.0.0"}
	dataset := backtest.DatasetDescriptor{DatasetID: "recorder-a", ManifestHash: hash, Revision: 1,
		SourceCommit: strings.Repeat("c", 40), SchemaVersion: "dataset.v1", ParserVersion: "parser.v1",
		NormalizationVersion: "normalizer.v1", SegmentHashes: []string{hash}, RecordCount: 1,
		Complete: true, Confidence: backtest.ConfidenceB}
	namespace := backtest.ModelNamespace{ID: "namespace-a", MarketContext: "production-public",
		LiquidityDomain: "combined-a", FeeDomain: "fixed-bps-v1", LatencyDomain: "fixed-zero-v1", FillDomain: "fill-v1"}
	manifest, err := ownerConsoleRunManifest("backtest-a", "backtest", request, config.DefaultConfiguration(), strings.Repeat("6", 64), dataset, namespace,
		replay.MaximumTiming, 1)
	if err != nil || manifest.ResearchGenerationID != "generation-a" {
		t.Fatal(err)
	}
	if _, err = manifest.CanonicalHash(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	buildinfo.Dirty = "true"
	if _, err = ownerConsoleRunManifest("backtest-b", "backtest", request, config.DefaultConfiguration(), strings.Repeat("6", 64), dataset, namespace,
		replay.MaximumTiming, 1); err == nil {
		t.Fatal("dirty build identity accepted")
	}
}

func TestOfflineJobTimingUsesPersistedReplayMode(t *testing.T) {
	accelerated, maximum := "accelerated", "maximum"
	for _, test := range []struct {
		kind         string
		speed        *string
		mode         replay.TimingMode
		acceleration uint64
	}{
		{kind: "backtest", speed: nil, mode: replay.MaximumTiming, acceleration: 1},
		{kind: "replay", speed: nil, mode: replay.OriginalTiming, acceleration: 1},
		{kind: "replay", speed: &accelerated, mode: replay.AcceleratedTiming, acceleration: 10},
		{kind: "replay", speed: &maximum, mode: replay.MaximumTiming, acceleration: 1},
	} {
		mode, acceleration, err := offlineJobTiming(test.kind, test.speed)
		if err != nil || mode != test.mode || acceleration != test.acceleration {
			t.Fatalf("timing %s = %s/%d %v", test.kind, mode, acceleration, err)
		}
	}
}
