package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

func TestCaptureAddsLegacyDiagnosticContext(t *testing.T) {
	t.Parallel()
	transport := &captureTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: dsn, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	reporter := &Reporter{hub: sentry.NewHub(client, sentry.NewScope())}

	reporter.Capture(errors.New("boom"), "series.missing", "Missing search job failed")
	event := transport.captured()
	if event == nil {
		t.Fatal("no event captured")
	}
	if event.Tags["context"] != "series.missing" {
		t.Fatalf("context tag = %q", event.Tags["context"])
	}
	if event.Message != "Missing search job failed" {
		t.Fatalf("message = %q", event.Message)
	}
	if len(event.Exception) != 1 || event.Exception[0].Value != "boom" {
		t.Fatalf("exceptions = %#v", event.Exception)
	}

	reporter.Capture(nil, "ignored", "ignored")
	if transport.sends != 1 {
		t.Fatalf("nil error changed send count to %d", transport.sends)
	}
}

func TestNewUsesConfiguredRelease(t *testing.T) {
	t.Parallel()
	reporter := New("4.2.0")
	if reporter.hub == nil {
		t.Fatal("valid DSN unexpectedly disabled reporting")
	}
	if release := reporter.hub.Client().Options().Release; release != "4.2.0" {
		t.Fatalf("release = %q, want configured release", release)
	}
}

type captureTransport struct {
	mu    sync.Mutex
	event *sentry.Event
	sends int
}

func (*captureTransport) Configure(sentry.ClientOptions)        {}
func (*captureTransport) Flush(time.Duration) bool              { return true }
func (*captureTransport) FlushWithContext(context.Context) bool { return true }
func (*captureTransport) Close()                                {}
func (t *captureTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.event = event
	t.sends++
}

func (t *captureTransport) captured() *sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.event
}
