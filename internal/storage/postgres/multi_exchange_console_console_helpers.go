package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

func multiExchangeConsoleSnapshotRevision(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM outbox_events`).Scan(&revision); err != nil {
		return "", err
	}
	return strconv.FormatInt(revision, 10), nil
}

func multiExchangeConsoleQuality(source string, observed time.Time, confidence generated.QualityEvidenceConfidence,
	freshness generated.QualityEvidenceFreshness, warnings ...string) generated.QualityEvidence {
	tier := generated.QualityEvidenceTier("local_tier_b")
	if confidence == generated.QualityEvidenceConfidence("insufficient") ||
		confidence == generated.QualityEvidenceConfidence("unknown") {
		tier = generated.QualityEvidenceTier("unknown")
	}
	result := generated.QualityEvidence{Tier: tier, Confidence: confidence, Freshness: freshness,
		Source: source, ObservedAt: observed.UTC(), ProvenanceComplete: true}
	if len(warnings) > 0 {
		result.Warnings = &warnings
	}
	return result
}

func multiExchangeConsoleFreshness(now, observed time.Time) generated.QualityEvidenceFreshness {
	age := now.Sub(observed)
	switch {
	case age < 0:
		return generated.QualityEvidenceFreshness("stale")
	case age <= 5*time.Second:
		return generated.QualityEvidenceFreshness("live")
	case age <= 5*time.Minute:
		return generated.QualityEvidenceFreshness("fresh")
	case age <= 24*time.Hour:
		return generated.QualityEvidenceFreshness("stale")
	default:
		return generated.QualityEvidenceFreshness("historical")
	}
}

func decodeMultiExchangeConsoleStringCursor(codec console.CursorCodec, scope, cursor string) (string, error) {
	return codec.Decode(scope, cursor)
}

func encodeMultiExchangeConsoleStringCursor(codec console.CursorCodec, scope, position string) string {
	return codec.Encode(scope, position)
}

func multiExchangeConsolePositiveAge(now, observed time.Time) string {
	if now.Before(observed) {
		return "0"
	}
	return strconv.FormatInt(now.Sub(observed).Nanoseconds(), 10)
}

func decodeMultiExchangeConsoleTimeCursor(codec console.CursorCodec, scope, cursor string) (time.Time, string, error) {
	position, err := codec.Decode(scope, cursor)
	if err != nil || position == "" {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(position, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", console.ErrInvalidRequest
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || parts[1] == "" {
		return time.Time{}, "", console.ErrInvalidRequest
	}
	return recordedAt.UTC(), parts[1], nil
}

func encodeMultiExchangeConsoleTimeCursor(codec console.CursorCodec, scope string, recordedAt time.Time, id string) string {
	return codec.Encode(scope, fmt.Sprintf("%s|%s", recordedAt.UTC().Format(time.RFC3339Nano), id))
}
