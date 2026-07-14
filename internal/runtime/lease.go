package runtimecore

import (
	"context"
	"sync"
)

// FencingToken is a monotonically increasing ownership epoch.
type FencingToken uint64

type leaseRecord struct {
	resource  string
	owner     string
	token     FencingToken
	expiresAt LogicalTime
}

// Lease is immutable proof of time-bounded exclusive ownership.
type Lease struct{ record leaseRecord }

// Resource returns the protected resource identifier.
func (lease Lease) Resource() string { return lease.record.resource }

// Owner returns the stable owner identifier.
func (lease Lease) Owner() string { return lease.record.owner }

// Token returns the monotonic fencing epoch.
func (lease Lease) Token() FencingToken { return lease.record.token }

// ExpiresAt returns the logical expiry time.
func (lease Lease) ExpiresAt() LogicalTime { return lease.record.expiresAt }

// LeaseRepository provides conditional exclusive ownership operations.
type LeaseRepository interface {
	Acquire(context.Context, string, string, LogicalTime, LogicalTime) (Lease, error)
	Renew(context.Context, Lease, LogicalTime, LogicalTime) (Lease, error)
	Release(context.Context, Lease) error
	Validate(context.Context, Lease, LogicalTime) (bool, error)
	Mutate(context.Context, Lease, LogicalTime, func(FencingToken) error) error
}

type leaseSlot struct {
	record    leaseRecord
	lastToken FencingToken
}

// MemoryLeaseRepository is the deterministic A3 conformance repository.
type MemoryLeaseRepository struct {
	mutex     sync.Mutex
	available bool
	slots     map[string]leaseSlot
}

// NewMemoryLeaseRepository constructs an available empty conformance repository.
func NewMemoryLeaseRepository() *MemoryLeaseRepository {
	return &MemoryLeaseRepository{available: true, slots: make(map[string]leaseSlot)}
}

// SetAvailable injects storage availability for deterministic fault tests.
func (repository *MemoryLeaseRepository) SetAvailable(available bool) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	repository.available = available
}

// Acquire conditionally creates one active owner with a new fencing token.
func (repository *MemoryLeaseRepository) Acquire(_ context.Context, resource, owner string, now, ttl LogicalTime) (Lease, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if err := repository.validateOperation(resource, owner, now, ttl); err != nil {
		return Lease{}, err
	}
	slot := repository.slots[resource]
	if slot.record.token != 0 && slot.record.expiresAt > now {
		return Lease{}, runtimeError("lease_held", resource)
	}
	slot.lastToken++
	if slot.lastToken == 0 {
		return Lease{}, runtimeError("fencing_exhausted", resource)
	}
	slot.record = leaseRecord{resource: resource, owner: owner, token: slot.lastToken, expiresAt: now + ttl}
	repository.slots[resource] = slot
	return Lease{record: slot.record}, nil
}

// Renew extends only the exact current unexpired lease.
func (repository *MemoryLeaseRepository) Renew(_ context.Context, lease Lease, now, ttl LogicalTime) (Lease, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if err := repository.validateOperation(lease.Resource(), lease.Owner(), now, ttl); err != nil {
		return Lease{}, err
	}
	slot := repository.slots[lease.Resource()]
	if slot.record != lease.record || slot.record.expiresAt <= now {
		return Lease{}, runtimeError("stale_fencing_token", lease.Resource())
	}
	slot.record.expiresAt = now + ttl
	repository.slots[lease.Resource()] = slot
	return Lease{record: slot.record}, nil
}

// Release clears ownership only for the exact current lease.
func (repository *MemoryLeaseRepository) Release(_ context.Context, lease Lease) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if !repository.available {
		return runtimeError("lease_store_unavailable", "release")
	}
	slot := repository.slots[lease.Resource()]
	if slot.record != lease.record {
		return runtimeError("stale_fencing_token", lease.Resource())
	}
	slot.record = leaseRecord{}
	repository.slots[lease.Resource()] = slot
	return nil
}

// Validate checks exact ownership, token, and logical expiry.
func (repository *MemoryLeaseRepository) Validate(_ context.Context, lease Lease, now LogicalTime) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if !repository.available {
		return false, runtimeError("lease_store_unavailable", "validate")
	}
	slot := repository.slots[lease.Resource()]
	return slot.record == lease.record && slot.record.expiresAt > now, nil
}

// Mutate linearizes fencing validation and the protected commit boundary.
// PostgreSQL implementations provide the same guarantee with a transaction and
// a resource-and-token predicate rather than a process mutex.
func (repository *MemoryLeaseRepository) Mutate(_ context.Context, lease Lease, now LogicalTime, mutation func(FencingToken) error) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if !repository.available {
		return runtimeError("lease_store_unavailable", "mutate")
	}
	slot := repository.slots[lease.Resource()]
	if mutation == nil || slot.record != lease.record || slot.record.expiresAt <= now {
		return runtimeError("stale_fencing_token", lease.Resource())
	}
	return mutation(slot.record.token)
}

func (repository *MemoryLeaseRepository) validateOperation(resource, owner string, now, ttl LogicalTime) error {
	if !repository.available {
		return runtimeError("lease_store_unavailable", "operation")
	}
	if resource == "" || owner == "" || now == 0 || ttl == 0 || now+ttl < now {
		return runtimeError("invalid_lease", "operation")
	}
	return nil
}

var _ LeaseRepository = (*MemoryLeaseRepository)(nil)
