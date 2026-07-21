package recorder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyManifestChainAcceptsCumulativeHistory(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	recordPair(t, recorder, 1, `{"wire":1}`, `{"canonical":1}`)
	if _, err = recorder.Flush(); err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time { return time.Unix(1_700_000_060, 0).UTC() }
	recordPair(t, recorder, 2, `{"wire":2}`, `{"canonical":2}`)
	selected, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifyManifestChain(recorder.root, selected); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyManifestChainRejectsForkedPredecessor(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	recordPair(t, recorder, 1, `{"wire":1}`, `{"canonical":1}`)
	first, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time { return time.Unix(1_700_000_060, 0).UTC() }
	recordPair(t, recorder, 2, `{"wire":2}`, `{"canonical":2}`)
	selected, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	first.Gaps = append(first.Gaps, Gap{Exchange: "binance", Instrument: recorderInstrument(t),
		ConnectionGeneration: 1, FirstSourceSequence: 5, LastSourceSequence: 5,
		StartedAt: first.CreatedAt, EndedAt: first.CreatedAt, Reason: "test_gap"})
	first.Complete = false
	first.Hash = manifestHash(first)
	overwriteManifest(t, recorder.root, first)
	if recorderCode(VerifyManifestChain(recorder.root, selected)) != "manifest_chain_invalid" {
		t.Fatal("forked predecessor was accepted")
	}
}

func overwriteManifest(t *testing.T, root string, manifest DatasetManifest) {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "session-a7-000001.dataset.json")
	if err = os.WriteFile(path, encoded, 0o640); err != nil {
		t.Fatal(err)
	}
}
