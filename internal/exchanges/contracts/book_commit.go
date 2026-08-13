package exchangecontracts

import (
	"axiom/internal/domain"
)

// BookCommit identifies one immutable book version that became visible to an
// in-process consumer. It contains ordering evidence only and no credentials or
// exchange capability.
type BookCommit struct {
	Exchange             string            `json:"exchange"`
	Instrument           domain.Instrument `json:"instrument"`
	ConnectionGeneration uint64            `json:"connection_generation"`
	BookVersion          uint64            `json:"book_version"`
	IngestOrdinal        uint64            `json:"ingest_ordinal"`
	ReceivedOffsetNanos  uint64            `json:"received_offset_nanos"`
	PublishedOffsetNanos uint64            `json:"published_offset_nanos"`
}

// Validate rejects incomplete or regressing commit evidence.
func (commit BookCommit) Validate() error {
	instrument, instrumentErr := domain.NewSpotInstrument(commit.Instrument.Base, commit.Instrument.Quote)
	if commit.Exchange == "" || instrumentErr != nil || instrument != commit.Instrument ||
		commit.ConnectionGeneration == 0 || commit.BookVersion == 0 || commit.IngestOrdinal == 0 ||
		commit.ReceivedOffsetNanos == 0 || commit.PublishedOffsetNanos < commit.ReceivedOffsetNanos {
		return NewError(ErrorValidation, OperationStream, 0)
	}
	return nil
}
