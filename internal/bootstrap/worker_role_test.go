package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestWorkerRolePollsDurableQueueAndRecoversReadiness(t *testing.T) {
	worker := &workerRoleStub{results: []workerRoleResult{{err: errors.New("temporary")}, {worked: true}, {}}}
	work, err := newWorkerRoleWork(worker, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- work.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	for !work.Ready() && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if !work.Ready() {
		t.Fatal("worker role did not recover readiness")
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

type workerRoleResult struct {
	worked bool
	err    error
}

type workerRoleStub struct {
	mutex   sync.Mutex
	results []workerRoleResult
}

func (stub *workerRoleStub) RunOne(context.Context) (bool, error) {
	stub.mutex.Lock()
	defer stub.mutex.Unlock()
	if len(stub.results) == 0 {
		return false, nil
	}
	result := stub.results[0]
	stub.results = stub.results[1:]
	return result.worked, result.err
}
