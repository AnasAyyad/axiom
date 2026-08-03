package postgres

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"axiom/internal/authentication"
)

func TestA11StreamWriteDeadlineBoundsSlowConsumers(t *testing.T) {
	if err := a11SetStreamWriteDeadline(httptest.NewRecorder()); err != nil {
		t.Fatalf("unsupported recorder deadline was not ignored: %v", err)
	}
	writer := &a11DeadlineWriter{header: make(http.Header)}
	before := time.Now()
	if err := a11SetStreamWriteDeadline(writer); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || writer.deadline.Before(before.Add(a11StreamWriteWait-time.Second)) ||
		writer.deadline.After(time.Now().Add(a11StreamWriteWait+time.Second)) {
		t.Fatalf("stream deadline = %s calls=%d", writer.deadline, writer.calls)
	}
	want := errors.New("slow_consumer")
	writer.err = want
	if err := a11SetStreamWriteDeadline(writer); !errors.Is(err, want) {
		t.Fatalf("slow-consumer deadline failure hidden: %v", err)
	}
}

func TestD1StreamPermissionsAreFilteredByEventClass(t *testing.T) {
	operator := authentication.Principal{Permissions: []string{
		"operations.read", "activity.read", "qualification.monitor",
	}}
	for stream, want := range map[string]bool{
		"activity": true, "qualification": true, "risk": true,
		"configuration": false, "export": false, "sandbox": false, "unknown": false,
	} {
		if got := a11StreamAllowed(operator, stream); got != want {
			t.Errorf("operator stream %s = %t, want %t", stream, got, want)
		}
	}
	auditor := authentication.Principal{Permissions: []string{
		"operations.read", "activity.read", "artifacts.read",
	}}
	if !a11StreamAllowed(auditor, "export") || a11StreamAllowed(auditor, "configuration") {
		t.Fatal("auditor export/configuration stream boundary is open")
	}
}

type a11DeadlineWriter struct {
	header   http.Header
	deadline time.Time
	calls    int
	err      error
}

func (writer *a11DeadlineWriter) Header() http.Header      { return writer.header }
func (*a11DeadlineWriter) Write(value []byte) (int, error) { return len(value), nil }
func (*a11DeadlineWriter) WriteHeader(int)                 {}
func (writer *a11DeadlineWriter) SetWriteDeadline(at time.Time) error {
	writer.calls++
	writer.deadline = at
	return writer.err
}
