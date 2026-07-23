package binance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type forensicReadCloser struct {
	reader io.Reader
}

func (reader forensicReadCloser) Read(payload []byte) (int, error) {
	return reader.reader.Read(payload)
}

func (forensicReadCloser) Close() error { return nil }

type forensicFailingReader struct {
	payload []byte
	err     error
	done    bool
}

func (reader *forensicFailingReader) Read(destination []byte) (int, error) {
	if reader.done {
		return 0, reader.err
	}
	reader.done = true
	return copy(destination, reader.payload), reader.err
}

type forensicTimeout struct{}

func (forensicTimeout) Error() string   { return "redacted timeout" }
func (forensicTimeout) Timeout() bool   { return true }
func (forensicTimeout) Temporary() bool { return true }

func TestBinancePublicTransportDistinguishesEveryBodyFailure(t *testing.T) {
	tests := []struct {
		name    string
		body    io.Reader
		cause   string
		kind    exchangecontracts.ErrorKind
		bytes   uint64
		content int64
	}{
		{name: "timeout", body: &forensicFailingReader{payload: []byte("part"), err: forensicTimeout{}},
			cause: "response_body_timeout", kind: exchangecontracts.ErrorTransient, bytes: 4, content: 10},
		{name: "interrupted", body: &forensicFailingReader{payload: []byte("part"), err: io.ErrUnexpectedEOF},
			cause: "response_body_read_failed", kind: exchangecontracts.ErrorTransient, bytes: 4, content: 10},
		{name: "empty", body: strings.NewReader(""), cause: "response_body_empty",
			kind: exchangecontracts.ErrorTransient, content: 0},
		{name: "oversized", body: strings.NewReader(strings.Repeat("x", publicBodyLimit+1)),
			cause: "response_body_too_large", kind: exchangecontracts.ErrorValidation,
			bytes: publicBodyLimit + 1, content: publicBodyLimit + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
			client, _ := NewPublicClient(publicEndpointSet, clock)
			client.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					ContentLength: test.content, Body: forensicReadCloser{reader: test.body}}, nil
			})}
			err := client.Ping(context.Background())
			var failure *exchangecontracts.Error
			if !errors.As(err, &failure) || failure.Kind != test.kind || failure.Cause != test.cause ||
				failure.HTTPStatus != http.StatusOK || failure.Metadata.ResponseBytes != test.bytes ||
				!failure.Metadata.ContentLengthKnown ||
				failure.Metadata.ContentLengthBytes != uint64(max(test.content, 0)) ||
				failure.Metadata.BodyLimitBytes != publicBodyLimit {
				t.Fatalf("failure=%#v", failure)
			}
			if failure.Metadata.RequestDuration < 0 || failure.Metadata.ResponseHeaderDuration < 0 ||
				failure.Metadata.ResponseBodyDuration < 0 {
				t.Fatalf("negative timing=%#v", failure.Metadata)
			}
		})
	}
}
