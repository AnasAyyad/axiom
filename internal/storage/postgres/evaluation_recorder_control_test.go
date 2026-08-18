package postgres

import (
	"testing"
	"time"

	"axiom/internal/evaluation"
)

func TestEvaluationRecorderObservationValidationKeepsCampaignUniverseFailClosed(t *testing.T) {
	now := time.Date(2030, 8, 11, 12, 0, 0, 0, time.UTC)
	baseline := []EvaluationRecorderInstrumentObservation{
		{ExchangeID: "binance", Instrument: "BTCUSDT", LatestEventAt: now},
		{ExchangeID: "binance", Instrument: "ETHUSDT", LatestEventAt: now},
	}
	if err := validateEvaluationRecorderObservationIdentity("baseline-recorder", now); err != nil {
		t.Fatalf("valid recorder identity rejected: %v", err)
	}
	if err := validateEvaluationRecorderObservation("baseline-recorder", now, baseline); err == nil {
		t.Fatal("active-campaign validation accepted an incomplete instrument universe")
	}
	if err := validateEvaluationRecorderObservationIdentity("", now); err == nil {
		t.Fatal("empty recorder identity accepted")
	}
}

func TestEvaluationShadowReserveLeavesFinalSafeBoundaryAllowance(t *testing.T) {
	reserve := int64(20 * 1024 * 1024 * 1024)
	lastSafeRecorded := evaluationRecordingLimitBytes - reserve - evaluationRecorderFinalizeAllowance
	if !evaluationShadowReserveFits(lastSafeRecorded, reserve) {
		t.Fatal("exact reserve and finalization boundary was rejected")
	}
	if evaluationShadowReserveFits(lastSafeRecorded+1, reserve) {
		t.Fatal("recording that could consume the shadow reserve was admitted")
	}
	if evaluationShadowReserveFits(0, evaluationRecordingLimitBytes) {
		t.Fatal("reserve that leaves no finalization allowance was admitted")
	}
	if evaluationShadowReserveFits(-1, reserve) || evaluationShadowReserveFits(0, 0) {
		t.Fatal("invalid byte accounting was admitted")
	}
}

func TestEvaluationRecordingRateUsesValidTimeForConservativeShadowReserve(t *testing.T) {
	const recorded = int64(12 * 1024 * 1024 * 1024)
	validSeconds := int64(6 * 60 * 60)
	rate, available := evaluationMeasuredBytesPerHour(recorded, validSeconds)
	if !available {
		t.Fatal("valid-time rate was unavailable")
	}
	if rate != 2*1024*1024*1024 {
		t.Fatalf("valid-time rate = %d", rate)
	}
	// Twelve recorded GiB spread over twelve wall hours but only six valid
	// hours must still reserve at the two-GiB-per-valid-hour rate.
	wallRate := recorded * 3600 / int64(12*60*60)
	if rate <= wallRate {
		t.Fatalf("valid-time rate %d did not exceed wall-time rate %d", rate, wallRate)
	}
	if _, available = evaluationMeasuredBytesPerHour(recorded, 3599); available {
		t.Fatal("rate became available before one valid hour")
	}
}

func TestEvaluationRecorderGapPausesOneIntervalThenResumesValidTime(t *testing.T) {
	now := time.Date(2030, 8, 11, 12, 0, 0, 0, time.UTC)
	instruments := evaluationRecorderTestObservations(now.Add(5*time.Minute), 200, 1)
	gapInterval := calculateEvaluationRecorderInterval(evaluationPriorRecorderObservation{
		ordinal: 1, session: "campaign-session", at: now, messages: 100,
	}, true, "campaign-session", now.Add(5*time.Minute), true, true, 200, 0, 1, 0, instruments)
	if gapInterval.valid || gapInterval.validSeconds != 0 {
		t.Fatalf("gap interval qualified: %#v", gapInterval)
	}

	instruments = evaluationRecorderTestObservations(now.Add(10*time.Minute), 300, 1)
	recovered := calculateEvaluationRecorderInterval(evaluationPriorRecorderObservation{
		ordinal: 2, session: "campaign-session", at: now.Add(5 * time.Minute), messages: 200, gaps: 1,
	}, true, "campaign-session", now.Add(10*time.Minute), true, true, 300, 0, 1, 0, instruments)
	if !recovered.valid || recovered.validSeconds != 300 {
		t.Fatalf("stable post-resync interval did not qualify: %#v", recovered)
	}
}

func TestEvaluationRecorderQualificationPolicyRetriesWithoutTerminatingCampaign(t *testing.T) {
	now := time.Date(2030, 8, 11, 12, 0, 0, 0, time.UTC)
	base := EvaluationRecorderQualification{State: "ACTIVE", ObservationCount: 2,
		LastObservedAt: now, LatestAllEligible: true, LatestPersistence: true,
		LossObserved: true, LastLossObservedAt: now, UnresolvedObservations: 1}
	progress, terminal, block := evaluationRecorderQualificationPolicy(now, base, []byte(`{"recovering":true}`))
	if !terminal || block || progress.State != evaluation.ProgressPause ||
		progress.Reason != evaluation.ReasonDataUnavailable {
		t.Fatalf("recoverable gap policy progress=%#v terminal=%t block=%t", progress, terminal, block)
	}

	base.UnresolvedObservations = 0
	base.LatestIntervalValid = true
	base.ValidSeconds = int64(time.Hour / time.Second)
	progress, terminal, block = evaluationRecorderQualificationPolicy(now, base, nil)
	if !terminal || block || progress.State != evaluation.ProgressWaiting {
		t.Fatalf("recovered gap did not resume accumulation: %#v terminal=%t block=%t", progress, terminal, block)
	}

	base.UnresolvedObservations = evaluationRecorderMaxUnresolvedObservations
	base.LatestIntervalValid = false
	progress, terminal, block = evaluationRecorderQualificationPolicy(now, base, nil)
	if !terminal || block || progress.State != evaluation.ProgressPause ||
		progress.Reason != evaluation.ReasonDataUnavailable {
		t.Fatalf("bounded unresolved recovery progress=%#v terminal=%t block=%t", progress, terminal, block)
	}
}

func evaluationRecorderTestObservations(at time.Time, messages, gaps uint64) []EvaluationRecorderInstrumentObservation {
	values := make([]EvaluationRecorderInstrumentObservation, 0, 6)
	for _, exchange := range []string{"binance", "bybit"} {
		for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
			values = append(values, EvaluationRecorderInstrumentObservation{ExchangeID: exchange,
				Instrument: instrument, Eligible: true, BookFresh: true, ClockEligible: true,
				LatestEventAt: at, Messages: messages / 6, Gaps: gaps})
		}
	}
	return values
}
