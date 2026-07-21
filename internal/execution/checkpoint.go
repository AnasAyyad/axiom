package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

// Checkpoint captures every deterministic virtual execution state category.
type Checkpoint struct {
	RunManifestHash   string  `json:"run_manifest_hash"`
	CursorOrdinal     uint64  `json:"cursor_ordinal"`
	CursorLogicalTime uint64  `json:"cursor_logical_time"`
	Orders            []Order `json:"orders"`
	Plans             []Saga  `json:"plans"`
	LiquidityHash     string  `json:"liquidity_hash"`
	JournalHash       string  `json:"journal_hash"`
	ProjectionHash    string  `json:"projection_hash"`
	ModelNamespace    string  `json:"model_namespace"`
	RandomStateHash   string  `json:"random_state_hash"`
	Revision          uint64  `json:"revision"`
}

// CanonicalHash returns the exact durable checkpoint checksum.
func (checkpoint Checkpoint) CanonicalHash() (string, error) {
	if !validCheckpoint(checkpoint) {
		return "", executionError("checkpoint_invalid")
	}
	checkpoint = cloneCheckpoint(checkpoint)
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return "", executionError("checkpoint_encode_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// CheckpointStore atomically persists and restores run checkpoints.
type CheckpointStore interface {
	Save(Checkpoint) error
	Load(string) (Checkpoint, bool, error)
}

// MemoryCheckpointStore is the deterministic atomic conformance store.
type MemoryCheckpointStore struct {
	mutex sync.Mutex
	items map[string]Checkpoint
}

// NewMemoryCheckpointStore constructs an empty checkpoint store.
func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{items: make(map[string]Checkpoint)}
}

// Save atomically replaces only a strictly newer valid run checkpoint.
func (store *MemoryCheckpointStore) Save(checkpoint Checkpoint) error {
	if _, err := checkpoint.CanonicalHash(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	prior, exists := store.items[checkpoint.RunManifestHash]
	if exists && checkpoint.Revision <= prior.Revision {
		return executionError("checkpoint_revision_rejected")
	}
	store.items[checkpoint.RunManifestHash] = cloneCheckpoint(checkpoint)
	return nil
}

// Load returns a defensive exact checkpoint copy.
func (store *MemoryCheckpointStore) Load(runManifestHash string) (Checkpoint, bool, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	checkpoint, exists := store.items[runManifestHash]
	return cloneCheckpoint(checkpoint), exists, nil
}

func validCheckpoint(checkpoint Checkpoint) bool {
	return validSHA256(checkpoint.RunManifestHash) && checkpoint.CursorOrdinal > 0 &&
		checkpoint.CursorLogicalTime > 0 && checkpoint.LiquidityHash != "" && checkpoint.JournalHash != "" &&
		checkpoint.ProjectionHash != "" && checkpoint.ModelNamespace != "" && checkpoint.RandomStateHash != "" &&
		checkpoint.Revision > 0
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func cloneCheckpoint(checkpoint Checkpoint) Checkpoint {
	checkpoint.Orders = append([]Order(nil), checkpoint.Orders...)
	for index := range checkpoint.Orders {
		checkpoint.Orders[index] = cloneOrder(checkpoint.Orders[index])
	}
	checkpoint.Plans = append([]Saga(nil), checkpoint.Plans...)
	for index := range checkpoint.Plans {
		checkpoint.Plans[index] = cloneSaga(checkpoint.Plans[index])
	}
	return checkpoint
}
