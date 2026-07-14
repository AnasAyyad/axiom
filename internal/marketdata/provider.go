package marketdata

import (
	"strconv"
	"sync"

	"axiom/internal/domain"
)

// Provider resolves immutable market views without exposing writers.
type Provider struct {
	mutex   sync.RWMutex
	books   map[string]*Book
	candles map[string]*CandleStore
}

var _ MarketViewProvider = (*Provider)(nil)

// NewProvider constructs an empty view registry.
func NewProvider() *Provider {
	return &Provider{books: make(map[string]*Book), candles: make(map[string]*CandleStore)}
}

// RegisterBook adds one unique writer-owned book.
func (provider *Provider) RegisterBook(book *Book) error {
	if book == nil {
		return marketError("book_missing")
	}
	view := book.View()
	key := marketKey(view.Exchange(), view.Instrument(), "")
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if _, exists := provider.books[key]; exists {
		return marketError("book_duplicate")
	}
	provider.books[key] = book
	return nil
}

// RegisterCandles adds one unique completed-candle store.
func (provider *Provider) RegisterCandles(store *CandleStore) error {
	if store == nil {
		return marketError("candle_store_missing")
	}
	key := marketKey(store.exchange, store.instrument, store.interval)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if _, exists := provider.candles[key]; exists {
		return marketError("candle_store_duplicate")
	}
	provider.candles[key] = store
	return nil
}

// Book returns an immutable book view.
func (provider *Provider) Book(exchange string, instrument domain.Instrument) (BookView, error) {
	provider.mutex.RLock()
	book := provider.books[marketKey(exchange, instrument, "")]
	provider.mutex.RUnlock()
	if book == nil {
		return BookView{}, marketError("book_missing")
	}
	return book.View(), nil
}

// CompletedCandles returns an immutable completed-candle view.
func (provider *Provider) CompletedCandles(
	exchange string,
	instrument domain.Instrument,
	interval string,
) (CandleView, error) {
	provider.mutex.RLock()
	store := provider.candles[marketKey(exchange, instrument, interval)]
	provider.mutex.RUnlock()
	if store == nil {
		return CandleView{}, marketError("candle_store_missing")
	}
	return store.View(), nil
}

func marketKey(exchange string, instrument domain.Instrument, interval string) string {
	base, quote := string(instrument.Base), string(instrument.Quote)
	return strconv.Itoa(len(exchange)) + ":" + exchange + ":" + strconv.Itoa(len(base)) + ":" + base + ":" +
		strconv.Itoa(len(quote)) + ":" + quote + ":" + strconv.Itoa(len(interval)) + ":" + interval
}
