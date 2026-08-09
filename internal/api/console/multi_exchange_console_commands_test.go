package console

import (
	"testing"

	"axiom/internal/api/generated"
)

func TestMultiExchangeConsoleReplayFaultBoundaryAcceptsOnlyDeterministicSimulationSchedule(t *testing.T) {
	valid := generated.ReplayFaultRequest{Fault: generated.Latency, Ordinal: "42",
		DelayNanos: "1000000", ExpectedRevision: "0",
		Reason: "exercise deterministic latency recovery"}
	if !validMultiExchangeConsoleReplayFault(valid) {
		t.Fatal("valid deterministic latency schedule rejected")
	}
	for name, mutate := range map[string]func(*generated.ReplayFaultRequest){
		"zero ordinal":      func(value *generated.ReplayFaultRequest) { value.Ordinal = "0" },
		"ordinal overflow":  func(value *generated.ReplayFaultRequest) { value.Ordinal = "18446744073709551615" },
		"negative revision": func(value *generated.ReplayFaultRequest) { value.ExpectedRevision = "-1" },
		"missing delay":     func(value *generated.ReplayFaultRequest) { value.DelayNanos = "0" },
		"short reason":      func(value *generated.ReplayFaultRequest) { value.Reason = "short" },
		"unknown fault":     func(value *generated.ReplayFaultRequest) { value.Fault = "production_order" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			if validMultiExchangeConsoleReplayFault(changed) {
				t.Fatalf("unsafe replay fault accepted: %#v", changed)
			}
		})
	}
	disconnect := valid
	disconnect.Fault = generated.Disconnect
	disconnect.DelayNanos = "0"
	if !validMultiExchangeConsoleReplayFault(disconnect) {
		t.Fatal("zero-delay deterministic disconnect rejected")
	}
}
