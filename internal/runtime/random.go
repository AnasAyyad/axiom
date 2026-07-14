package runtimecore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

// RandomKey fixes every identity dimension for one deterministic draw family.
type RandomKey struct {
	RunID       string
	ComponentID string
	DecisionID  string
	OrderLegID  string
	EventID     string
}

// Randomness derives stateless keyed draws from one immutable root seed.
type Randomness struct{ root [sha256.Size]byte }

// NewRandomness accepts exactly 256 bits of owner-supplied run seed material.
func NewRandomness(root []byte) (*Randomness, error) {
	if len(root) != sha256.Size {
		return nil, runtimeError("invalid_random_seed", "root")
	}
	var seed [sha256.Size]byte
	copy(seed[:], root)
	return &Randomness{root: seed}, nil
}

// Uint64 returns one indexed deterministic draw without shared consumption state.
func (randomness *Randomness) Uint64(key RandomKey, drawIndex uint64) (uint64, error) {
	if randomness == nil || !validRandomKey(key) {
		return 0, runtimeError("invalid_random_key", "identity")
	}
	digest := hmac.New(sha256.New, randomness.root[:])
	for _, value := range []string{key.RunID, key.ComponentID, key.DecisionID, key.OrderLegID, key.EventID} {
		writeLengthPrefixed(digest, value)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], drawIndex)
	_, _ = digest.Write(counter[:])
	sum := digest.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8]), nil
}

type digestWriter interface{ Write([]byte) (int, error) }

func writeLengthPrefixed(writer digestWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func validRandomKey(key RandomKey) bool {
	return key.RunID != "" && key.ComponentID != "" && key.DecisionID != "" &&
		key.OrderLegID != "" && key.EventID != ""
}
