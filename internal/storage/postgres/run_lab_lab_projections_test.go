package postgres

import "testing"

func TestRunLabLabLifecycleCapabilitiesFollowDurableStateMachine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state                            string
		pause, resume, cancel, reproduce bool
	}{
		{state: "QUEUED", cancel: true},
		{state: "RUNNING", pause: true, cancel: true},
		{state: "PAUSE_REQUESTED", cancel: true},
		{state: "PAUSED", resume: true, cancel: true},
		{state: "CANCEL_REQUESTED"},
		{state: "CANCELED", reproduce: true},
		{state: "SUCCEEDED", reproduce: true},
		{state: "FAILED", reproduce: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.state, func(t *testing.T) {
			t.Parallel()
			got := runLabLabLifecycle(test.state)
			if got.Pause != test.pause || got.Resume != test.resume || got.Cancel != test.cancel ||
				got.Reproduce != test.reproduce || !got.Compare || !got.Export {
				t.Fatalf("unexpected capabilities for %s: %+v", test.state, got)
			}
		})
	}
}
