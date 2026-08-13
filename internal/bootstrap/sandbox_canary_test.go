package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/sandbox"
)

func TestSandboxCanaryInputIsStrictAndProtected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request")
	input := `{"email":"owner@example.test","password":"secret-value","totp":"012345","reason":"bounded canary","instrument":"BTCUSDT","side":"buy","quantity":"0.001","limit_price":"10000","style":"LIMIT_GTC"}`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := readSandboxCanaryRequest(path)
	if err != nil || request.TOTP != "012345" ||
		request.Instrument != "BTCUSDT" {
		t.Fatalf("request=%#v error=%v", request, err)
	}
	if err = os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = readSandboxCanaryRequest(path); err == nil {
		t.Fatal("other-readable canary request accepted")
	}
}

func TestSandboxCanaryEvidenceIsAtomicAndNeverOverwritten(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	evidence := sandboxCanaryQualificationEvidence{
		SchemaVersion: "axiom.sandbox_runtime.sandbox_connectivity.canary-evidence.v1",
		CanaryID:      "execution_plan:canary", Exchange: sandbox.ExchangeBinance,
		AccountID: "binance-testnet-canary", AccountEpoch: 1,
		ConfigurationID:            "config_snapshot:canary",
		Build:                      buildinfo.Info{Commit: strings.Repeat("a", 40)},
		ExecutableSHA256:           strings.Repeat("b", 64),
		CreateRequestEvidenceCount: 1, OutboxAttemptCount: 1,
		Qualified: true, CompletedAt: time.Unix(1_800_000_000, 0).UTC(),
	}
	id, err := writeSandboxCanaryEvidence(directory, &evidence)
	if err != nil || len(id) != 64 {
		t.Fatalf("evidence id=%q error=%v", id, err)
	}
	path := filepath.Join(
		directory,
		"binance-execution_plan:canary.json",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o440 {
		t.Fatalf("sealed evidence mode=%v error=%v", info.Mode(), err)
	}
	if strings.Contains(string(data), `"price"`) ||
		strings.Contains(string(data), `"quantity"`) {
		t.Fatal("private order values entered canary evidence")
	}
	if _, err = writeSandboxCanaryEvidence(directory, &evidence); err == nil {
		t.Fatal("existing canary evidence was overwritten")
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".sandbox_runtime-canary-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary evidence remains: %v error=%v", matches, err)
	}
}

func TestCurrentExecutableSHA256IdentifiesRunningCandidate(t *testing.T) {
	first, err := currentExecutableSHA256()
	if err != nil || len(first) != 64 {
		t.Fatalf("executable sha256=%q error=%v", first, err)
	}
	second, err := currentExecutableSHA256()
	if err != nil || second != first {
		t.Fatalf("second executable sha256=%q error=%v", second, err)
	}
}

type sandboxCanaryStatusCase struct {
	name   string
	status sandbox.CanaryOrderStatus
	want   bool
}

func TestSandboxCanaryRequiresOneTerminalCancelOrFill(t *testing.T) {
	tests := []sandboxCanaryStatusCase{
		{
			name: "canceled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
			want: true,
		},
		{
			name: "filled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "FILLED",
			},
			want: true,
		},
		{
			name: "ambiguous cancel",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxUnknown,
				OrderState: "UNKNOWN",
			},
		},
		{
			name: "rejected",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "REJECTED",
			},
		},
		{
			name: "duplicate",
			status: sandbox.CanaryOrderStatus{
				Attempt: 2, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canaryCancelOrFillConfirmed(test.status); got != test.want {
				t.Fatalf("confirmed=%t want=%t status=%#v", got, test.want, test.status)
			}
		})
	}
}

func TestSandboxCanaryAbortRequiresOneTerminalAttempt(t *testing.T) {
	tests := []sandboxCanaryStatusCase{
		{
			name: "rejected",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "REJECTED",
			},
			want: true,
		},
		{
			name: "canceled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
			want: true,
		},
		{
			name: "filled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "FILLED",
			},
			want: true,
		},
		{
			name: "unknown",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxUnknown,
				OrderState: "UNKNOWN",
			},
		},
		{
			name: "duplicate",
			status: sandbox.CanaryOrderStatus{
				Attempt: 2, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canaryAbortable(test.status); got != test.want {
				t.Fatalf("abortable=%t want=%t", got, test.want)
			}
		})
	}
}

func TestSandboxCanaryRecoveryRequiresOneTerminalCancelOrFill(t *testing.T) {
	tests := []sandboxCanaryStatusCase{
		{
			name: "filled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "FILLED",
			},
			want: true,
		},
		{
			name: "canceled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
			want: true,
		},
		{
			name: "rejected",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "REJECTED",
			},
		},
		{
			name: "unknown",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxUnknown,
				OrderState: "UNKNOWN",
			},
		},
		{
			name: "duplicate",
			status: sandbox.CanaryOrderStatus{
				Attempt: 2, OutboxState: sandbox.OutboxTerminal,
				OrderState: "FILLED",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recoverableCanaryStatus(test.status); got != test.want {
				t.Fatalf("recoverable=%t want=%t", got, test.want)
			}
		})
	}
}

func TestSandboxCanaryRecoveryResumesOneExistingOrder(t *testing.T) {
	assertSandboxCanaryResumableStatuses(t, []sandboxCanaryStatusCase{
		{
			name: "unknown recovery required",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxUnknown,
				OrderState: "RECOVERY_REQUIRED",
			},
			want: true,
		},
		{
			name: "acknowledged",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxAcknowledged,
				OrderState: "ACKNOWLEDGED",
			},
			want: true,
		},
		{
			name: "partially filled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxAcknowledged,
				OrderState: "PARTIALLY_FILLED",
			},
			want: true,
		},
		{
			name: "terminal canceled",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "CANCELED",
			},
			want: true,
		},
	})
}

func TestSandboxCanaryRecoveryRejectsUnsafeResumeState(t *testing.T) {
	assertSandboxCanaryResumableStatuses(t, []sandboxCanaryStatusCase{
		{
			name: "terminal rejected",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxTerminal,
				OrderState: "REJECTED",
			},
		},
		{
			name: "unsent",
			status: sandbox.CanaryOrderStatus{
				Attempt: 1, OutboxState: sandbox.OutboxPending,
				OrderState: "PENDING",
			},
		},
		{
			name: "duplicate",
			status: sandbox.CanaryOrderStatus{
				Attempt: 2, OutboxState: sandbox.OutboxUnknown,
				OrderState: "RECOVERY_REQUIRED",
			},
		},
	})
}

func assertSandboxCanaryResumableStatuses(
	t *testing.T,
	tests []sandboxCanaryStatusCase,
) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resumableCanaryStatus(test.status); got != test.want {
				t.Fatalf("resumable=%t want=%t", got, test.want)
			}
		})
	}
}

func TestSandboxCanaryRecoveryRequiresOneEvidencePrefix(t *testing.T) {
	cycle := uint64(17)
	record := func(stage sandbox.CanaryEvidenceStage) sandbox.CanaryEvidenceRecord {
		return sandbox.CanaryEvidenceRecord{
			Stage: stage, StartupCycle: cycle,
		}
	}
	assertValidSandboxCanaryRecoveryEvidence(t, cycle, [][]sandbox.CanaryEvidenceRecord{
		{record(sandbox.CanaryPlanApproved)},
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryQuerySucceeded),
		},
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryQuerySucceeded),
			record(sandbox.CanaryCancelOrFillConfirmed),
		},
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryQuerySucceeded),
			record(sandbox.CanaryCancelOrFillConfirmed),
			record(sandbox.CanaryReconciled),
		},
	})
	assertInvalidSandboxCanaryRecoveryEvidence(t, [][]sandbox.CanaryEvidenceRecord{
		nil,
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryCancelOrFillConfirmed),
		},
		{
			record(sandbox.CanaryPlanApproved),
			{
				Stage:        sandbox.CanaryQuerySucceeded,
				StartupCycle: cycle + 1,
			},
		},
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryRestartVerified),
		},
		{
			record(sandbox.CanaryPlanApproved),
			record(sandbox.CanaryPlanApproved),
		},
	})
}

func assertValidSandboxCanaryRecoveryEvidence(
	t *testing.T,
	cycle uint64,
	cases [][]sandbox.CanaryEvidenceRecord,
) {
	t.Helper()
	for _, records := range cases {
		gotCycle, _, err := recoverableCanaryEvidence(records)
		if err != nil || gotCycle != cycle {
			t.Fatalf(
				"valid recovery evidence cycle=%d want=%d error=%v",
				gotCycle,
				cycle,
				err,
			)
		}
	}
}

func assertInvalidSandboxCanaryRecoveryEvidence(
	t *testing.T,
	cases [][]sandbox.CanaryEvidenceRecord,
) {
	t.Helper()
	for _, records := range cases {
		if _, _, err := recoverableCanaryEvidence(records); err == nil {
			t.Fatalf("invalid recovery evidence accepted: %#v", records)
		}
	}
}
