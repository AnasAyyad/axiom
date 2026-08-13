package exchangecontracts

import (
	"testing"

	"axiom/internal/domain"
)

func TestBookCommitValidation(t *testing.T) {
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	valid := BookCommit{Exchange: "binance", Instrument: instrument, ConnectionGeneration: 1,
		BookVersion: 2, IngestOrdinal: 3, ReceivedOffsetNanos: 4, PublishedOffsetNanos: 5}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*BookCommit){
		"exchange":           func(value *BookCommit) { value.Exchange = "" },
		"instrument":         func(value *BookCommit) { value.Instrument = domain.Instrument{} },
		"generation":         func(value *BookCommit) { value.ConnectionGeneration = 0 },
		"version":            func(value *BookCommit) { value.BookVersion = 0 },
		"ordinal":            func(value *BookCommit) { value.IngestOrdinal = 0 },
		"receive":            func(value *BookCommit) { value.ReceivedOffsetNanos = 0 },
		"publish_regression": func(value *BookCommit) { value.PublishedOffsetNanos = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if candidate.Validate() == nil {
				t.Fatalf("invalid commit accepted: %#v", candidate)
			}
		})
	}
}
