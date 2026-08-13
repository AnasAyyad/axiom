package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

const (
	ownerConsoleStreamHeartbeat = 15 * time.Second
	ownerConsoleStreamBatch     = 256
	ownerConsoleMaximumStreams  = 3
	ownerConsoleStreamWriteWait = 5 * time.Second
)

// Serve streams durable outbox events after validating retention and user quota.
func (store *OwnerConsoleStore) Serve(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return console.ErrUnavailable
	}
	after, err := ownerConsoleResumeRevision(request)
	if err != nil {
		return err
	}
	connectionID, err := store.openOwnerConsoleStream(request.Context(), principal, after)
	if err != nil {
		return err
	}
	defer store.closeOwnerConsoleStream(connectionID)
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	if err = ownerConsoleSetStreamWriteDeadline(writer); err != nil {
		return console.ErrUnavailable
	}
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	return store.runOwnerConsoleStream(request.Context(), writer, flusher, connectionID, principal, after)
}

func (store *OwnerConsoleStore) openOwnerConsoleStream(ctx context.Context, principal authentication.Principal, after int64) (string, error) {
	now := store.clock.Now().UTC
	var oldest int64
	err := store.pool.QueryRow(ctx, `SELECT coalesce(min(revision),0) FROM outbox_events WHERE created_at >= $1 OR revision > (SELECT greatest(coalesce(max(revision),0)-100000,0) FROM outbox_events)`, now.Add(-24*time.Hour)).Scan(&oldest)
	if err != nil {
		return "", err
	}
	if after > 0 && oldest > 0 && after < oldest-1 {
		return "", console.ErrCursorExpired
	}
	connectionID, _ := ownerConsoleIdentifier("stream")
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, _ = tx.Exec(ctx, `UPDATE stream_connections SET closed_at=$1 WHERE closed_at IS NULL AND heartbeat_at<$2`, now, now.Add(-45*time.Second))
	var active int
	if err = tx.QueryRow(ctx, `SELECT count(*)::integer FROM stream_connections WHERE user_id=$1 AND closed_at IS NULL`, principal.UserID).Scan(&active); err != nil {
		return "", err
	}
	if active >= ownerConsoleMaximumStreams {
		return "", console.ErrQuota
	}
	if _, err = tx.Exec(ctx, `INSERT INTO stream_connections(id,user_id,session_id,opened_at,heartbeat_at,last_revision) VALUES($1,$2,$3,$4,$4,$5)`, connectionID, principal.UserID, principal.SessionID, now, after); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return connectionID, nil
}

func (store *OwnerConsoleStore) closeOwnerConsoleStream(connectionID string) {
	_, _ = store.pool.Exec(context.Background(), `UPDATE stream_connections SET closed_at=$2 WHERE id=$1 AND closed_at IS NULL`, connectionID, store.clock.Now().UTC)
}

func (store *OwnerConsoleStore) runOwnerConsoleStream(
	ctx context.Context,
	writer http.ResponseWriter,
	flusher http.Flusher,
	connectionID string,
	principal authentication.Principal,
	after int64,
) error {
	poll := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(ownerConsoleStreamHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	revision := after
	for {
		sent, next, sendErr := store.writeOwnerConsoleEvents(ctx, writer, principal, revision)
		if sendErr != nil {
			return nil
		}
		if next > revision {
			revision = next
			if sent {
				flusher.Flush()
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
		case <-heartbeat.C:
			if ownerConsoleSetStreamWriteDeadline(writer) != nil {
				return nil
			}
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
			_, _ = store.pool.Exec(ctx, `UPDATE stream_connections SET heartbeat_at=$2,last_revision=$3 WHERE id=$1 AND closed_at IS NULL`, connectionID, store.clock.Now().UTC, revision)
		}
	}
}

func (store *OwnerConsoleStore) writeOwnerConsoleEvents(
	ctx context.Context,
	writer http.ResponseWriter,
	principal authentication.Principal,
	after int64,
) (bool, int64, error) {
	rows, err := store.pool.Query(ctx, `SELECT revision,id,topic,stream,schema_version,entity_revision,event_time,correlation_id,causation_id,payload FROM outbox_events WHERE revision>$1 ORDER BY revision LIMIT $2`, after, ownerConsoleStreamBatch)
	if err != nil {
		return false, after, err
	}
	defer rows.Close()
	sent := false
	revision := after
	for rows.Next() {
		var event generated.StreamEvent
		var payload []byte
		var rawRevision, entityRevision int64
		if err = rows.Scan(&rawRevision, &event.Id, &event.EventType, &event.Stream, &event.SchemaVersion, &entityRevision, &event.OccurredAt, &event.CorrelationId, &event.CausationId, &payload); err != nil {
			return false, after, err
		}
		event.Revision = strconv.FormatInt(rawRevision, 10)
		event.EntityRevision = strconv.FormatInt(entityRevision, 10)
		revision = rawRevision
		if !ownerConsoleStreamAllowed(principal, string(event.Stream)) {
			continue
		}
		if err = json.Unmarshal(payload, &event.Payload); err != nil {
			event.Payload = map[string]any{"redacted": true}
		}
		encoded, _ := json.Marshal(event)
		if err = ownerConsoleSetStreamWriteDeadline(writer); err != nil {
			return false, after, err
		}
		if _, err = fmt.Fprintf(writer, "id: %d\ndata: %s\n\n", rawRevision, encoded); err != nil {
			return false, after, err
		}
		sent = true
	}
	return sent, revision, rows.Err()
}

func ownerConsoleStreamAllowed(principal authentication.Principal, stream string) bool {
	if principal.UserID == "" {
		return false
	}
	switch stream {
	case "activity", "configuration", "export", "qualification", "sandbox", "research", "shadow":
		return true
	case "alert", "exchange", "fill", "incident", "inventory", "job",
		"opportunity", "order", "portfolio", "rebalancing", "risk",
		"strategy", "system", "trend":
		return true
	default:
		return false
	}
}

func ownerConsoleSetStreamWriteDeadline(writer http.ResponseWriter) error {
	err := http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(ownerConsoleStreamWriteWait))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func ownerConsoleResumeRevision(request *http.Request) (int64, error) {
	raw := request.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = request.URL.Query().Get("after_revision")
	}
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, console.ErrInvalidRequest
	}
	return value, nil
}

var _ console.StreamService = (*OwnerConsoleStore)(nil)
