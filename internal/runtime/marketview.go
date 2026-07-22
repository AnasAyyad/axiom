package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"axiom/internal/domain"
)

// MarketKey identifies one exchange/instrument single-writer view.
type MarketKey struct {
	Exchange   string            `json:"exchange"`
	Instrument domain.Instrument `json:"instrument"`
}

// MarketViewInput contains immutable version evidence for one published view.
type MarketViewInput struct {
	Key                   MarketKey
	BookVersion           uint64
	ConnectionGeneration  uint64
	ReceiveMonotonicNanos uint64
	ReceiveUTC            time.Time
	IngestOrdinal         uint64
	ClockOffset           time.Duration
	ClockUncertainty      time.Duration
	StateHash             string
	CollectorInstance     string
	CollectorRegion       string
}

type marketViewRecord struct {
	Key                   MarketKey     `json:"key"`
	BookVersion           uint64        `json:"book_version"`
	ConnectionGeneration  uint64        `json:"connection_generation"`
	ReceiveMonotonicNanos uint64        `json:"receive_monotonic_nanos"`
	ReceiveUTC            time.Time     `json:"receive_utc"`
	IngestOrdinal         uint64        `json:"ingest_ordinal"`
	ClockOffset           time.Duration `json:"clock_offset_nanos"`
	ClockUncertainty      time.Duration `json:"clock_uncertainty_nanos"`
	StateHash             string        `json:"state_hash"`
	CollectorInstance     string        `json:"collector_instance"`
	CollectorRegion       string        `json:"collector_region"`
}

// MarketView is an immutable versioned market-state reference.
type MarketView struct{ record marketViewRecord }

// Key returns the exchange and instrument identity.
func (view MarketView) Key() MarketKey { return view.record.Key }

// Version returns the monotonically increasing local book version.
func (view MarketView) Version() uint64 { return view.record.BookVersion }

// ConnectionGeneration returns the source connection generation.
func (view MarketView) ConnectionGeneration() uint64 { return view.record.ConnectionGeneration }

// ReceiveMonotonicNanos returns the authoritative local receive order time.
func (view MarketView) ReceiveMonotonicNanos() uint64 { return view.record.ReceiveMonotonicNanos }

// ReceiveUTC returns persisted human receive-time evidence.
func (view MarketView) ReceiveUTC() time.Time { return view.record.ReceiveUTC }

// IngestOrdinal returns the dataset-wide deterministic tie-breaker.
func (view MarketView) IngestOrdinal() uint64 { return view.record.IngestOrdinal }

// ClockOffset returns the measured server-minus-local midpoint offset.
func (view MarketView) ClockOffset() time.Duration { return view.record.ClockOffset }

// ClockUncertainty returns the bounded server-time uncertainty.
func (view MarketView) ClockUncertainty() time.Duration { return view.record.ClockUncertainty }

// StateHash returns the canonical state digest.
func (view MarketView) StateHash() string { return view.record.StateHash }

// CollectorInstance returns the immutable collector identity.
func (view MarketView) CollectorInstance() string { return view.record.CollectorInstance }

// CollectorRegion returns the operator-declared collector region.
func (view MarketView) CollectorRegion() string { return view.record.CollectorRegion }

// ClockInterval returns the corrected receive-time uncertainty interval.
func (view MarketView) ClockInterval() (time.Time, time.Time) {
	center := view.record.ReceiveUTC.Add(view.record.ClockOffset)
	return center.Add(-view.record.ClockUncertainty), center.Add(view.record.ClockUncertainty)
}

// ViewReference is the exact immutable decision-input version reference.
type ViewReference struct {
	Key                   MarketKey     `json:"key"`
	BookVersion           uint64        `json:"book_version"`
	ConnectionGeneration  uint64        `json:"connection_generation"`
	ReceiveMonotonicNanos uint64        `json:"receive_monotonic_nanos"`
	ReceiveUTC            time.Time     `json:"receive_utc"`
	IngestOrdinal         uint64        `json:"ingest_ordinal"`
	ClockOffset           time.Duration `json:"clock_offset_nanos"`
	ClockUncertainty      time.Duration `json:"clock_uncertainty_nanos"`
	StateHash             string        `json:"state_hash"`
	CollectorInstance     string        `json:"collector_instance"`
	CollectorRegion       string        `json:"collector_region"`
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
	mutex             sync.RWMutex
	keys              map[string]MarketKey
	latest            map[string]MarketView
	history           map[string][]MarketView
	activeGenerations map[string]uint64
	gaps              map[string][]ViewGap
}

// NewMarketViews constructs an empty view registry.
func NewMarketViews() *MarketViews {
	return &MarketViews{keys: make(map[string]MarketKey), latest: make(map[string]MarketView), history: make(map[string][]MarketView),
		activeGenerations: make(map[string]uint64), gaps: make(map[string][]ViewGap)}
}

// ActivateGeneration selects the only connection generation eligible to publish.
func (views *MarketViews) ActivateGeneration(key MarketKey, generation uint64) error {
	if !validMarketKey(key) || generation == 0 {
		return runtimeError("market_generation_rejected", "identity")
	}
	identity := marketKeyString(key)
	views.mutex.Lock()
	defer views.mutex.Unlock()
	if current := views.activeGenerations[identity]; current >= generation {
		return runtimeError("market_generation_rejected", identity)
	}
	views.keys[identity] = key
	views.activeGenerations[identity] = generation
	return nil
}

// Publish accepts only the next monotonic version for a valid spot key.
func (views *MarketViews) Publish(input MarketViewInput) (MarketView, error) {
	if err := validateMarketView(input); err != nil {
		return MarketView{}, err
	}
	views.mutex.Lock()
	defer views.mutex.Unlock()
	key := marketKeyString(input.Key)
	if views.activeGenerations[key] != input.ConnectionGeneration {
		return MarketView{}, runtimeError("market_view_generation_rejected", key)
	}
	current, exists := views.latest[key]
	if exists && input.BookVersion != current.record.BookVersion+1 {
		return MarketView{}, runtimeError("market_view_version_rejected", key)
	}
	if exists && (input.ReceiveMonotonicNanos < current.record.ReceiveMonotonicNanos ||
		input.IngestOrdinal <= current.record.IngestOrdinal) {
		return MarketView{}, runtimeError("market_view_order_rejected", key)
	}
	view := MarketView{record: marketViewRecord(input)}
	views.latest[key] = view
	views.history[key] = append(views.history[key], view)
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
		references = append(references, referenceForView(view))
	}
	sort.Slice(references, func(left, right int) bool {
		return lessMarketKey(references[left].Key, references[right].Key)
	})
	return ViewVector{references: references}, nil
}

func validateMarketView(input MarketViewInput) error {
	if !validMarketKey(input.Key) || input.BookVersion == 0 || input.ConnectionGeneration == 0 ||
		input.ReceiveMonotonicNanos == 0 || input.IngestOrdinal == 0 || input.ReceiveUTC.IsZero() ||
		input.ReceiveUTC.Location() != time.UTC || input.ClockUncertainty < 0 ||
		!validViewLabel(input.CollectorInstance) || !validViewLabel(input.CollectorRegion) {
		return runtimeError("invalid_market_view", "identity")
	}
	if len(input.StateHash) != sha256.Size*2 {
		return runtimeError("invalid_market_view", "evidence")
	}
	if _, err := hex.DecodeString(input.StateHash); err != nil {
		return runtimeError("invalid_market_view", "state_hash")
	}
	return nil
}

func validMarketKey(key MarketKey) bool {
	return key.Exchange != "" && validViewLabel(key.Exchange) && validSpotInstrument(key.Instrument)
}

func validViewLabel(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' &&
			character != '.' && character != ':' {
			return false
		}
	}
	return true
}

func referenceForView(view MarketView) ViewReference {
	return ViewReference{Key: view.Key(), BookVersion: view.Version(),
		ConnectionGeneration: view.ConnectionGeneration(), ReceiveMonotonicNanos: view.ReceiveMonotonicNanos(),
		ReceiveUTC: view.ReceiveUTC(), IngestOrdinal: view.IngestOrdinal(), ClockOffset: view.ClockOffset(),
		ClockUncertainty: view.ClockUncertainty(), StateHash: view.StateHash(),
		CollectorInstance: view.CollectorInstance(), CollectorRegion: view.CollectorRegion()}
}

func marketKeyString(key MarketKey) string {
	base, quote := string(key.Instrument.Base), string(key.Instrument.Quote)
	return strconv.Itoa(len(key.Exchange)) + ":" + key.Exchange + ":" +
		strconv.Itoa(len(base)) + ":" + base + ":" + strconv.Itoa(len(quote)) + ":" + quote
}

func lessMarketKey(left, right MarketKey) bool {
	if left.Exchange != right.Exchange {
		return left.Exchange < right.Exchange
	}
	if left.Instrument.Base != right.Instrument.Base {
		return left.Instrument.Base < right.Instrument.Base
	}
	return left.Instrument.Quote < right.Instrument.Quote
}
