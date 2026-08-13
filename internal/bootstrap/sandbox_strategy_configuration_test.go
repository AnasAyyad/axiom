package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestDecodeSandboxStrategyConfigurationRequiresExactBoundSandboxRuntimeGraph(t *testing.T) {
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin, "test", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	work := sandbox.StrategySessionWork{SessionID: "session", Strategy: sandbox.StrategyTrend,
		Instrument: "BTCUSDT", Account: sandbox.StrategySessionAccount{ID: "binance-account", Epoch: 1, Exchange: sandbox.ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: snapshot.Hash(), StrategySetHash: strings.Repeat("a", 64),
		SessionRevision: 1, StrategyRevision: 1, ArmID: "arm", ArmRevision: 1,
		StartedAt: time.Now().UTC().Add(-time.Minute), ArmExpiresAt: time.Now().UTC().Add(time.Minute)}
	record := sandbox.StrategySessionConfiguration{ID: work.ConfigurationID, Hash: snapshot.Hash(), Payload: payload}
	now := time.Now().UTC()
	decoded, err := decodeSandboxStrategyConfiguration(work, record, now)
	if err != nil || decoded.Mode != config.ModeTestnet || decoded.SchemaVersion != config.SchemaVersionSandboxRuntime {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
	record.Hash = strings.Repeat("b", 64)
	if _, err = decodeSandboxStrategyConfiguration(work, record, now); err == nil {
		t.Fatal("mismatched configuration hash accepted")
	}
	if _, err = decodeSandboxStrategyConfiguration(work, record, work.ArmExpiresAt); err == nil {
		t.Fatal("expired strategy work was decoded")
	}
}
