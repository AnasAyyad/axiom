package marketdata

import (
	"sort"
	"sync"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Book owns one ordered exchange/instrument writer and immutable readers.
type Book struct {
	mutex       sync.RWMutex
	exchange    string
	instrument  domain.Instrument
	depth       int
	bufferLimit int
	sink        DefectSink
	health      HealthState
	connection  string
	generation  uint64
	sequence    uint64
	version     uint64
	observation Observation
	lastHash    string
	bids        []exchangecontracts.PriceLevel
	asks        []exchangecontracts.PriceLevel
	buffer      []DepthEvent
	hasSnapshot bool
	lastDefect  *Defect
}

// NewBook constructs an empty bounded local book.
func NewBook(exchange string, instrument domain.Instrument, depth, bufferLimit int, sink DefectSink) (*Book, error) {
	validated, err := domain.NewSpotInstrument(instrument.Base, instrument.Quote)
	if err != nil || validated != instrument || exchange == "" || depth <= 0 || bufferLimit < depth {
		return nil, marketError("book_configuration_invalid")
	}
	return &Book{exchange: exchange, instrument: instrument, depth: depth, bufferLimit: bufferLimit,
		sink: sink, health: HealthDisconnected}, nil
}

// BeginGeneration discards prior mutable state and starts snapshot synchronization.
func (book *Book) BeginGeneration(connectionID string, generation uint64) error {
	book.mutex.Lock()
	defer book.mutex.Unlock()
	if connectionID == "" || generation == 0 || generation <= book.generation {
		return marketError("generation_rejected")
	}
	book.connection, book.generation, book.health = connectionID, generation, HealthSyncing
	book.sequence, book.version = 0, 0
	book.lastHash = ""
	book.bids, book.asks, book.buffer = nil, nil, nil
	book.hasSnapshot = false
	return nil
}

// Buffer queues one pre-snapshot delta without skipping an individual event.
func (book *Book) Buffer(event DepthEvent) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if err := book.validateEvent(event); err != nil || book.health != HealthSyncing {
		return marketError("delta_rejected")
	}
	if len(book.buffer) >= book.bufferLimit {
		book.invalidateLocked("buffer_overflow", event.Update.LastSequence)
		return marketError("buffer_overflow")
	}
	book.buffer = append(book.buffer, cloneDepthEvent(event))
	return nil
}

// InstallSnapshot replaces depth and bridges all safely buffered events.
func (book *Book) InstallSnapshot(snapshot exchangecontracts.BookSnapshot, observation Observation) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if book.health != HealthSyncing || book.hasSnapshot || observation.Validate() != nil ||
		observation.ConnectionID != book.connection || observation.ConnectionGeneration != book.generation ||
		observation.SourceSequence != snapshot.LastSequence || snapshot.Exchange != exchangecontracts.ExchangeID(book.exchange) ||
		snapshot.Instrument != book.instrument || snapshot.LastSequence == 0 {
		return marketError("snapshot_rejected")
	}
	bids, err := normalizeSide(snapshot.Bids, true, book.bufferLimit, false)
	if err != nil {
		return book.invalidateAndReturn("snapshot_invalid", snapshot.LastSequence)
	}
	asks, err := normalizeSide(snapshot.Asks, false, book.bufferLimit, false)
	if err != nil || len(bids) == 0 || len(asks) == 0 || crossed(bids, asks) || snapshot.RawPayloadHash == "" {
		return book.invalidateAndReturn("snapshot_invalid", snapshot.LastSequence)
	}
	book.bids, book.asks = bids, asks
	book.sequence, book.version, book.observation, book.hasSnapshot = snapshot.LastSequence, 1, observation, true
	book.lastHash = snapshot.RawPayloadHash
	buffered := book.buffer
	book.buffer = nil
	for _, event := range buffered {
		if event.Update.LastSequence <= book.sequence {
			continue
		}
		if err = book.applyLocked(event); err != nil {
			return err
		}
	}
	return nil
}

// Apply validates and commits the next live delta or invalidates the generation.
func (book *Book) Apply(event DepthEvent) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if !book.hasSnapshot {
		return marketError("snapshot_missing")
	}
	return book.applyLocked(event)
}

// VerifyChecksum invalidates the active generation on an exchange checksum mismatch.
func (book *Book) VerifyChecksum(matches bool) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if matches {
		return nil
	}
	return book.invalidateAndReturn("checksum_mismatch", book.sequence)
}

// MarkStale invalidates a generation whose monotonic publication age exceeds policy.
func (book *Book) MarkStale(currentOffset uint64, maximumAge uint64) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if !book.hasSnapshot || currentOffset < book.observation.PublishedOffsetNanos || maximumAge == 0 {
		return book.invalidateAndReturn("stale_time_invalid", book.sequence)
	}
	if currentOffset-book.observation.PublishedOffsetNanos <= maximumAge {
		return nil
	}
	return book.invalidateAndReturn("book_stale", book.sequence)
}

// Invalidate makes the active generation ineligible for a bounded source defect.
func (book *Book) Invalidate(code string, sequence uint64) error {
	book.mutex.Lock()
	defer book.finishMutation()
	if code == "" {
		code = "source_invalid"
	}
	return book.invalidateAndReturn(code, sequence)
}

// View returns a complete defensive snapshot even when it is ineligible.
func (book *Book) View() BookView {
	book.mutex.RLock()
	defer book.mutex.RUnlock()
	return BookView{record: bookViewRecord{Exchange: book.exchange, Instrument: book.instrument,
		Health: book.health, Generation: book.generation, Sequence: book.sequence, Version: book.version,
		Observation: book.observation, Bids: cloneDepth(book.bids, book.depth), Asks: cloneDepth(book.asks, book.depth)}}
}

func (book *Book) applyLocked(event DepthEvent) error {
	if err := book.validateEvent(event); err != nil {
		return book.invalidateAndReturn("delta_invalid", event.Update.LastSequence)
	}
	if event.Update.LastSequence <= book.sequence {
		if event.Update.LastSequence == book.sequence && event.Update.RawPayloadHash != book.lastHash {
			return book.invalidateAndReturn("duplicate_conflict", event.Update.LastSequence)
		}
		return nil
	}
	next := book.sequence + 1
	if event.Update.FirstSequence > next || event.Update.LastSequence < next {
		return book.invalidateAndReturn("sequence_gap", event.Update.LastSequence)
	}
	bids, err := applySide(book.bids, event.Update.Bids, true, book.bufferLimit)
	if err != nil {
		return book.invalidateAndReturn("bid_invalid", event.Update.LastSequence)
	}
	asks, err := applySide(book.asks, event.Update.Asks, false, book.bufferLimit)
	if err != nil || len(bids) == 0 || len(asks) == 0 || crossed(bids, asks) {
		return book.invalidateAndReturn("book_crossed", event.Update.LastSequence)
	}
	observation := event.Observation
	if observation.IngestOrdinal < book.observation.IngestOrdinal {
		observation.IngestOrdinal = book.observation.IngestOrdinal
	}
	book.bids, book.asks = bids, asks
	book.sequence, book.version, book.observation = event.Update.LastSequence, book.version+1, observation
	book.lastHash = event.Update.RawPayloadHash
	book.health = HealthHealthy
	return nil
}

func cloneDepth(levels []exchangecontracts.PriceLevel, depth int) []exchangecontracts.PriceLevel {
	if len(levels) > depth {
		levels = levels[:depth]
	}
	return cloneLevels(levels)
}

func (book *Book) validateEvent(event DepthEvent) error {
	if event.Observation.Validate() != nil || event.Observation.ConnectionID != book.connection ||
		event.Observation.ConnectionGeneration != book.generation ||
		event.Observation.SourceSequence != event.Update.LastSequence ||
		event.Update.Exchange != exchangecontracts.ExchangeID(book.exchange) || event.Update.Instrument != book.instrument ||
		event.Update.FirstSequence == 0 || event.Update.LastSequence < event.Update.FirstSequence ||
		event.Update.RawPayloadHash == "" {
		return marketError("event_identity_invalid")
	}
	return nil
}

func (book *Book) invalidateAndReturn(code string, sequence uint64) error {
	book.invalidateLocked(code, sequence)
	return marketError(code)
}

func (book *Book) invalidateLocked(code string, sequence uint64) {
	book.health, book.hasSnapshot, book.buffer = HealthPaused, false, nil
	defect := Defect{Code: code, Exchange: book.exchange, Instrument: book.instrument,
		Generation: book.generation, Sequence: sequence}
	book.lastDefect = &defect
}

func (book *Book) finishMutation() {
	defect, sink := book.lastDefect, book.sink
	book.lastDefect = nil
	book.mutex.Unlock()
	if defect != nil && sink != nil {
		sink(*defect)
	}
}

func normalizeSide(levels []exchangecontracts.PriceLevel, bids bool, depth int, allowZero bool) ([]exchangecontracts.PriceLevel, error) {
	result := cloneLevels(levels)
	for _, level := range result {
		if level.Price.String() == "0" || (!allowZero && level.Quantity.String() == "0") {
			return nil, marketError("level_invalid")
		}
	}
	sort.Slice(result, func(left, right int) bool {
		comparison := result[left].Price.Compare(result[right].Price)
		if bids {
			return comparison > 0
		}
		return comparison < 0
	})
	for index := 1; index < len(result); index++ {
		if result[index-1].Price.Compare(result[index].Price) == 0 {
			return nil, marketError("level_duplicate")
		}
	}
	if len(result) > depth {
		result = result[:depth]
	}
	return result, nil
}

func applySide(current, updates []exchangecontracts.PriceLevel, bids bool, depth int) ([]exchangecontracts.PriceLevel, error) {
	result := cloneLevels(current)
	seen := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		key := update.Price.String()
		if key == "0" {
			return nil, marketError("level_invalid")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, marketError("level_duplicate")
		}
		seen[key] = struct{}{}
		index := sort.Search(len(result), func(index int) bool {
			comparison := result[index].Price.Compare(update.Price)
			if bids {
				return comparison <= 0
			}
			return comparison >= 0
		})
		if index < len(result) && result[index].Price.Compare(update.Price) == 0 {
			if update.Quantity.String() == "0" {
				result = append(result[:index], result[index+1:]...)
			} else {
				result[index] = update
			}
		} else if update.Quantity.String() != "0" {
			result = append(result, exchangecontracts.PriceLevel{})
			copy(result[index+1:], result[index:])
			result[index] = update
		}
	}
	if len(result) > depth {
		result = result[:depth]
	}
	return result, nil
}

func crossed(bids, asks []exchangecontracts.PriceLevel) bool {
	return len(bids) == 0 || len(asks) == 0 || bids[0].Price.Compare(asks[0].Price) >= 0
}

func cloneLevels(levels []exchangecontracts.PriceLevel) []exchangecontracts.PriceLevel {
	return append([]exchangecontracts.PriceLevel(nil), levels...)
}

func cloneDepthEvent(event DepthEvent) DepthEvent {
	event.Update.Bids = cloneLevels(event.Update.Bids)
	event.Update.Asks = cloneLevels(event.Update.Asks)
	return event
}
