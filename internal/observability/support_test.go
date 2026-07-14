package observability

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSupportBundleCannotExposeCanaryOrRealTrading(t *testing.T) {
	const canary = "support-canary-secret-123456"
	var output bytes.Buffer
	err := WriteSupportBundle(&output, SupportSnapshot{
		SchemaVersion: "axiom.support.v1", GeneratedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Service: "api", Version: "dev", Commit: "commit-" + canary, GoVersion: "go1.26.5",
		Lifecycle: "READY_PAUSED", ExecutionMode: "shadow",
		Dependencies: []SupportDependency{{Name: "postgres", Status: "ready"}},
	}, canary)
	if err != nil {
		t.Fatal(err)
	}
	encoded := output.String()
	if strings.Contains(encoded, canary) || !strings.Contains(encoded, `"real_trading_enabled":false`) ||
		!strings.Contains(encoded, redactedValue) {
		t.Fatalf("unsafe support artifact: %s", encoded)
	}
}

func TestSupportBundleRejectsArbitraryDependencyAndLiveState(t *testing.T) {
	snapshot := SupportSnapshot{
		SchemaVersion: "axiom.support.v1", GeneratedAt: time.Now().UTC(), Service: "api",
		Lifecycle: "READY_PAUSED", ExecutionMode: "shadow",
		Dependencies: []SupportDependency{{Name: "user-123", Status: "ready"}},
	}
	if err := WriteSupportBundle(&bytes.Buffer{}, snapshot); err == nil {
		t.Fatal("arbitrary dependency accepted")
	}
	snapshot.Dependencies = nil
	snapshot.RealTradingEnabled = true
	if err := WriteSupportBundle(&bytes.Buffer{}, snapshot); err == nil {
		t.Fatal("real trading support state accepted")
	}
}
