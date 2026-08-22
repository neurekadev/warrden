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
func New(release string) *Reporter {
	options := sentry.ClientOptions{Dsn: dsn}
	if strings.TrimSpace(release) != "" {
		options.Release = release
	}
	client, err := sentry.NewClient(options)
	if err != nil {
		return &Reporter{}
	}
	return &Reporter{hub: sentry.NewHub(client, sentry.NewScope())}
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

// Flush waits for pending reports until ctx expires.
func (r *Reporter) Flush(ctx context.Context) bool {
	if r.hub == nil {
		return true
	}
	return r.hub.FlushWithContext(ctx)
}
