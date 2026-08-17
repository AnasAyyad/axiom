package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRecordedSegmentCommitRetriesSerializationFailure(t *testing.T) {
	manifest, finalizedAt := recordedSegmentCommitFixture()
	attempts := 0
	var delays []time.Duration
	committer := &RecordedSegmentCommitter{
		attempt: func(_ context.Context, session, exchange string, actual segments.Manifest, at time.Time) error {
			attempts++
			if session != "evaluation-session" || exchange != "binance" || actual != manifest || at != finalizedAt {
				t.Fatal("retry changed immutable segment input")
			}
			if attempts < 3 {
				return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
			}
			return nil
		},
		wait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	}

	if err := committer.Commit(context.Background(), "evaluation-session", "binance", manifest, finalizedAt); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || len(delays) != 2 || delays[0] != 25*time.Millisecond || delays[1] != 50*time.Millisecond {
		t.Fatalf("attempts=%d delays=%v", attempts, delays)
	}
}

func TestRecordedSegmentCommitBoundsSerializationRetries(t *testing.T) {
	manifest, finalizedAt := recordedSegmentCommitFixture()
	attempts := 0
	waits := 0
	serializationFailure := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	committer := &RecordedSegmentCommitter{
		attempt: func(context.Context, string, string, segments.Manifest, time.Time) error {
			attempts++
			return serializationFailure
		},
		wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	}

	err := committer.Commit(context.Background(), "evaluation-session", "bybit", manifest, finalizedAt)
	if !errors.Is(err, serializationFailure) || attempts != recordedSegmentCommitAttempts || waits != recordedSegmentCommitAttempts-1 {
		t.Fatalf("error=%v attempts=%d waits=%d", err, attempts, waits)
	}
}

func TestRecordedSegmentCommitDoesNotRetryPermanentFailure(t *testing.T) {
	manifest, finalizedAt := recordedSegmentCommitFixture()
	attempts := 0
	permanent := errors.New("permanent")
	committer := &RecordedSegmentCommitter{
		attempt: func(context.Context, string, string, segments.Manifest, time.Time) error {
			attempts++
			return permanent
		},
		wait: func(context.Context, time.Duration) error {
			t.Fatal("permanent failure waited for retry")
			return nil
		},
	}

	if err := committer.Commit(context.Background(), "evaluation-session", "binance", manifest, finalizedAt); !errors.Is(err, permanent) || attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}

func TestRecordedSegmentCommitRetryHonorsCancellation(t *testing.T) {
	manifest, finalizedAt := recordedSegmentCommitFixture()
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	committer := &RecordedSegmentCommitter{
		attempt: func(context.Context, string, string, segments.Manifest, time.Time) error {
			attempts++
			cancel()
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		},
	}

	if err := committer.Commit(ctx, "evaluation-session", "binance", manifest, finalizedAt); !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}

func recordedSegmentCommitFixture() (segments.Manifest, time.Time) {
	startedAt := time.Date(2026, 8, 16, 20, 0, 0, 0, time.UTC)
	return segments.Manifest{
		Spec: segments.Spec{
			Name:                 "evaluation-session-000001-wire",
			SchemaVersion:        "market-wire.v1",
			ParserVersion:        "wire",
			NormalizationVersion: "wire",
			OrderedContentHash:   strings.Repeat("a", 64),
			FirstOrdinal:         1,
			LastOrdinal:          10,
			RecordCount:          10,
			StartedAt:            startedAt,
			EndedAt:              startedAt.Add(time.Minute),
		},
		Path:               "evaluation-session-000001-wire.parquet",
		Checksum:           strings.Repeat("b", 64),
		OrderedContentHash: strings.Repeat("a", 64),
		Size:               1024,
		Format:             "parquet",
		Compression:        "zstd",
	}, startedAt.Add(2 * time.Minute)
}
