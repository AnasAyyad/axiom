package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"
)

// SupportDependency is one bounded dependency fact in a support artifact.
type SupportDependency struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// SupportSnapshot is the closed diagnostic artifact contract. It intentionally
// has no log, environment, URL, header, payload, path, or configuration fields.
type SupportSnapshot struct {
	SchemaVersion      string              `json:"schema_version"`
	GeneratedAt        time.Time           `json:"generated_at"`
	Service            string              `json:"service"`
	Version            string              `json:"version"`
	Commit             string              `json:"commit"`
	GoVersion          string              `json:"go_version"`
	Lifecycle          string              `json:"lifecycle"`
	ExecutionMode      string              `json:"execution_mode"`
	RealTradingEnabled bool                `json:"real_trading_enabled"`
	Dependencies       []SupportDependency `json:"dependencies"`
}

// WriteSupportBundle validates and redacts the small JSON artifact before any
// byte reaches output. Registered canary/secret literals are replaced even in
// build metadata.
func WriteSupportBundle(output io.Writer, snapshot SupportSnapshot, secrets ...string) error {
	if output == nil || snapshot.SchemaVersion != "axiom.support.v1" || snapshot.GeneratedAt.IsZero() || snapshot.GeneratedAt.Location() != time.UTC ||
		!slices.Contains([]string{"api", "engine-shadow", "recorder", "worker"}, snapshot.Service) ||
		!slices.Contains([]string{"STARTING", "READY_PAUSED", "STOPPING"}, snapshot.Lifecycle) ||
		!slices.Contains([]string{"backtest", "replay", "paper", "shadow"}, snapshot.ExecutionMode) || snapshot.RealTradingEnabled || len(snapshot.Dependencies) > 16 {
		return fmt.Errorf("support_bundle_rejected")
	}
	for index := range snapshot.Dependencies {
		dependency := snapshot.Dependencies[index]
		if !slices.Contains([]string{"postgres", "disk", "clock", "fencing", "books", "queues"}, dependency.Name) ||
			!slices.Contains([]string{"ready", "not_ready"}, dependency.Status) ||
			(dependency.ReasonCode != "" && dependency.ReasonCode != "required_dependency_unavailable") {
			return fmt.Errorf("support_bundle_rejected")
		}
	}
	redactor := &redactingHandler{secrets: filterSecrets(secrets)}
	snapshot.Version = redactor.redactText(snapshot.Version)
	snapshot.Commit = redactor.redactText(snapshot.Commit)
	snapshot.GoVersion = redactor.redactText(snapshot.GoVersion)
	return json.NewEncoder(output).Encode(snapshot)
}
