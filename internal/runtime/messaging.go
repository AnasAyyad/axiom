package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"axiom/internal/domain"
)

// DurableCommand is an immutable idempotent administrative or engine command.
type DurableCommand struct {
	ID                domain.CommandID
	DeduplicationKey  string
	PayloadHash       string
	ConfigurationHash string
	CreatedAt         domain.EventTime
}

// InboxMessage identifies one durable consumer delivery.
type InboxMessage struct {
	ID          domain.InboxMessageID
	Consumer    string
	PayloadHash string
}

// OutboxDraft is authoritative event metadata created with a durable transition.
type OutboxDraft struct {
	ID          domain.OutboxMessageID
	Topic       string
	PayloadHash string
}

// OutboxRecord is a monotonically revised durable event contract.
type OutboxRecord struct {
	Draft    OutboxDraft
	Revision uint64
}

// CommandResult records whether an idempotent command was newly applied.
type CommandResult struct {
	Applied bool
	Outbox  []OutboxRecord
}

// CoordinationRepository defines atomic command, inbox, outbox, and cursor behavior.
type CoordinationRepository interface {
	ApplyCommand(DurableCommand, func() ([]OutboxDraft, error)) (CommandResult, error)
	ConsumeInbox(InboxMessage, func() ([]OutboxDraft, error)) (CommandResult, error)
	ReadOutbox(uint64, int) ([]OutboxRecord, error)
}

type storedResult struct {
	payloadHash string
	outbox      []OutboxRecord
}

// CoordinationSnapshot is restartable conformance state for A3 fault tests.
type CoordinationSnapshot struct {
	commands map[string]storedResult
	inbox    map[string]storedResult
	outbox   []OutboxRecord
	revision uint64
}

// MemoryCoordinationRepository is the deterministic A3 durable-contract model.
type MemoryCoordinationRepository struct {
	mutex     sync.Mutex
	commands  map[string]storedResult
	inbox     map[string]storedResult
	outbox    []OutboxRecord
	revision  uint64
	available bool
	outboxIDs map[string]struct{}
}

// NewMemoryCoordinationRepository constructs an available empty contract model.
func NewMemoryCoordinationRepository() *MemoryCoordinationRepository {
	return &MemoryCoordinationRepository{
		commands: make(map[string]storedResult), inbox: make(map[string]storedResult),
		available: true, outboxIDs: make(map[string]struct{}),
	}
}

// RestoreMemoryCoordinationRepository simulates restart from an immutable snapshot.
func RestoreMemoryCoordinationRepository(snapshot CoordinationSnapshot) *MemoryCoordinationRepository {
	return &MemoryCoordinationRepository{
		commands: cloneStoredResults(snapshot.commands), inbox: cloneStoredResults(snapshot.inbox),
		outbox: append([]OutboxRecord(nil), snapshot.outbox...), revision: snapshot.revision,
		available: true, outboxIDs: outboxIdentities(snapshot.outbox),
	}
}

// SetAvailable injects storage loss for fail-closed tests.
func (repository *MemoryCoordinationRepository) SetAvailable(available bool) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	repository.available = available
}

// ApplyCommand atomically deduplicates a command and creates revised outbox facts.
func (repository *MemoryCoordinationRepository) ApplyCommand(command DurableCommand, effect func() ([]OutboxDraft, error)) (CommandResult, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if err := validateCommand(command); err != nil {
		return CommandResult{}, err
	}
	key := command.ID.String() + ":" + command.DeduplicationKey
	return repository.apply(key, command.PayloadHash, repository.commands, effect)
}

// ConsumeInbox atomically deduplicates a consumer message and creates outbox facts.
func (repository *MemoryCoordinationRepository) ConsumeInbox(message InboxMessage, effect func() ([]OutboxDraft, error)) (CommandResult, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if message.ID.Value() == "" || message.Consumer == "" || !validDigest(message.PayloadHash) {
		return CommandResult{}, runtimeError("invalid_inbox", "message")
	}
	key := message.Consumer + ":" + message.ID.String()
	return repository.apply(key, message.PayloadHash, repository.inbox, effect)
}

// ReadOutbox returns the monotonic cursor page after a revision.
func (repository *MemoryCoordinationRepository) ReadOutbox(after uint64, limit int) ([]OutboxRecord, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if !repository.available || limit <= 0 || limit > 1000 {
		return nil, runtimeError("outbox_read_rejected", "cursor")
	}
	result := make([]OutboxRecord, 0, limit)
	for _, record := range repository.outbox {
		if record.Revision > after {
			result = append(result, record)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

// Snapshot returns a defensive restart model; notifications are deliberately absent.
func (repository *MemoryCoordinationRepository) Snapshot() CoordinationSnapshot {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	return CoordinationSnapshot{
		commands: cloneStoredResults(repository.commands), inbox: cloneStoredResults(repository.inbox),
		outbox: append([]OutboxRecord(nil), repository.outbox...), revision: repository.revision,
	}
}

func (repository *MemoryCoordinationRepository) apply(key, payloadHash string, records map[string]storedResult, effect func() ([]OutboxDraft, error)) (CommandResult, error) {
	if !repository.available || effect == nil {
		return CommandResult{}, runtimeError("coordination_unavailable", "apply")
	}
	if prior, exists := records[key]; exists {
		if prior.payloadHash != payloadHash {
			return CommandResult{}, runtimeError("idempotency_conflict", "payload")
		}
		return CommandResult{Outbox: append([]OutboxRecord(nil), prior.outbox...)}, nil
	}
	drafts, err := effect()
	if err != nil {
		return CommandResult{}, err
	}
	outbox, err := repository.appendOutbox(drafts)
	if err != nil {
		return CommandResult{}, err
	}
	records[key] = storedResult{payloadHash: payloadHash, outbox: outbox}
	return CommandResult{Applied: true, Outbox: append([]OutboxRecord(nil), outbox...)}, nil
}

func (repository *MemoryCoordinationRepository) appendOutbox(drafts []OutboxDraft) ([]OutboxRecord, error) {
	if uint64(len(drafts)) > ^uint64(0)-repository.revision {
		return nil, runtimeError("outbox_revision_exhausted", "append")
	}
	seen := make(map[string]struct{}, len(drafts))
	for _, draft := range drafts {
		if draft.ID.Value() == "" || draft.Topic == "" || !validDigest(draft.PayloadHash) {
			return nil, runtimeError("invalid_outbox", "draft")
		}
		if _, duplicate := seen[draft.ID.String()]; duplicate {
			return nil, runtimeError("invalid_outbox", "duplicate")
		}
		if _, duplicate := repository.outboxIDs[draft.ID.String()]; duplicate {
			return nil, runtimeError("invalid_outbox", "duplicate")
		}
		seen[draft.ID.String()] = struct{}{}
	}
	records := make([]OutboxRecord, 0, len(drafts))
	for _, draft := range drafts {
		repository.revision++
		records = append(records, OutboxRecord{Draft: draft, Revision: repository.revision})
		repository.outboxIDs[draft.ID.String()] = struct{}{}
	}
	repository.outbox = append(repository.outbox, records...)
	return records, nil
}

func validateCommand(command DurableCommand) error {
	if command.ID.Value() == "" || command.DeduplicationKey == "" || command.CreatedAt.Validate() != nil {
		return runtimeError("invalid_command", "identity")
	}
	if !validDigest(command.PayloadHash) || !validDigest(command.ConfigurationHash) {
		return runtimeError("invalid_command", "hash")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cloneStoredResults(source map[string]storedResult) map[string]storedResult {
	cloned := make(map[string]storedResult, len(source))
	for key, value := range source {
		value.outbox = append([]OutboxRecord(nil), value.outbox...)
		cloned[key] = value
	}
	return cloned
}

func outboxIdentities(records []OutboxRecord) map[string]struct{} {
	identities := make(map[string]struct{}, len(records))
	for _, record := range records {
		identities[record.Draft.ID.String()] = struct{}{}
	}
	return identities
}

var _ CoordinationRepository = (*MemoryCoordinationRepository)(nil)
