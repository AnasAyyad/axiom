package runtimecore

import (
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestCommandInboxOutboxIdempotencyAndRestart(t *testing.T) {
	repository := NewMemoryCoordinationRepository()
	command := durableCommand(t, "command-a", "dedupe-a", "payload-a")
	factoryCalls := 0
	factory := func() ([]OutboxDraft, error) {
		factoryCalls++
		return []OutboxDraft{outboxDraft(t, "outbox-a", "runtime.event", "event-a")}, nil
	}
	first, err := repository.ApplyCommand(command, factory)
	if err != nil || !first.Applied || len(first.Outbox) != 1 || first.Outbox[0].Revision != 1 {
		t.Fatalf("first result = %#v, %v", first, err)
	}
	duplicate, err := repository.ApplyCommand(command, factory)
	if err != nil || duplicate.Applied || factoryCalls != 1 || duplicate.Outbox[0].Revision != 1 {
		t.Fatalf("duplicate = %#v, calls=%d, %v", duplicate, factoryCalls, err)
	}
	restored := RestoreMemoryCoordinationRepository(repository.Snapshot())
	afterRestart, err := restored.ApplyCommand(command, factory)
	if err != nil || afterRestart.Applied || factoryCalls != 1 {
		t.Fatalf("restart duplicate = %#v, calls=%d, %v", afterRestart, factoryCalls, err)
	}
	page, err := restored.ReadOutbox(0, 10)
	if err != nil || len(page) != 1 || page[0].Revision != 1 {
		t.Fatalf("outbox page = %#v, %v", page, err)
	}
}

func TestInboxDeduplicationConflictAndStoreLoss(t *testing.T) {
	repository := NewMemoryCoordinationRepository()
	inboxID, _ := domain.NewInboxMessageID("message-a")
	message := InboxMessage{ID: inboxID, Consumer: "worker-a", PayloadHash: PayloadDigest([]byte("payload-a"))}
	factory := func() ([]OutboxDraft, error) {
		return []OutboxDraft{outboxDraft(t, "outbox-a", "worker.event", "event-a")}, nil
	}
	if result, err := repository.ConsumeInbox(message, factory); err != nil || !result.Applied {
		t.Fatalf("inbox result = %#v, %v", result, err)
	}
	message.PayloadHash = PayloadDigest([]byte("different"))
	if _, err := repository.ConsumeInbox(message, factory); err == nil {
		t.Fatal("same inbox identity with different payload accepted")
	}
	repository.SetAvailable(false)
	if _, err := repository.ReadOutbox(0, 10); err == nil {
		t.Fatal("outbox read succeeded during store loss")
	}
}

func TestOutboxCursorIsMonotonicAndNotificationIndependent(t *testing.T) {
	repository := NewMemoryCoordinationRepository()
	for index, value := range []string{"a", "b", "c"} {
		command := durableCommand(t, "command-"+value, "dedupe-"+value, "payload-"+value)
		draft := outboxDraft(t, "outbox-"+value, "topic", "event-"+value)
		if _, err := repository.ApplyCommand(command, func() ([]OutboxDraft, error) { return []OutboxDraft{draft}, nil }); err != nil {
			t.Fatal(err)
		}
		page, err := repository.ReadOutbox(uint64(index), 1)
		if err != nil || page[0].Revision != uint64(index+1) {
			t.Fatalf("cursor page = %#v, %v", page, err)
		}
	}
}

func durableCommand(t *testing.T, idValue, dedupe, payload string) DurableCommand {
	t.Helper()
	id, err := domain.NewCommandID(idValue)
	if err != nil {
		t.Fatal(err)
	}
	return DurableCommand{
		ID: id, DeduplicationKey: dedupe, PayloadHash: PayloadDigest([]byte(payload)),
		ConfigurationHash: PayloadDigest([]byte("configuration")),
		CreatedAt:         domain.EventTime{UTC: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), Sequence: 1},
	}
}

func outboxDraft(t *testing.T, idValue, topic, payload string) OutboxDraft {
	t.Helper()
	id, err := domain.NewOutboxMessageID(idValue)
	if err != nil {
		t.Fatal(err)
	}
	return OutboxDraft{ID: id, Topic: topic, PayloadHash: PayloadDigest([]byte(payload))}
}
