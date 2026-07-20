package postgres

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
