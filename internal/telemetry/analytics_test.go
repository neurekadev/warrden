package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBeaconAnalyticsConnectionConstants(t *testing.T) {
	t.Parallel()
	if analyticsEndpoint != "https://beacon.neureka.dev/api/v1/analytics/events" {
		t.Fatalf("endpoint=%q", analyticsEndpoint)
	}
	if analyticsAPIKey != "bcn_7b6ed689_2d8901dd275541bf6c2ed9b4e01fa912" {
		t.Fatal("embedded Beacon ingestion key does not match the project key")
	}
}

func TestAnalyticsDeliverUsesBeaconContract(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method=%s", request.Method)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type=%q", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer public-key" {
			t.Errorf("authorization=%q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"accepted":1}`))
	}))
	t.Cleanup(server.Close)

	analytics := testAnalytics(server)
	timestamp := time.Date(2026, time.August, 22, 12, 0, 0, 123_000_000, time.UTC)
	body, err := analytics.deliver(context.Background(), analytics.event("app_started", timestamp, nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"accepted":1}` {
		t.Fatalf("body=%q", body)
	}
	if got := mapKeys(captured); !reflect.DeepEqual(got, []string{"events"}) {
		t.Fatalf("envelope keys=%v", got)
	}
	events, ok := captured["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events=%#v", captured["events"])
	}
	event, ok := events[0].(map[string]any)
	if !ok {
		t.Fatalf("event=%#v", events[0])
	}
	if got := mapKeys(event); !reflect.DeepEqual(got, []string{"event", "installId", "platform", "properties", "release", "timestamp"}) {
		t.Fatalf("event keys=%v", got)
	}
	if event["event"] != "app_started" || event["installId"] != "f08e267c-9070-4f3a-a485-5fcfa26a1670" || event["release"] != "4.7.0" || event["platform"] != "linux-amd64" {
		t.Fatalf("event=%#v", event)
	}
	if event["timestamp"] != "2026-08-22T12:00:00.123Z" {
		t.Fatalf("timestamp=%q", event["timestamp"])
	}
	properties, ok := event["properties"].(map[string]any)
	if !ok || properties == nil || len(properties) != 0 {
		t.Fatalf("properties=%#v", event["properties"])
	}
}

func TestAnalyticsLifecycleSendsStartedHeartbeatAndCleanExit(t *testing.T) {
	t.Parallel()
	events := make(chan analyticsEvent, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope analyticsEnvelope
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(envelope.Events) != 1 {
			t.Errorf("events=%d", len(envelope.Events))
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		events <- envelope.Events[0]
		_, _ = writer.Write([]byte(`{"accepted":1}`))
	}))
	t.Cleanup(server.Close)

	analytics := testAnalytics(server)
	ticker := &fakeEventTicker{ticks: make(chan time.Time, 1)}
	analytics.newTicker = func(interval time.Duration) eventTicker {
		if interval != 15*time.Minute {
			t.Errorf("heartbeat interval=%s", interval)
		}
		return ticker
	}
	start := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	times := []time.Time{start, start.Add(15 * time.Minute), start.Add(901 * time.Second), start.Add(901 * time.Second)}
	var timeMu sync.Mutex
	analytics.now = func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		value := times[0]
		times = times[1:]
		return value
	}

	runCtx, cancel := context.WithCancel(context.Background())
	analytics.Start(runCtx)
	started := receiveEvent(t, events)
	if started.Event != "app_started" || started.Properties == nil {
		t.Fatalf("started=%#v", started)
	}
	ticker.ticks <- start.Add(15 * time.Minute)
	heartbeat := receiveEvent(t, events)
	if heartbeat.Event != "heartbeat" || heartbeat.Properties == nil {
		t.Fatalf("heartbeat=%#v", heartbeat)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	analytics.Stop(stopCtx, "shutdown")
	exited := receiveEvent(t, events)
	if exited.Event != "app_exited" || exited.Properties["duration_seconds"] != float64(901) || exited.Properties["reason"] != "shutdown" {
		t.Fatalf("exited=%#v", exited)
	}
	if !ticker.stopped {
		t.Fatal("heartbeat ticker was not stopped")
	}
}

func TestAnalyticsDeliveryFailureIsBounded(t *testing.T) {
	t.Parallel()
	analytics := NewAnalytics("f08e267c-9070-4f3a-a485-5fcfa26a1670", "4.7.0", "linux-amd64", nil)
	analytics.client = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	analytics.endpoint = "https://beacon.invalid/events"
	analytics.requestTimeout = 10 * time.Millisecond
	_, err := analytics.deliver(context.Background(), analytics.event("heartbeat", time.Now(), nil))
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnalyticsRejectsNonSuccessResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "rejected", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	analytics := testAnalytics(server)
	_, err := analytics.deliver(context.Background(), analytics.event("heartbeat", time.Now(), nil))
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnalyticsRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()
	analytics := NewAnalytics("f08e267c-9070-4f3a-a485-5fcfa26a1670", "4.7.0", "linux-amd64", nil)
	for _, event := range []analyticsEvent{
		{InstallID: analytics.installID, Properties: map[string]any{}},
		{Event: "heartbeat", Properties: map[string]any{}},
	} {
		if _, err := analytics.deliver(context.Background(), event); err == nil {
			t.Fatalf("event=%#v did not fail validation", event)
		}
	}
}

func testAnalytics(server *httptest.Server) *Analytics {
	analytics := NewAnalytics("f08e267c-9070-4f3a-a485-5fcfa26a1670", "4.7.0", "linux-amd64", nil)
	analytics.client = server.Client()
	analytics.endpoint = server.URL
	analytics.apiKey = "public-key"
	return analytics
}

func receiveEvent(t *testing.T, events <-chan analyticsEvent) analyticsEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for analytics event")
		return analyticsEvent{}
	}
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

type fakeEventTicker struct {
	ticks   chan time.Time
	stopped bool
}

func (t *fakeEventTicker) C() <-chan time.Time { return t.ticks }
func (t *fakeEventTicker) Stop()               { t.stopped = true }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
