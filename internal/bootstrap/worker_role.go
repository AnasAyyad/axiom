package bootstrap

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type offlineWorker interface {
	RunOne(context.Context) (bool, error)
}

type workerRoleWork struct {
	worker   offlineWorker
	interval time.Duration
	ready    atomic.Bool
}

func newWorkerRoleWork(worker offlineWorker, interval time.Duration) (*workerRoleWork, error) {
	if worker == nil || interval <= 0 || interval > time.Minute {
		return nil, roleError("worker_role_configuration_invalid")
	}
	return &workerRoleWork{worker: worker, interval: interval}, nil
}

// Run continuously drains durable offline work until shutdown.
func (work *workerRoleWork) Run(ctx context.Context, logger *slog.Logger) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			worked, err := work.worker.RunOne(ctx)
			if err != nil {
				work.ready.Store(false)
				logger.Warn("offline worker iteration failed", "event_code", "offline_worker_iteration_failed",
					"cause", err)
				timer.Reset(work.interval)
				continue
			}
			work.ready.Store(true)
			if worked {
				timer.Reset(time.Nanosecond)
			} else {
				timer.Reset(work.interval)
			}
		}
	}
}

// Ready requires at least one successful authoritative queue poll.
func (work *workerRoleWork) Ready() bool { return work != nil && work.ready.Load() }

type roleFailure struct{ code string }

// Error returns the stable bootstrap role failure code.
func (failure roleFailure) Error() string { return "bootstrap:" + failure.code }

func roleError(code string) error { return roleFailure{code: code} }
