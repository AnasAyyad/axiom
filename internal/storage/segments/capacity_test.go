package segments

import (
	"math"
	"testing"
	"time"
)

func TestCapacityPlanRoundsUpAndEnforcesHeadroom(t *testing.T) {
	policy := CapacityPolicy{
		HotRetentionDays: 30, HeadroomPercent: 30, MinimumFreeBytes: MinimumFreeBytes,
		SegmentMaxBytes: MaximumSegmentBytes, SegmentMaxAge: MaximumSegmentDuration,
	}
	samples := []CapacitySample{
		{Stream: "bybit/trades/BTC-USDT/full", ObservedBytes: 101, ObservedDuration: 10 * time.Second},
		{Stream: "binance/depth/BTC-USDT/1000", ObservedBytes: 1000, ObservedDuration: 100 * time.Second},
	}
	plan, err := PlanCapacity(samples, policy, 12*1024*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	// 101/10 seconds rounds up to 872,640 bytes/day; the second sample is 864,000.
	if plan.BytesPerDay != 1_736_640 || plan.HotRetentionBytes != 52_099_200 || plan.HeadroomBytes != 15_629_760 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.RequiredBytes != 10_805_147_200 || !plan.Sufficient {
		t.Fatalf("capacity result = %#v", plan)
	}
	if plan.Forecasts[0].Stream != "binance/depth/BTC-USDT/1000" {
		t.Fatalf("forecasts are not canonical: %#v", plan.Forecasts)
	}
}

func TestCapacityPlanReportsInsufficientProvisioning(t *testing.T) {
	plan, err := PlanCapacity(
		[]CapacitySample{{Stream: "binance/depth/BTC-USDT/1000", ObservedBytes: 1, ObservedDuration: time.Second}},
		CapacityPolicy{HotRetentionDays: 30, HeadroomPercent: 30, MinimumFreeBytes: MinimumFreeBytes,
			SegmentMaxBytes: MaximumSegmentBytes, SegmentMaxAge: MaximumSegmentDuration},
		MinimumFreeBytes,
	)
	if err != nil || plan.Sufficient {
		t.Fatalf("insufficient capacity = %#v, %v", plan, err)
	}
}

func TestCapacityPlanFailsClosedOnWeakPolicyDuplicateOrOverflow(t *testing.T) {
	valid := CapacityPolicy{HotRetentionDays: 30, HeadroomPercent: 30, MinimumFreeBytes: MinimumFreeBytes,
		SegmentMaxBytes: MaximumSegmentBytes, SegmentMaxAge: MaximumSegmentDuration}
	sample := CapacitySample{Stream: "stream-a", ObservedBytes: 1, ObservedDuration: time.Second}
	weak := valid
	weak.HeadroomPercent = 29
	if _, err := PlanCapacity([]CapacitySample{sample}, weak, 1); err == nil {
		t.Fatal("weak headroom accepted")
	}
	if _, err := PlanCapacity([]CapacitySample{sample, sample}, valid, 1); err == nil {
		t.Fatal("duplicate stream accepted")
	}
	overflow := sample
	overflow.ObservedBytes = math.MaxUint64
	if _, err := PlanCapacity([]CapacitySample{overflow}, valid, 1); err == nil {
		t.Fatal("capacity overflow accepted")
	}
}
