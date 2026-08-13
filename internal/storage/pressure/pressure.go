// Package pressure implements the fail-closed operational readiness storage-watermark policy.
package pressure

import (
	"fmt"
	"math"
	"syscall"
	"time"
)

// Level is the current storage-pressure posture.
type Level string

// Storage-pressure levels ordered from least to most severe.
const (
	// LevelNormal permits new work when the observation is also fresh.
	LevelNormal Level = "NORMAL"
	// LevelHigh blocks new heavy work while safe existing work may continue.
	LevelHigh Level = "HIGH"
	// LevelCritical stops recording and new shadow entries before exhaustion.
	LevelCritical Level = "CRITICAL"
)

// Policy uses free-byte watermarks. Critical must be lower than high because
// lower available space is more severe.
type Policy struct {
	HighFreeBytes     uint64
	CriticalFreeBytes uint64
	SampleInterval    time.Duration
}

// Observation is one bounded filesystem capacity sample.
type Observation struct {
	Level          Level
	AvailableBytes uint64
	TotalBytes     uint64
	ObservedAt     time.Time
}

// Validate rejects weakened or ambiguous operational readiness watermark policies.
func (policy Policy) Validate() error {
	const minimumCriticalReserve = uint64(1024 * 1024 * 1024)
	const minimumHighReserve = uint64(10 * 1024 * 1024 * 1024)
	if policy.CriticalFreeBytes < minimumCriticalReserve ||
		policy.HighFreeBytes < minimumHighReserve ||
		policy.CriticalFreeBytes > math.MaxInt64 || policy.HighFreeBytes > math.MaxInt64 ||
		policy.CriticalFreeBytes >= policy.HighFreeBytes ||
		policy.SampleInterval < time.Second || policy.SampleInterval > time.Minute {
		return fmt.Errorf("storage_pressure_policy_invalid")
	}
	return nil
}

// Classify applies the exact free-byte thresholds without floating point.
func (policy Policy) Classify(available, total uint64, observedAt time.Time) (Observation, error) {
	if policy.Validate() != nil || total == 0 || available > total || total > math.MaxInt64 ||
		observedAt.IsZero() || observedAt.Location() != time.UTC {
		return Observation{}, fmt.Errorf("storage_pressure_observation_invalid")
	}
	level := LevelNormal
	if available <= policy.CriticalFreeBytes {
		level = LevelCritical
	} else if available <= policy.HighFreeBytes {
		level = LevelHigh
	}
	return Observation{Level: level, AvailableBytes: available,
		TotalBytes: total, ObservedAt: observedAt}, nil
}

// Probe reads the filesystem containing root. It uses available blocks rather
// than free blocks so space reserved from the process is not treated as usable.
func (policy Policy) Probe(root string, observedAt time.Time) (Observation, error) {
	if root == "" {
		return Observation{}, fmt.Errorf("storage_pressure_root_invalid")
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(root, &statistics); err != nil || statistics.Bsize <= 0 {
		return Observation{}, fmt.Errorf("storage_pressure_probe_failed")
	}
	blockSize := uint64(statistics.Bsize)
	available, availableOK := checkedMultiply(uint64(statistics.Bavail), blockSize)
	total, totalOK := checkedMultiply(uint64(statistics.Blocks), blockSize)
	if !availableOK || !totalOK {
		return Observation{}, fmt.Errorf("storage_pressure_probe_overflow")
	}
	return policy.Classify(available, total, observedAt)
}

func checkedMultiply(left, right uint64) (uint64, bool) {
	if right != 0 && left > math.MaxUint64/right {
		return 0, false
	}
	return left * right, true
}
