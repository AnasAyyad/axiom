package runtimecore

import (
	"context"
	"sync"
	"time"
)

const maximumGracefulShutdown = 60 * time.Second

// Lifecycle owns bounded workers and measured fail-closed shutdown.
type Lifecycle struct {
	mutex          sync.Mutex
	context        context.Context
	cancel         context.CancelFunc
	gate           *SafetyGate
	metrics        *RuntimeMetrics
	shutdownLimit  time.Duration
	maximumWorkers int
	workers        int
	stopping       bool
	group          sync.WaitGroup
}

// NewLifecycle constructs an initially paused bounded worker owner.
func NewLifecycle(gate *SafetyGate, metrics *RuntimeMetrics, shutdownLimit time.Duration, maximumWorkers int) (*Lifecycle, error) {
	if gate == nil || metrics == nil || shutdownLimit <= 0 || shutdownLimit > maximumGracefulShutdown || maximumWorkers <= 0 {
		return nil, runtimeError("invalid_lifecycle", "configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Lifecycle{
		context: ctx, cancel: cancel, gate: gate, metrics: metrics,
		shutdownLimit: shutdownLimit, maximumWorkers: maximumWorkers,
	}, nil
}

// Go starts one owned worker within the declared bound.
func (lifecycle *Lifecycle) Go(worker func(context.Context)) error {
	lifecycle.mutex.Lock()
	defer lifecycle.mutex.Unlock()
	if lifecycle.stopping || worker == nil || lifecycle.workers >= lifecycle.maximumWorkers {
		return runtimeError("worker_rejected", "lifecycle")
	}
	lifecycle.workers++
	lifecycle.group.Add(1)
	go func() {
		defer lifecycle.group.Done()
		worker(lifecycle.context)
	}()
	return nil
}

// Shutdown pauses acceptance, cancels workers, and waits within the configured limit.
func (lifecycle *Lifecycle) Shutdown(ctx context.Context) error {
	started := time.Now()
	lifecycle.mutex.Lock()
	if lifecycle.stopping {
		lifecycle.mutex.Unlock()
		return runtimeError("shutdown_in_progress", "lifecycle")
	}
	lifecycle.stopping = true
	lifecycle.gate.Pause("shutdown")
	lifecycle.cancel()
	lifecycle.mutex.Unlock()
	done := make(chan struct{})
	go func() { lifecycle.group.Wait(); close(done) }()
	timer := time.NewTimer(lifecycle.shutdownLimit)
	defer timer.Stop()
	select {
	case <-done:
		lifecycle.metrics.RecordShutdown(uint64(time.Since(started).Nanoseconds()))
		return nil
	case <-ctx.Done():
		return runtimeError("shutdown_cancelled", "lifecycle")
	case <-timer.C:
		return runtimeError("shutdown_timeout", "lifecycle")
	}
}
