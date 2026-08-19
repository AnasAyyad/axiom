package operationalReadiness

import "fmt"

// SourceFailure is a redacted, fixed-cardinality observation failure. It
// intentionally excludes URLs, payloads, database text, and raw errors.
type SourceFailure struct {
	Source    string `json:"source"`
	Stage     string `json:"stage"`
	Role      string `json:"role,omitempty"`
	Reason    string `json:"reason"`
	Retryable bool   `json:"retryable"`
}

func (failure *SourceFailure) Error() string {
	if failure == nil {
		return "operational_readiness_source_failure"
	}
	return fmt.Sprintf("operational_readiness_%s_%s", failure.Source, failure.Reason)
}

func sourceFailure(source, stage, role, reason string, retryable bool) error {
	return &SourceFailure{Source: source, Stage: stage, Role: role, Reason: reason, Retryable: retryable}
}

// SourceFailureDetails returns only the approved redacted diagnostic fields.
func SourceFailureDetails(err error) (SourceFailure, bool) {
	failure, ok := err.(*SourceFailure)
	if !ok || failure == nil {
		return SourceFailure{}, false
	}
	return *failure, true
}

// ProbeFailure describes why the runner could not consume a new rolling
// sample. SourceCause is copied from the observer's redacted status file when
// available; it never contains an arbitrary error string.
type ProbeFailure struct {
	Reason      string
	SourceCause SourceFailure
	Retryable   bool
}

func (failure *ProbeFailure) Error() string {
	if failure == nil {
		return "operational_readiness_probe_failure"
	}
	return "operational_readiness_probe_" + failure.Reason
}

func probeFailureDetails(err error) (ProbeFailure, bool) {
	failure, ok := err.(*ProbeFailure)
	if !ok || failure == nil {
		return ProbeFailure{}, false
	}
	return *failure, true
}
