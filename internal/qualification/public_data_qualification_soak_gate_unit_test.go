package qualification

import (
	"reflect"
	"testing"
)

func TestAppendFormalCollectorFailuresSeparatesSmokeFromQualification(t *testing.T) {
	t.Parallel()

	smoke := soakEvidence{Formal: false}
	appendFormalCollectorFailures(&smoke, "BTCUSDT", false, false)
	if len(smoke.Failures) != 0 {
		t.Fatalf("smoke must report live state without applying formal gates: %v", smoke.Failures)
	}

	formal := soakEvidence{Formal: true}
	appendFormalCollectorFailures(&formal, "BTCUSDT", false, false)
	want := []string{"BTCUSDT_slo_failed", "BTCUSDT_ineligible"}
	if !reflect.DeepEqual(formal.Failures, want) {
		t.Fatalf("formal failures=%v want=%v", formal.Failures, want)
	}
}

func TestAppendFormalCollectorFailuresAcceptsHealthyFormalCollector(t *testing.T) {
	t.Parallel()

	evidence := soakEvidence{Formal: true}
	appendFormalCollectorFailures(&evidence, "ETHUSDT", true, true)
	if len(evidence.Failures) != 0 {
		t.Fatalf("healthy formal collector failures=%v", evidence.Failures)
	}
}
