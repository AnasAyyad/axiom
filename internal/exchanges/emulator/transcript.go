package emulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// TranscriptEntry is one ordered emulator interaction fact.
type TranscriptEntry struct {
	Ordinal     uint64 `json:"ordinal"`
	Transport   string `json:"transport"`
	Direction   string `json:"direction"`
	Path        string `json:"path"`
	Connection  uint64 `json:"connection"`
	Status      int    `json:"status"`
	PayloadHash string `json:"payload_hash"`
}

func transcriptHash(entries []TranscriptEntry) (string, error) {
	canonical, err := json.Marshal(entries)
	if err != nil {
		return "", scenarioError("transcript")
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func bodyHash(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
