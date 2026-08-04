package config

import (
	"fmt"
	"path/filepath"
	"time"
)

func loadRecorderRuntime() (RecorderRuntime, error) {
	flush, err := durationValue("RECORDER_FLUSH_INTERVAL", "5m")
	if err != nil || flush < time.Second || flush > time.Hour {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_FLUSH_INTERVAL")
	}
	pressureInterval, err := durationValue("RECORDER_PRESSURE_SAMPLE_INTERVAL", "15s")
	if err != nil || pressureInterval < time.Second || pressureInterval > time.Minute {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_PRESSURE_SAMPLE_INTERVAL")
	}
	highFreeBytes, err := uint64Value("RECORDER_MIN_FREE_BYTES", "10737418240")
	if err != nil || highFreeBytes < 10*1024*1024*1024 {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_MIN_FREE_BYTES")
	}
	criticalFreeBytes, err := uint64Value("RECORDER_CRITICAL_FREE_BYTES", "5368709120")
	if err != nil || criticalFreeBytes < 1024*1024*1024 || criticalFreeBytes >= highFreeBytes {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_CRITICAL_FREE_BYTES")
	}
	queue, err := integerValue("MARKET_EVENT_QUEUE_CAPACITY", "16384", 1000, 1<<20)
	if err != nil {
		return RecorderRuntime{}, err
	}
	depth, err := integerValue("ORDER_BOOK_RETAINED_DEPTH", "1000", 1, 5000)
	if err != nil || queue < depth {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:ORDER_BOOK_RETAINED_DEPTH")
	}
	root := value("RECORDER_ROOT", "/var/lib/axiom/market-data")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_ROOT")
	}
	region := value("COLLECTOR_REGION", "local")
	if !validRuntimeLabel(region) {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:COLLECTOR_REGION")
	}
	return RecorderRuntime{Root: filepath.Clean(root), CollectorRegion: region, FlushInterval: flush,
		PressureInterval: pressureInterval, HighFreeBytes: highFreeBytes,
		CriticalFreeBytes: criticalFreeBytes, QueueCapacity: queue, BookDepth: depth}, nil
}
