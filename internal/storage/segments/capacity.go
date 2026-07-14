package segments

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Capacity policy constants encode the non-weakenable initial V1 defaults.
const (
	// MinimumHotRetentionDays is the initial raw-data retention floor.
	MinimumHotRetentionDays = 30
	// MinimumHeadroomPercent is required above measured hot storage.
	MinimumHeadroomPercent = 30
	// MinimumFreeBytes preserves the initial small-server decision reserve.
	MinimumFreeBytes = 10 * 1024 * 1024 * 1024
	// MaximumSegmentBytes bounds one finalized segment.
	MaximumSegmentBytes = 256 * 1024 * 1024
	// MaximumSegmentDuration bounds one finalized segment by wall duration.
	MaximumSegmentDuration = time.Hour
	secondsPerDay          = 24 * 60 * 60
)

// CapacitySample is an observed finalized-byte rate for one exact
// exchange/event-type/instrument/depth stream.
type CapacitySample struct {
	Stream           string
	ObservedBytes    uint64
	ObservedDuration time.Duration
}

// CapacityPolicy fixes the initial retention and segment boundaries.
type CapacityPolicy struct {
	HotRetentionDays uint32
	HeadroomPercent  uint32
	MinimumFreeBytes uint64
	SegmentMaxBytes  uint64
	SegmentMaxAge    time.Duration
}

// StreamForecast preserves the measured input and its conservative daily rate.
type StreamForecast struct {
	Stream           string
	ObservedBytes    uint64
	ObservedDuration time.Duration
	BytesPerDay      uint64
}

// CapacityPlan is auditable integer-only capacity evidence. Sufficient is a
// result, never an override of the required byte calculation.
type CapacityPlan struct {
	Forecasts         []StreamForecast
	BytesPerDay       uint64
	HotRetentionBytes uint64
	HeadroomBytes     uint64
	MinimumFreeBytes  uint64
	RequiredBytes     uint64
	ProvisionedBytes  uint64
	Sufficient        bool
}

// PlanCapacity extrapolates measured finalized bytes without binary
// floating-point arithmetic and rounds every storage requirement upward.
func PlanCapacity(samples []CapacitySample, policy CapacityPolicy, provisionedBytes uint64) (CapacityPlan, error) {
	if len(samples) == 0 || policy.HotRetentionDays < MinimumHotRetentionDays ||
		policy.HeadroomPercent < MinimumHeadroomPercent || policy.MinimumFreeBytes < MinimumFreeBytes ||
		policy.SegmentMaxBytes == 0 || policy.SegmentMaxBytes > MaximumSegmentBytes ||
		policy.SegmentMaxAge <= 0 || policy.SegmentMaxAge > MaximumSegmentDuration || provisionedBytes == 0 {
		return CapacityPlan{}, fmt.Errorf("segment_capacity_policy_invalid")
	}
	forecasts := make([]StreamForecast, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))
	var daily uint64
	for _, sample := range samples {
		seconds := uint64(sample.ObservedDuration / time.Second)
		if sample.Stream == "" || sample.ObservedBytes == 0 || seconds == 0 || sample.ObservedDuration%time.Second != 0 {
			return CapacityPlan{}, fmt.Errorf("segment_capacity_sample_invalid")
		}
		if _, duplicate := seen[sample.Stream]; duplicate {
			return CapacityPlan{}, fmt.Errorf("segment_capacity_sample_duplicate")
		}
		seen[sample.Stream] = struct{}{}
		bytesPerDay, err := multiplyCeilingDivide(sample.ObservedBytes, secondsPerDay, seconds)
		if err != nil || math.MaxUint64-daily < bytesPerDay {
			return CapacityPlan{}, fmt.Errorf("segment_capacity_overflow")
		}
		daily += bytesPerDay
		forecasts = append(forecasts, StreamForecast{
			Stream: sample.Stream, ObservedBytes: sample.ObservedBytes,
			ObservedDuration: sample.ObservedDuration, BytesPerDay: bytesPerDay,
		})
	}
	sort.Slice(forecasts, func(left, right int) bool { return forecasts[left].Stream < forecasts[right].Stream })
	hot, err := checkedMultiply(daily, uint64(policy.HotRetentionDays))
	if err != nil {
		return CapacityPlan{}, err
	}
	headroom, err := multiplyCeilingDivide(hot, uint64(policy.HeadroomPercent), 100)
	if err != nil || math.MaxUint64-hot < headroom || math.MaxUint64-(hot+headroom) < policy.MinimumFreeBytes {
		return CapacityPlan{}, fmt.Errorf("segment_capacity_overflow")
	}
	required := hot + headroom + policy.MinimumFreeBytes
	return CapacityPlan{
		Forecasts: forecasts, BytesPerDay: daily, HotRetentionBytes: hot, HeadroomBytes: headroom,
		MinimumFreeBytes: policy.MinimumFreeBytes, RequiredBytes: required,
		ProvisionedBytes: provisionedBytes, Sufficient: provisionedBytes >= required,
	}, nil
}

func multiplyCeilingDivide(value, multiplier, divisor uint64) (uint64, error) {
	product, err := checkedMultiply(value, multiplier)
	if err != nil || divisor == 0 {
		return 0, fmt.Errorf("segment_capacity_overflow")
	}
	quotient := product / divisor
	if product%divisor != 0 {
		if quotient == math.MaxUint64 {
			return 0, fmt.Errorf("segment_capacity_overflow")
		}
		quotient++
	}
	return quotient, nil
}

func checkedMultiply(left, right uint64) (uint64, error) {
	if right != 0 && left > math.MaxUint64/right {
		return 0, fmt.Errorf("segment_capacity_overflow")
	}
	return left * right, nil
}
