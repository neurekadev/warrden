package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neurekadev/warrden/internal/arr"
	"github.com/neurekadev/warrden/internal/config"
	"github.com/neurekadev/warrden/internal/health"
	"github.com/neurekadev/warrden/internal/output"
)

func TestAliasArgsDispatchesByExecutableName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"/app/bin/warrden", "clear-missing", "Series"}, []string{"clear-missing", "Series"}},
		{[]string{"/app/bin/clear-missing", "Series"}, []string{"clear-missing", "Series"}},
		{[]string{"/app/bin/clear-upgrades"}, []string{"clear-upgrades"}},
		{nil, nil},
	}
	for _, test := range tests {
		if got := aliasArgs(test.args); !reflect.DeepEqual(got, test.want) {
			t.Errorf("aliasArgs(%v) = %v, want %v", test.args, got, test.want)
		}
	}
}

func TestRunClearAliasCreatesGoDatabase(t *testing.T) {
	directory := prepareRunTest(t)
	t.Setenv("CONFIG_PATH", filepath.Join(directory, "ignored-config.yaml"))
	t.Setenv("DATABASE_PATH", filepath.Join(directory, "ignored.db"))
	var stdout bytes.Buffer
	code := Run(context.Background(), []string{"/app/bin/clear-missing", "Series"}, &stdout)
	if code != 0 {
		t.Fatalf("exit=%d output:\n%s", code, stdout.String())
	}
	for _, want := range []string{"INFO] [cli.series.clear-missing]", "Type:       Missing", "Cleared:    0 entries"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q:\n%s", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "data", "warrden.db")); err != nil {
		t.Fatalf("database was not created: %v", err)
	}
}

func TestDeploymentEnvironment(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"": "development", "dev": "development", "DEV": "development",
		"edge-abc123": "edge", "4.7.0": "production",
	}
	for release, want := range tests {
		if got := deploymentEnvironment(release); got != want {
			t.Errorf("deploymentEnvironment(%q)=%q, want %q", release, got, want)
		}
	}
}

func TestRunReportsUnknownCommandOnStdout(t *testing.T) {
	prepareRunTest(t)
	var stdout bytes.Buffer
	code := Run(context.Background(), []string{"warrden", "unknown"}, &stdout)
	if code != 1 {
		t.Fatalf("exit=%d", code)
	}
	text := stdout.String()
	if !strings.Contains(text, "Unknown command: unknown") || !strings.Contains(text, "Available commands: clear-missing [instance], clear-upgrades [instance]") {
		t.Fatalf("unexpected output:\n%s", text)
	}
}

func TestRunRejectsInvalidIdentityBeforeDatabaseOpen(t *testing.T) {
	directory := prepareRunTest(t)
	t.Setenv("PUID", "0")
	t.Setenv("PGID", "1000")
	var stdout bytes.Buffer
	code := Run(context.Background(), []string{"warrden", "clear-missing"}, &stdout)
	if code != 1 || !strings.Contains(stdout.String(), "Container identity setup failed") {
		t.Fatalf("exit=%d output:\n%s", code, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "data", "warrden.db")); !os.IsNotExist(err) {
		t.Fatalf("database should not be opened, stat error=%v", err)
	}
}

func TestRunMissingConfigUsesEstablishedError(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("CONFIG_PATH", filepath.Join(directory, "ignored.yaml"))
	t.Setenv("DATABASE_PATH", filepath.Join(directory, "ignored.db"))
	uid, gid := testIdentity()
	t.Setenv("PUID", uid)
	t.Setenv("PGID", gid)
	var stdout bytes.Buffer
	code := Run(context.Background(), []string{"warrden", "clear-missing"}, &stdout)
	if code != 1 || !strings.Contains(stdout.String(), "Config file not found: ") {
		t.Fatalf("exit=%d output:\n%s", code, stdout.String())
	}
}

func TestRunDisablesRejectedAPIKeyAndShutsDown(t *testing.T) {
	directory := prepareRunTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/system/status" || r.Header.Get("X-Api-Key") != "secret" {
			t.Errorf("unexpected validation request: %s key=%q", r.URL.Path, r.Header.Get("X-Api-Key"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	configPath := filepath.Join(directory, "data", "config.yaml")
	content := "logLevel: info\ninstances:\n  - type: sonarr\n    enabled: true\n    name: Series\n    url: " + server.URL + "\n    apiVersion: v3\n    apiKey: secret\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := newReadyWriter()
	done := make(chan int, 1)
	dependencies := defaultRunDependencies()
	analytics := &fakeAnalytics{}
	dependencies.newAnalytics = func(string, string, string, outputDebugger) lifecycleAnalytics {
		return analytics
	}
	go func() { done <- run(ctx, []string{"warrden"}, stdout, dependencies) }()
	select {
	case <-stdout.ready:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("startup banner was not written")
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit=%d output:\n%s", code, stdout.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not shut down")
	}
	text := stdout.String()
	if !strings.Contains(text, "DISABLED — API key rejected (401 Unauthorized)") || !strings.Contains(text, "Fix the API key and restart wArrden") {
		t.Fatalf("missing disabled health state:\n%s", text)
	}
	analytics.mu.Lock()
	defer analytics.mu.Unlock()
	if !analytics.started || !analytics.stopped || analytics.reason != "shutdown" {
		t.Fatalf("analytics lifecycle started=%t stopped=%t reason=%q", analytics.started, analytics.stopped, analytics.reason)
	}
}

func TestResolveTimezone(t *testing.T) {
	t.Parallel()
	location, warning := resolveTimezone(":America/Los_Angeles")
	if warning != "" || location.String() != "America/Los_Angeles" {
		t.Fatalf("location=%s warning=%q", location, warning)
	}
	location, warning = resolveTimezone("Not/AZone")
	want := "Invalid timezone 'Not/AZone' — falling back to UTC: The time zone ID 'Not/AZone' was not found on the local computer."
	if location != time.UTC || warning != want {
		t.Fatalf("location=%s warning=%q", location, warning)
	}
}

func TestEmbeddedTimezoneDataPreservesDST(t *testing.T) {
	t.Parallel()
	location, warning := resolveTimezone("America/Los_Angeles")
	if warning != "" {
		t.Fatal(warning)
	}
	_, winterOffset := time.Date(2026, time.January, 15, 12, 0, 0, 0, location).Zone()
	_, summerOffset := time.Date(2026, time.July, 15, 12, 0, 0, 0, location).Zone()
	if winterOffset != -8*60*60 || summerOffset != -7*60*60 {
		t.Fatalf("Los Angeles offsets = winter %d summer %d", winterOffset, summerOffset)
	}
}

func TestValidationWarningsAreStableAndDoNotMutateConfig(t *testing.T) {
	t.Parallel()
	secondSeen := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one/api/v3/system/status":
			<-secondSeen
			w.WriteHeader(http.StatusInternalServerError)
		case "/two/api/v3/system/status":
			close(secondSeen)
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	instances := make([]config.Instance, 2)
	clients := make(map[string]*arr.Client, 2)
	shared := server.Client()
	for index, name := range []string{"One", "Two"} {
		instanceURL := *base
		instanceURL.Path = "/" + strings.ToLower(name)
		instance := config.Instance{Kind: config.Sonarr, Enabled: true, Name: name, URL: &instanceURL, URLText: instanceURL.String(), APIVersion: "v3", APIKey: "secret"}
		instances[index] = instance
		clients[instance.Key()] = arr.NewClient(arr.ClientOptions{Instance: instance, HTTPClient: shared, AttemptTimeout: time.Second, BaseDelay: time.Nanosecond})
	}
	cfg := &config.Config{Instances: instances}
	warnings := validateInstances(context.Background(), cfg, clients, health.New())
	if len(warnings) != 2 || !strings.Contains(warnings[0], "Unexpected response from One") || !strings.Contains(warnings[1], "Unexpected response from Two") {
		t.Fatalf("warnings are not in config order: %v", warnings)
	}
	if got := cfg.Warnings(); len(got) != 0 {
		t.Fatalf("normalized config was mutated: %v", got)
	}
}

func TestFailureReportingFiltersExpectedArrFailures(t *testing.T) {
	t.Parallel()
	reporter := &captureReporter{}
	var stdout bytes.Buffer
	out := output.New(&stdout, output.Debug, time.UTC, reporter)
	instance := config.Instance{Kind: config.Sonarr, Name: "Series"}
	tracker := health.New()

	reportFailure(out, tracker, instance, "series.missing", "Missing search job failed", &arr.HTTPError{StatusCode: 500, Status: "Internal Server Error"})
	reportFailure(out, tracker, instance, "series.missing", "Missing search job failed", &arr.HTTPError{StatusCode: 401, Status: "Unauthorized"})
	if reporter.count != 0 {
		t.Fatalf("expected arr failures reached telemetry: %d", reporter.count)
	}
	reportFailure(out, tracker, instance, "series.missing", "Missing search job failed", errors.New("defect"))
	if reporter.count != 1 {
		t.Fatalf("unexpected defect captures = %d, want 1", reporter.count)
	}
}

func prepareRunTest(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.MkdirAll(filepath.Join(directory, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "data", "config.yaml")
	content := `logLevel: info
instances:
  - type: sonarr
    enabled: false
    name: Series
    url: http://127.0.0.1:8989
    apiVersion: v3
    apiKey: secret
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	uid, gid := testIdentity()
	t.Setenv("PUID", uid)
	t.Setenv("PGID", gid)
	t.Setenv("TZ", "UTC")
	t.Setenv("GIT_TAG", "test")
	return directory
}

type readyWriter struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

type captureReporter struct{ count int }

type fakeAnalytics struct {
	mu      sync.Mutex
	started bool
	stopped bool
	reason  string
}

func (r *captureReporter) Capture(error, string, string) { r.count++ }

func (a *fakeAnalytics) Start(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = true
}

func (a *fakeAnalytics) Stop(_ context.Context, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = true
	a.reason = reason
}

func newReadyWriter() *readyWriter { return &readyWriter{ready: make(chan struct{})} }

func (w *readyWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	count, err := w.data.Write(data)
	if strings.Contains(w.data.String(), "[system.ready]") {
		w.once.Do(func() { close(w.ready) })
	}
	return count, err
}

func (w *readyWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.String()
}
