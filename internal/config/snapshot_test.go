package config

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestSnapshotHashReproducesExactConfiguration(t *testing.T) {
	clock := replayClock(t)
	configuration := DefaultConfiguration()
	first, err := NewSnapshot(configuration, SourceDefault, "bootstrap", clock)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSnapshot(configuration, SourceFile, "operator", clock)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() != second.Hash() || !first.EqualConfiguration(second) {
		t.Fatal("identical configuration did not produce an identical canonical hash")
	}
	reproduced, err := json.Marshal(first.Configuration())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeJSON(reproduced)
	if err != nil {
		t.Fatal(err)
	}
	third, err := NewSnapshot(decoded, SourceFile, "replay", clock)
	if err != nil || third.Hash() != first.Hash() {
		t.Fatalf("reproduced hash = %q, %v", third.Hash(), err)
	}
}

func TestSnapshotIsDefensivelyImmutable(t *testing.T) {
	configuration := DefaultConfiguration()
	snapshot, err := NewSnapshot(configuration, SourceDefault, "bootstrap", replayClock(t))
	if err != nil {
		t.Fatal(err)
	}
	configuration.Assets[0].Status = domain.AssetBlocked
	read := snapshot.Configuration()
	read.Assets[0].Status = domain.AssetBlocked
	if snapshot.Configuration().Assets[0].Status != domain.AssetApproved {
		t.Fatal("snapshot changed through an external slice")
	}
}

func TestManagerPublishesAtomicallyAndRecordsHistory(t *testing.T) {
	manager := &Manager{}
	clock := replayClock(t)
	first, err := manager.Publish(DefaultConfiguration(), SourceDefault, "bootstrap", clock)
	if err != nil {
		t.Fatal(err)
	}
	configuration := DefaultConfiguration()
	configuration.Revision = 2
	configuration.Risk.MaximumOrderNotional.Value = "900"
	second, err := manager.Publish(configuration, SourceAdmin, "risk-operator", clock)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() == second.Hash() {
		t.Fatal("changed graph retained old hash")
	}
	if _, err := manager.Publish(configuration, SourceAdmin, "risk-operator", clock); configurationErrorCode(err) != "stale_configuration" {
		t.Fatalf("stale publish error = %v", err)
	}
	current, ok := manager.Current()
	if !ok || current.Configuration().Revision != 2 {
		t.Fatalf("current = %#v, %t", current, ok)
	}
	history := manager.History()
	if len(history) != 2 || history[1].Actor != "risk-operator" || history[1].Source != SourceAdmin {
		t.Fatalf("history = %#v", history)
	}
	if len(history[1].Changes) != 2 || history[1].Changes[1] != "risk" {
		t.Fatalf("history changes = %#v", history[1].Changes)
	}
}

func TestReloadAllowsOnlyAtomicSafeChanges(t *testing.T) {
	manager := &Manager{}
	clock := replayClock(t)
	if _, err := manager.Publish(DefaultConfiguration(), SourceDefault, "bootstrap", clock); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		alter func(*Configuration)
		code  string
	}{
		{name: "mode", alter: func(c *Configuration) { c.Mode = ModeReplay }, code: "restart_required"},
		{name: "endpoint", alter: func(c *Configuration) { c.Endpoint.Set = "other" }, code: "endpoint_rejected"},
		{name: "risk loosening", alter: func(c *Configuration) { c.Risk.MaximumOrderNotional.Value = "2000" }, code: "risk_loosening_rejected"},
		{name: "instrument addition", alter: func(c *Configuration) {
			c.Instruments = append(c.Instruments, Instrument{Base: "ETH", Quote: "BTC", Product: "spot"})
		}, code: "reload_rejected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := DefaultConfiguration()
			next.Revision = 2
			test.alter(&next)
			if _, err := manager.Publish(next, SourceAdmin, "operator", clock); configurationErrorCode(err) != test.code {
				t.Fatalf("reload error = %v", err)
			}
		})
	}
}

func TestConcurrentSnapshotReadersSeeCompleteGraph(t *testing.T) {
	manager := &Manager{}
	if _, err := manager.Publish(DefaultConfiguration(), SourceDefault, "bootstrap", replayClock(t)); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			snapshot, ok := manager.Current()
			if !ok || len(snapshot.Configuration().Assets) != 3 || snapshot.Hash() == "" {
				t.Errorf("reader observed incomplete snapshot")
			}
		}()
	}
	group.Wait()
}

func replayClock(t *testing.T) domain.Clock {
	t.Helper()
	clock, err := domain.NewReplayClock(time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return clock
}
