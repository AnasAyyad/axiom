package runtimecore

import (
	"context"
	"sync"
)

// FencedOwner linearizes lease loss before protected mutation.
type FencedOwner struct {
	mutex       sync.Mutex
	repository  LeaseRepository
	gate        *SafetyGate
	lease       Lease
	held        bool
	metricState LeaseMetricState
}

// NewFencedOwner constructs an initially non-owning, paused runtime owner.
func NewFencedOwner(repository LeaseRepository, gate *SafetyGate) (*FencedOwner, error) {
	if repository == nil || gate == nil {
		return nil, runtimeError("invalid_owner", "dependencies")
	}
	return &FencedOwner{repository: repository, gate: gate, metricState: LeaseMetricAbsent}, nil
}

// Acquire conditionally obtains exclusive ownership without activating decisions.
func (owner *FencedOwner) Acquire(ctx context.Context, resource, identity string, now, ttl LogicalTime) (Lease, error) {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.held {
		return Lease{}, runtimeError("lease_held", resource)
	}
	lease, err := owner.repository.Acquire(ctx, resource, identity, now, ttl)
	if err != nil {
		owner.gate.Lock("lease_acquire_failed")
		return Lease{}, err
	}
	owner.lease, owner.held = lease, true
	owner.metricState = LeaseMetricHeld
	return lease, nil
}

// Renew extends ownership or synchronously locks acceptance on any uncertainty.
func (owner *FencedOwner) Renew(ctx context.Context, now, ttl LogicalTime) error {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if !owner.held {
		return runtimeError("lease_not_held", "renew")
	}
	lease, err := owner.repository.Renew(ctx, owner.lease, now, ttl)
	if err != nil {
		owner.loseLocked("lease_renew_failed")
		return err
	}
	owner.lease = lease
	return nil
}

// Mutate runs only after current fencing is synchronously revalidated.
func (owner *FencedOwner) Mutate(ctx context.Context, token FencingToken, now LogicalTime, mutation func() error) error {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if !owner.held || owner.lease.Token() != token || !owner.gate.Accepting() || mutation == nil {
		return runtimeError("protected_mutation_rejected", "precondition")
	}
	err := owner.repository.Mutate(ctx, owner.lease, now, func(validated FencingToken) error {
		if validated != token {
			return runtimeError("stale_fencing_token", owner.lease.Resource())
		}
		return mutation()
	})
	if err != nil {
		owner.loseLocked("lease_lost")
		return err
	}
	return nil
}

// Lose synchronously stops plan acceptance before returning.
func (owner *FencedOwner) Lose(reason string) {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	owner.loseLocked(reason)
}

// Release conditionally gives up current ownership and pauses acceptance.
func (owner *FencedOwner) Release(ctx context.Context) error {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if !owner.held {
		return nil
	}
	if err := owner.repository.Release(ctx, owner.lease); err != nil {
		owner.loseLocked("lease_release_failed")
		return err
	}
	owner.held = false
	owner.metricState = LeaseMetricAbsent
	owner.gate.Pause("lease_released")
	return nil
}

func (owner *FencedOwner) loseLocked(reason string) {
	owner.held = false
	owner.metricState = LeaseMetricLost
	owner.gate.Lock(reason)
}

// LeaseMetricState returns one bounded operational ownership label.
func (owner *FencedOwner) LeaseMetricState() LeaseMetricState {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.metricState
}
