package runtimecore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOverlappingOwnersCannotAcquireSameResource(t *testing.T) {
	repository := NewMemoryLeaseRepository()
	var successes atomic.Int32
	leases := make(chan Lease, 2)
	var group sync.WaitGroup
	for _, identity := range []string{"owner-a", "owner-b"} {
		identity := identity
		group.Add(1)
		go func() {
			defer group.Done()
			lease, err := repository.Acquire(context.Background(), "shadow-engine", identity, 1, 10)
			if err == nil {
				successes.Add(1)
				leases <- lease
			}
		}()
	}
	group.Wait()
	close(leases)
	if successes.Load() != 1 {
		t.Fatalf("successful owners = %d", successes.Load())
	}
	first := <-leases
	second, err := repository.Acquire(context.Background(), "shadow-engine", "owner-c", 11, 10)
	if err != nil || second.Token() <= first.Token() {
		t.Fatalf("new epoch = %d after %d, %v", second.Token(), first.Token(), err)
	}
	if valid, _ := repository.Validate(context.Background(), first, 11); valid {
		t.Fatal("expired stale token remained valid")
	}
}

func TestLeaseLossLinearizesBeforeProtectedMutation(t *testing.T) {
	repository := NewMemoryLeaseRepository()
	gate := NewSafetyGate()
	owner, _ := NewFencedOwner(repository, gate)
	lease, err := owner.Acquire(context.Background(), "shadow-engine", "owner-a", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.ManualActivate(true); err != nil {
		t.Fatal(err)
	}
	repository.SetAvailable(false)
	called := false
	if err := owner.Mutate(context.Background(), lease.Token(), 2, func() error { called = true; return nil }); err == nil {
		t.Fatal("mutation succeeded during storage loss")
	}
	if called {
		t.Fatal("protected mutation ran after lease uncertainty")
	}
	if state, _ := gate.State(); state != StateLocked || gate.Accepting() {
		t.Fatalf("gate state = %s", state)
	}
}

func TestRenewReleaseAndStaleTokenRejection(t *testing.T) {
	repository := NewMemoryLeaseRepository()
	gate := NewSafetyGate()
	owner, _ := NewFencedOwner(repository, gate)
	lease, _ := owner.Acquire(context.Background(), "resource", "owner", 1, 5)
	if err := owner.Renew(context.Background(), 3, 10); err != nil {
		t.Fatal(err)
	}
	if err := gate.ManualActivate(true); err != nil {
		t.Fatal(err)
	}
	if err := owner.Mutate(context.Background(), lease.Token()+1, 4, func() error { return nil }); err == nil {
		t.Fatal("wrong fencing token accepted")
	}
	if err := owner.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gate.Accepting() {
		t.Fatal("release left gate active")
	}
}

func TestExpiredOwnerCannotCommitAfterNewFenceAcquisition(t *testing.T) {
	repository := NewMemoryLeaseRepository()
	oldGate := NewSafetyGate()
	oldOwner, _ := NewFencedOwner(repository, oldGate)
	oldLease, _ := oldOwner.Acquire(context.Background(), "resource", "owner-a", 1, 5)
	_ = oldGate.ManualActivate(true)
	newLease, err := repository.Acquire(context.Background(), "resource", "owner-b", 6, 5)
	if err != nil || newLease.Token() <= oldLease.Token() {
		t.Fatalf("new lease = %#v, %v", newLease, err)
	}
	called := false
	if err := oldOwner.Mutate(context.Background(), oldLease.Token(), 6, func() error {
		called = true
		return nil
	}); err == nil {
		t.Fatal("expired owner committed after a newer fence")
	}
	if called {
		t.Fatal("stale protected mutation callback ran")
	}
	if state, _ := oldGate.State(); state != StateLocked {
		t.Fatalf("stale owner state = %s", state)
	}
}
