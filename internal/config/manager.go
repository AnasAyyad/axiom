package config

import (
	"sync"
	"sync/atomic"

	"axiom/internal/domain"
)

// Manager publishes validated snapshots atomically and retains audit metadata.
type Manager struct {
	current atomic.Pointer[Snapshot]
	mutex   sync.Mutex
	history []Metadata
}

// Publish validates and atomically replaces the current configuration revision.
func (manager *Manager) Publish(configuration Configuration, source Source, actor string, clock domain.Clock) (Snapshot, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	current := manager.current.Load()
	if current != nil && configuration.Revision <= current.configuration.Revision {
		return Snapshot{}, configError("stale_configuration", "revision")
	}
	if !validSource(source) || !validActor(actor) || clock == nil {
		return Snapshot{}, configError("invalid_configuration", "snapshot_origin")
	}
	if err := Validate(configuration); err != nil {
		return Snapshot{}, err
	}
	if current != nil {
		if err := validateReload(current.configuration, configuration); err != nil {
			return Snapshot{}, err
		}
	}
	snapshot, err := buildSnapshot(configuration, source, actor, clock)
	if err != nil {
		return Snapshot{}, err
	}
	stored := snapshot
	manager.current.Store(&stored)
	metadata := snapshot.metadata()
	metadata.Changes = configurationChanges(current, configuration)
	manager.history = append(manager.history, metadata)
	return snapshot, nil
}

// Current returns the current immutable snapshot when one has been published.
func (manager *Manager) Current() (Snapshot, bool) {
	current := manager.current.Load()
	if current == nil {
		return Snapshot{}, false
	}
	return *current, true
}

// History returns a defensive copy of accepted snapshot audit metadata.
func (manager *Manager) History() []Metadata {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	history := append([]Metadata(nil), manager.history...)
	for index := range history {
		history[index].Changes = append([]string(nil), history[index].Changes...)
	}
	return history
}
