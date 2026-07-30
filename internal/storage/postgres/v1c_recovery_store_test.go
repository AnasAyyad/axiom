package postgres

import (
	"strings"
	"testing"
)

func TestReconciledV1CTerminalStateRequiresTerminalFactOrNoCreateEvidence(
	t *testing.T,
) {
	for _, test := range []struct {
		name            string
		orderState      string
		createAttempted bool
		want            string
	}{
		{
			name:       "unsent unknown",
			orderState: "UNKNOWN",
			want:       "REJECTED",
		},
		{
			name:            "ambiguous create remains unknown",
			orderState:      "UNKNOWN",
			createAttempted: true,
		},
		{
			name:            "canceled",
			orderState:      "CANCELED",
			createAttempted: true,
			want:            "CANCELED",
		},
		{
			name:       "rejected",
			orderState: "REJECTED",
			want:       "REJECTED",
		},
		{
			name:       "filled uses private reducer",
			orderState: "FILLED",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := reconciledV1CTerminalState(
				test.orderState,
				test.createAttempted,
			); got != test.want {
				t.Fatalf("state=%q want=%q", got, test.want)
			}
		})
	}
}

func TestReconciledV1CTerminalSQLProvesCreateEvidenceBeforeRelease(
	t *testing.T,
) {
	normalized := strings.ToUpper(
		strings.Join(strings.Fields(loadV1CReconciledTerminalSQL), " "),
	)
	for _, required := range []string{
		"V1C_AUTHENTICATED_REQUEST_EVIDENCE",
		"EVIDENCE.METHOD='POST'",
		"'/API/V3/ORDER'",
		"'/V5/ORDER/CREATE'",
		"EVIDENCE.CONFIGURATION_ID=PLAN.CONFIGURATION_ID",
		"EVIDENCE.RECORDED_AT>=PLAN.APPROVED_AT",
		"EVIDENCE.RECORDED_AT<=$4",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("recovery SQL is missing %q", required)
		}
	}
}
