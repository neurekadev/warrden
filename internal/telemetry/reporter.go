// Package telemetry isolates optional Beacon/Sentry error reporting.
package telemetry

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
)

const dsn = "https://ca3bcb7569913062d079984cce25219e@beacon.neureka.dev/api/v1/sentry/2"

// Reporter captures unexpected wArrden defects without affecting application behavior.
type Reporter struct{ hub *sentry.Hub }

// New initializes reporting. Initialization failure intentionally returns a disabled reporter.
func New(release, environment, installID string) *Reporter {
	return newReporter(release, environment, installID, nil)
}

func newReporter(release, environment, installID string, transport sentry.Transport) *Reporter {
	if strings.TrimSpace(installID) == "" {
		return &Reporter{}
	}
	options := sentry.ClientOptions{
		Dsn:                    dsn,
		Environment:            strings.TrimSpace(environment),
		EnableTracing:          false,
		TracesSampleRate:       0,
		DisableLogs:            true,
		DisableMetrics:         true,
		DisableClientReports:   true,
		DisableTelemetryBuffer: true,
	}
	if strings.TrimSpace(release) != "" {
		options.Release = strings.TrimSpace(release)
	}
	if transport != nil {
		options.Transport = transport
	}
	client, err := sentry.NewClient(options)
	if err != nil {
		return &Reporter{}
	}
	scope := sentry.NewScope()
	scope.SetUser(sentry.User{ID: installID})
	return &Reporter{hub: sentry.NewHub(client, scope)}
}

// Capture reports an unexpected error with its log context.
func (r *Reporter) Capture(err error, contextName, message string) {
	if r.hub == nil || err == nil {
		return
	}
	hub := r.hub.Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("context", contextName)
		scope.AddEventProcessor(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			event.Message = message
			return event
		})
		hub.CaptureException(err)
	})
}

// Recover captures an unhandled panic and then preserves the panic.
func (r *Reporter) Recover() {
	panicValue := recover()
	if panicValue == nil {
		return
	}
	if r.hub != nil {
		r.hub.Recover(panicValue)
	}
	panic(panicValue)
}

// Flush waits for pending reports until ctx expires.
func (r *Reporter) Flush(ctx context.Context) bool {
	if r.hub == nil {
		return true
	}
	return r.hub.FlushWithContext(ctx)
}
