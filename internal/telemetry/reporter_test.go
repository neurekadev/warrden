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

func TestNewConfiguresEventOnlyReportingAndInstallUser(t *testing.T) {
	t.Parallel()
	transport := &captureTransport{}
	installID := "f08e267c-9070-4f3a-a485-5fcfa26a1670"
	reporter := newReporter("4.2.0", "production", installID, transport)
	if reporter.hub == nil {
		t.Fatal("valid DSN unexpectedly disabled reporting")
	}
	options := reporter.hub.Client().Options()
	if options.Release != "4.2.0" || options.Environment != "production" {
		t.Fatalf("options release=%q environment=%q", options.Release, options.Environment)
	}
	if options.EnableTracing || options.TracesSampleRate != 0 || !options.DisableLogs || !options.DisableMetrics || !options.DisableClientReports || !options.DisableTelemetryBuffer {
		t.Fatalf("non-error Sentry features were not disabled: %#v", options)
	}
	reporter.Capture(errors.New("boom"), "warden.telemetry", "Controlled report")
	event := transport.captured()
	if event == nil {
		t.Fatal("no event captured")
	}
	if event.User.ID != installID {
		t.Fatalf("captured user=%#v, want install ID %q", event.User, installID)
	}
}

func TestNewDisablesReportingWithoutInstallID(t *testing.T) {
	t.Parallel()
	if reporter := New("4.2.0", "production", ""); reporter.hub != nil {
		t.Fatal("reporting was enabled without a stable installation ID")
	}
}

func TestRecoverCapturesAndPreservesPanic(t *testing.T) {
	t.Parallel()
	transport := &captureTransport{}
	reporter := newReporter("4.2.0", "production", "f08e267c-9070-4f3a-a485-5fcfa26a1670", transport)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		func() {
			defer reporter.Recover()
			panic("controlled panic")
		}()
	}()
	if recovered != "controlled panic" {
		t.Fatalf("recovered=%v, want controlled panic", recovered)
	}
	event := transport.captured()
	if event == nil || event.Level != sentry.LevelFatal || event.Message != "controlled panic" {
		t.Fatalf("panic event=%#v", event)
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
