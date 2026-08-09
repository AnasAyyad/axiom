package postgres

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"axiom/internal/authentication"
)

func TestOwnerConsoleStreamWriteDeadlineBoundsSlowConsumers(t *testing.T) {
	if err := ownerConsoleSetStreamWriteDeadline(httptest.NewRecorder()); err != nil {
		t.Fatalf("unsupported recorder deadline was not ignored: %v", err)
	}
	writer := &ownerConsoleDeadlineWriter{header: make(http.Header)}
	before := time.Now()
	if err := ownerConsoleSetStreamWriteDeadline(writer); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || writer.deadline.Before(before.Add(ownerConsoleStreamWriteWait-time.Second)) ||
		writer.deadline.After(time.Now().Add(ownerConsoleStreamWriteWait+time.Second)) {
		t.Fatalf("stream deadline = %s calls=%d", writer.deadline, writer.calls)
	}
	want := errors.New("slow_consumer")
	writer.err = want
	if err := ownerConsoleSetStreamWriteDeadline(writer); !errors.Is(err, want) {
		t.Fatalf("slow-consumer deadline failure hidden: %v", err)
	}
}

func TestOwnerStreamIncludesEverySanitizedProductEvent(t *testing.T) {
	operator := authentication.Principal{UserID: "owner"}
	for stream, want := range map[string]bool{
		"activity": true, "qualification": true, "risk": true,
		"configuration": true, "export": true, "sandbox": true, "unknown": false,
	} {
		if got := ownerConsoleStreamAllowed(operator, stream); got != want {
			t.Errorf("operator stream %s = %t, want %t", stream, got, want)
		}
	}
	if ownerConsoleStreamAllowed(authentication.Principal{}, "activity") {
		t.Fatal("anonymous stream boundary is open")
	}
}

type ownerConsoleDeadlineWriter struct {
	header   http.Header
	deadline time.Time
	calls    int
	err      error
}

func (writer *ownerConsoleDeadlineWriter) Header() http.Header      { return writer.header }
func (*ownerConsoleDeadlineWriter) Write(value []byte) (int, error) { return len(value), nil }
func (*ownerConsoleDeadlineWriter) WriteHeader(int)                 {}
func (writer *ownerConsoleDeadlineWriter) SetWriteDeadline(at time.Time) error {
	writer.calls++
	writer.deadline = at
	return writer.err
}
