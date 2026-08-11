package postgres

import (
	"testing"
	"time"
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
