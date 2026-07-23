package triangular

import (
	"runtime"
	"sort"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
)

func TestB4DeclaredProfileEvaluatorClaimsAndRiskP99AtMost25Milliseconds(t *testing.T) {
	if raceInstrumentation {
		t.Skip("latency qualification is invalid under race instrumentation")
	}
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	set := NewCandidateClaimFixture(t, candidate, "portfolio-a")
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	engine, err := newB4PerformanceRiskEngine(now)
	if err != nil {
		t.Fatal(err)
	}
	riskInput := RiskInput{
		Policies:     []risk.Policy{triangularRiskPolicy(risk.StateNormal)},
		Observations: triangularHealthyRiskObservations(), EvaluatedAt: now.Add(time.Second),
	}
	durations := measureB4DeclaredProfile(t, input, candidate, set, engine, riskInput)
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p99 := durations[(len(durations)*99+99)/100-1]
	t.Logf(
		"declared profile go=%s os=%s arch=%s cpus=%d samples=%d p99=%s",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), len(durations), p99,
	)
	if p99 > 25*time.Millisecond {
		t.Fatalf("B4 evaluator + atomic claims + central risk p99 %s exceeds 25ms", p99)
	}
}

func measureB4DeclaredProfile(
	t *testing.T,
	input EvaluationInput,
	candidate Candidate,
	set *portfolio.AtomicClaimSet,
	engine *risk.Engine,
	riskInput RiskInput,
) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, 200)
	for index := 0; index < 210; index++ {
		started := time.Now()
		candidates, evaluateErr := Evaluate(input)
		if evaluateErr != nil || len(candidates) == 0 {
			t.Fatal(evaluateErr)
		}
		if _, riskErr := ApproveCandidate(engine, candidate, riskInput, 1_010); riskErr != nil {
			t.Fatal(riskErr)
		}
		reservationID, _ := domain.NewReservationID("b4-p99-" + uintString(uint64(index+1)))
		group, claimErr := ClaimCandidate(
			set, candidate, "portfolio-a", reservationID,
			runtimecore.FencingToken(1), 1_010,
		)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		elapsed := time.Since(started)
		if err := set.Close(group.ID, group.Revision, group.Fence, portfolio.ClaimReleased); err != nil {
			t.Fatal(err)
		}
		if index >= 10 {
			durations = append(durations, elapsed)
		}
	}
	return durations
}
