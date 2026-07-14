package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	"axiom/internal/domain"
)

// MarketKey identifies one exchange/instrument single-writer view.
type MarketKey struct {
	Exchange   string            `json:"exchange"`
	Instrument domain.Instrument `json:"instrument"`
}

// MarketViewInput contains immutable version evidence for one published view.
type MarketViewInput struct {
	Key       MarketKey
	Version   uint64
	AsOf      domain.EventTime
	StateHash string
}

type marketViewRecord struct {
	Key       MarketKey        `json:"key"`
	Version   uint64           `json:"version"`
	AsOf      domain.EventTime `json:"as_of"`
	StateHash string           `json:"state_hash"`
}

// MarketView is an immutable versioned market-state reference.
type MarketView struct{ record marketViewRecord }

// Key returns the exchange and instrument identity.
func (view MarketView) Key() MarketKey { return view.record.Key }

// Version returns the monotonically increasing view version.
func (view MarketView) Version() uint64 { return view.record.Version }

// StateHash returns the canonical state digest.
func (view MarketView) StateHash() string { return view.record.StateHash }

// ViewReference is the exact immutable decision-input version reference.
type ViewReference struct {
	Key       MarketKey `json:"key"`
	Version   uint64    `json:"version"`
	StateHash string    `json:"state_hash"`
}

// ViewVector is a complete canonically ordered decision-input vector.
type ViewVector struct{ references []ViewReference }

// References returns a defensive copy of the complete vector.
func (vector ViewVector) References() []ViewReference {
	return append([]ViewReference(nil), vector.references...)
}

// Hash returns the deterministic vector identity.
func (vector ViewVector) Hash() string {
	encoded, _ := json.Marshal(vector.references)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// MarketViews owns immutable versions under one single-writer key.
type MarketViews struct {
	mutex  sync.RWMutex
	latest map[string]MarketView
}

// NewMarketViews constructs an empty view registry.
func NewMarketViews() *MarketViews { return &MarketViews{latest: make(map[string]MarketView)} }

// Publish accepts only the next monotonic version for a valid spot key.
func (views *MarketViews) Publish(input MarketViewInput) (MarketView, error) {
	if err := validateMarketView(input); err != nil {
		return MarketView{}, err
	}
	views.mutex.Lock()
	defer views.mutex.Unlock()
	key := marketKeyString(input.Key)
	current, exists := views.latest[key]
	if exists && input.Version != current.record.Version+1 {
		return MarketView{}, runtimeError("market_view_version_rejected", key)
	}
	if !exists && input.Version != 1 {
		return MarketView{}, runtimeError("market_view_version_rejected", key)
	}
	view := MarketView{record: marketViewRecord(input)}
	views.latest[key] = view
	return view, nil
}

// Capture requires every requested key and returns a complete sorted vector.
func (views *MarketViews) Capture(keys []MarketKey) (ViewVector, error) {
	views.mutex.RLock()
	defer views.mutex.RUnlock()
	references := make([]ViewReference, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		identity := marketKeyString(key)
		if _, duplicate := seen[identity]; duplicate {
			return ViewVector{}, runtimeError("market_view_vector_rejected", "duplicate")
		}
		view, exists := views.latest[identity]
		if !exists {
			return ViewVector{}, runtimeError("market_view_missing", identity)
		}
		seen[identity] = struct{}{}
		references = append(references, ViewReference{Key: key, Version: view.Version(), StateHash: view.StateHash()})
	}
	sort.Slice(references, func(left, right int) bool {
		return marketKeyString(references[left].Key) < marketKeyString(references[right].Key)
	})
	return ViewVector{references: references}, nil
}

func validateMarketView(input MarketViewInput) error {
	if input.Key.Exchange == "" || !validSpotInstrument(input.Key.Instrument) || input.Version == 0 {
		return runtimeError("invalid_market_view", "identity")
	}
	if input.AsOf.Validate() != nil || len(input.StateHash) != sha256.Size*2 {
		return runtimeError("invalid_market_view", "evidence")
	}
	if _, err := hex.DecodeString(input.StateHash); err != nil {
		return runtimeError("invalid_market_view", "state_hash")
	}
	return nil
}

func marketKeyString(key MarketKey) string {
	base, quote := string(key.Instrument.Base), string(key.Instrument.Quote)
	return strconv.Itoa(len(key.Exchange)) + ":" + key.Exchange + ":" +
		strconv.Itoa(len(base)) + ":" + base + ":" + strconv.Itoa(len(quote)) + ":" + quote
}
