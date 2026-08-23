package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	analyticsEndpoint   = "https://beacon.neureka.dev/api/v1/analytics/events"
	analyticsAPIKey     = "bcn_7b6ed689_2d8901dd275541bf6c2ed9b4e01fa912"
	heartbeatInterval   = 15 * time.Minute
	analyticsTimeout    = 3 * time.Second
	maxResponseBody     = 64 * 1024
	analyticsLogContext = "warden.telemetry"
)

type debugger interface {
	Debug(context, message string, detail ...string)
}

type analyticsEvent struct {
	Event      string         `json:"event"`
	InstallID  string         `json:"installId"`
	Timestamp  string         `json:"timestamp,omitempty"`
	Release    string         `json:"release,omitempty"`
	Platform   string         `json:"platform,omitempty"`
	Properties map[string]any `json:"properties"`
}

type analyticsEnvelope struct {
	Events []analyticsEvent `json:"events"`
}

type eventTicker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

// Analytics sends anonymous Beacon installation lifecycle events.
type Analytics struct {
	client         *http.Client
	endpoint       string
	apiKey         string
	installID      string
	release        string
	platform       string
	debug          debugger
	now            func() time.Time
	newTicker      func(time.Duration) eventTicker
	requestTimeout time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time
}

// NewAnalytics constructs a best-effort Beacon lifecycle reporter.
func NewAnalytics(installID, release, platform string, debug debugger) *Analytics {
	return &Analytics{
		client:         &http.Client{Timeout: analyticsTimeout},
		endpoint:       analyticsEndpoint,
		apiKey:         analyticsAPIKey,
		installID:      strings.TrimSpace(installID),
		release:        strings.TrimSpace(release),
		platform:       strings.TrimSpace(platform),
		debug:          debug,
		now:            time.Now,
		newTicker:      func(interval time.Duration) eventTicker { return realTicker{time.NewTicker(interval)} },
		requestTimeout: analyticsTimeout,
	}
}

// Start begins non-blocking lifecycle delivery after successful application startup.
func (a *Analytics) Start(ctx context.Context) {
	if a == nil || a.installID == "" {
		return
	}
	a.startOnce.Do(func() {
		a.startedAt = a.now().UTC()
		runCtx, cancel := context.WithCancel(ctx)
		a.cancel = cancel
		a.done = make(chan struct{})
		go a.run(runCtx)
	})
}

// Stop stops heartbeats and attempts one bounded clean-exit delivery.
func (a *Analytics) Stop(ctx context.Context, reason string) {
	if a == nil || a.installID == "" {
		return
	}
	a.stopOnce.Do(func() {
		if a.cancel == nil {
			return
		}
		a.cancel()
		select {
		case <-a.done:
		case <-ctx.Done():
			return
		}
		duration := a.now().UTC().Sub(a.startedAt)
		if duration < 0 {
			duration = 0
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "shutdown"
		}
		event := a.event("app_exited", a.now().UTC(), map[string]any{
			"duration_seconds": int64(duration / time.Second),
			"reason":           reason,
		})
		a.deliverAndLog(ctx, event)
		a.client.CloseIdleConnections()
	})
}

func (a *Analytics) run(ctx context.Context) {
	defer close(a.done)
	a.deliverAndLog(ctx, a.event("app_started", a.startedAt, map[string]any{}))
	ticker := a.newTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			a.deliverAndLog(ctx, a.event("heartbeat", a.now().UTC(), map[string]any{}))
		}
	}
}

func (a *Analytics) event(name string, timestamp time.Time, properties map[string]any) analyticsEvent {
	if properties == nil {
		properties = map[string]any{}
	}
	return analyticsEvent{
		Event: name, InstallID: a.installID, Timestamp: timestamp.UTC().Format(time.RFC3339Nano),
		Release: a.release, Platform: a.platform, Properties: properties,
	}
}

func (a *Analytics) deliverAndLog(ctx context.Context, event analyticsEvent) {
	if _, err := a.deliver(ctx, event); err != nil {
		if a.debug != nil {
			a.debug.Debug(analyticsLogContext, "Beacon analytics delivery failed", err.Error())
		}
		return
	}
	if a.debug != nil {
		a.debug.Debug(analyticsLogContext, "Beacon accepted "+event.Event+" analytics event")
	}
}

func (a *Analytics) deliver(ctx context.Context, event analyticsEvent) ([]byte, error) {
	event.Event = strings.TrimSpace(event.Event)
	event.InstallID = strings.TrimSpace(event.InstallID)
	if event.Event == "" || event.InstallID == "" {
		return nil, fmt.Errorf("analytics event and installation ID must not be empty")
	}
	if event.Properties == nil {
		event.Properties = map[string]any{}
	}
	payload, err := json.Marshal(analyticsEnvelope{Events: []analyticsEvent{event}})
	if err != nil {
		return nil, fmt.Errorf("encode analytics event: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, a.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create analytics request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send analytics event: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read analytics response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return body, fmt.Errorf("analytics endpoint returned %s", response.Status)
	}
	return body, nil
}
