package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertCoordinationGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 14, 10, 4, 0, 0, time.UTC)
	assertCommandGuards(t, ctx, pool, now)
	assertOutboxGuards(t, ctx, pool, now)
	assertCursorGuards(t, ctx, pool, now)
	assertJobGuards(t, ctx, pool, now)
}

func assertCommandGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO command_requests
      (id,deduplication_key,payload_hash,configuration_id,state,created_at)
      VALUES ('command-a','dedupe-a',$1,'configuration-a','pending',$2)`, fixedHash("e"), now)
	if err != nil {
		t.Fatalf("command seed failed: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE command_requests SET state='applied',applied_at=$1 WHERE id='command-a'", now); err != nil {
		t.Fatalf("command completion rejected: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE command_requests SET payload_hash=$1 WHERE id='command-a'", fixedHash("f")); err == nil {
		t.Fatal("completed command mutated")
	}
}

func assertOutboxGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, "UPDATE outbox_events SET published_at=$1 WHERE id='outbox-unit'", now); err != nil {
		t.Fatalf("outbox publication rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE outbox_events SET published_at=$1 WHERE id='outbox-unit'", now.Add(time.Second)); err == nil {
		t.Fatal("outbox event republished")
	}
}

func assertCursorGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, "INSERT INTO consumer_cursors VALUES ('consumer-a',1,$1)", now); err != nil {
		t.Fatalf("cursor seed failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE consumer_cursors SET outbox_revision=0,updated_at=$1 WHERE consumer='consumer-a'", now); err == nil {
		t.Fatal("consumer cursor moved backward")
	}
	if _, err := pool.Exec(ctx, "UPDATE consumer_cursors SET outbox_revision=2,updated_at=$1 WHERE consumer='consumer-a'", now); err != nil {
		t.Fatalf("consumer cursor advance rejected: %v", err)
	}
}

func assertJobGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO jobs
      (id,job_type,idempotency_key,state,claim_owner,claim_epoch,claim_expires_at,payload_hash,created_at,updated_at)
      VALUES ('job-invalid','test','job-invalid','RUNNING','worker',1,$1,$2,$3,$3)`,
		now.Add(time.Minute), fixedHash("1"), now); err == nil {
		t.Fatal("job inserted directly as claimed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO jobs
      (id,job_type,idempotency_key,state,payload_hash,created_at,updated_at)
      VALUES ('job-a','test','job-a','QUEUED',$1,$2,$2)`, fixedHash("2"), now); err != nil {
		t.Fatalf("job seed failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='RUNNING',claim_owner='worker',claim_epoch=1,
      claim_expires_at=$1,started_at=$2,progress_revision=2,updated_at=$2 WHERE id='job-a'`, now.Add(time.Minute), now); err != nil {
		t.Fatalf("job claim rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE jobs SET claim_owner='other',updated_at=$1 WHERE id='job-a'", now.Add(2*time.Second)); err == nil {
		t.Fatal("active job ownership mutated")
	}
	if _, err := pool.Exec(ctx, "UPDATE jobs SET state='SUCCEEDED',claim_expires_at=NULL,completed_at=$1,progress_revision=3,updated_at=$1 WHERE id='job-a'", now.Add(2*time.Second)); err != nil {
		t.Fatalf("job completion rejected: %v", err)
	}
}
