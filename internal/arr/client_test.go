package arr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neurekadev/warrden/internal/config"
)

func TestQueuePaginatesAndDeduplicates(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Api-Key") != "secret" {
			t.Errorf("API key = %q", r.Header.Get("X-Api-Key"))
		}
		if r.URL.Path != "/root/api/v3/queue" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("includeUnknownSeriesItems"); got != "true" {
			t.Errorf("includeUnknownSeriesItems = %q", got)
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			records := make([]QueueItem, 100)
			for index := range records {
				records[index] = QueueItem{ID: index + 1, Title: stringPointer("old")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"page": 1, "pageSize": 100, "totalRecords": 101, "records": records})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{"page": 2, "pageSize": 100, "totalRecords": 101, "records": []QueueItem{{ID: 1, Title: stringPointer("new")}, {ID: 101, Title: stringPointer("last")}}})
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	t.Cleanup(server.Close)

	client := testClient(t, server.URL+"/root", config.Sonarr, "v3", 0)
	items, err := client.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(items); got != 101 {
		t.Fatalf("got %d queue items, want 101", got)
	}
	if items[0].ID != 1 || items[0].Title == nil || *items[0].Title != "new" || items[100].ID != 101 {
		t.Fatalf("deduplication/order mismatch: first=%+v last=%+v", items[0], items[100])
	}
	if requests.Load() != 2 {
		t.Fatalf("got %d requests, want 2", requests.Load())
	}
}

func TestQueueContinuesAfterFullDuplicatePage(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page: %v", err)
		}
		requests.Add(1)
		records := make([]QueueItem, 0, 100)
		if page <= 2 {
			for index := range 100 {
				records = append(records, QueueItem{ID: index + 1})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": page, "pageSize": 100, "totalRecords": 200, "records": records})
	}))
	t.Cleanup(server.Close)

	client := testClient(t, server.URL, config.Sonarr, "v3", 0)
	items, err := client.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 100 {
		t.Fatalf("got %d deduplicated items, want 100", len(items))
	}
	if requests.Load() != 3 {
		t.Fatalf("got %d requests, want 3", requests.Load())
	}
}

func stringPointer(value string) *string { return &value }

func TestFormatUserAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "release", version: "1.2.3", want: "wArrden/1.2.3"},
		{name: "development fallback", want: "wArrden/dev"},
		{name: "invalid token characters", version: " v1 beta/2 ", want: "wArrden/v1-beta-2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := formatUserAgent(test.version); got != test.want {
				t.Errorf("formatUserAgent(%q) = %q, want %q", test.version, got, test.want)
			}
		})
	}
}

func TestClientUsesApplicationSpecificEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		kind          config.Kind
		version       string
		wantDeleteRaw string
		wantCommand   map[string]any
		call          func(context.Context, *Client) error
	}{
		{
			name:          "radarr",
			kind:          config.Radarr,
			version:       "v3",
			wantDeleteRaw: "blocklist=true&skipRedownload=false",
			wantCommand:   map[string]any{"name": "MoviesSearch", "movieIds": []any{float64(4), float64(8)}},
			call: func(ctx context.Context, client *Client) error {
				if err := client.DeleteQueue(ctx, 9, true); err != nil {
					return err
				}
				return client.SearchMovies(ctx, []int{4, 8})
			},
		},
		{
			name:          "sonarr",
			kind:          config.Sonarr,
			version:       "v3",
			wantDeleteRaw: "blocklist=true&skipRedownload=false",
			wantCommand:   map[string]any{"name": "SeasonSearch", "seriesId": float64(12), "seasonNumber": float64(3)},
			call: func(ctx context.Context, client *Client) error {
				if err := client.DeleteQueue(ctx, 9, true); err != nil {
					return err
				}
				return client.SearchSeason(ctx, 12, 3)
			},
		},
		{
			name:          "lidarr",
			kind:          config.Lidarr,
			version:       "v1",
			wantDeleteRaw: "blocklist=false&removeFromClient=true",
			wantCommand:   map[string]any{"name": "ArtistSearch", "artistId": float64(44)},
			call: func(ctx context.Context, client *Client) error {
				if err := client.DeleteQueue(ctx, 9, false); err != nil {
					return err
				}
				return client.SearchArtist(ctx, 44)
			},
		},
		{
			name:          "whisparr",
			kind:          config.Whisparr,
			version:       "v3",
			wantDeleteRaw: "blocklist=true&skipRedownload=false",
			wantCommand:   map[string]any{"name": "SeasonSearch", "seriesId": float64(12), "seasonNumber": float64(3)},
			call: func(ctx context.Context, client *Client) error {
				if err := client.DeleteQueue(ctx, 9, true); err != nil {
					return err
				}
				return client.SearchSeason(ctx, 12, 3)
			},
		},
		{
			name:          "whisparr eros",
			kind:          config.Whisparr,
			version:       "v3-eros",
			wantDeleteRaw: "removeFromClient=true&blocklist=true&skipRedownload=false",
			wantCommand:   map[string]any{"name": "MoviesSearch", "movieIds": []any{float64(4), float64(8)}},
			call: func(ctx context.Context, client *Client) error {
				if err := client.DeleteQueue(ctx, 9, true); err != nil {
					return err
				}
				return client.SearchMovies(ctx, []int{4, 8})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var command map[string]any
			var deleteQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.UserAgent(); got != "wArrden/dev" {
					t.Errorf("User-Agent = %q, want %q", got, "wArrden/dev")
				}
				switch r.Method {
				case http.MethodDelete:
					deleteQuery = r.URL.RawQuery
				case http.MethodPost:
					if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
						t.Errorf("decode command: %v", err)
					}
				default:
					t.Errorf("method = %s", r.Method)
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(server.Close)
			client := testClient(t, server.URL, test.kind, test.version, 0)
			if err := test.call(context.Background(), client); err != nil {
				t.Fatal(err)
			}
			if deleteQuery != test.wantDeleteRaw {
				t.Errorf("delete query = %q, want %q", deleteQuery, test.wantDeleteRaw)
			}
			if !reflect.DeepEqual(command, test.wantCommand) {
				t.Errorf("command = %#v, want %#v", command, test.wantCommand)
			}
		})
	}
}

func TestWhisparrErosUsesMovieQueueAndWantedContracts(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"page":1,"pageSize":100,"totalRecords":0,"records":[]}`)
	}))
	t.Cleanup(server.Close)

	client := testClient(t, server.URL, config.Whisparr, "v3-eros", 0)
	if _, err := client.Queue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WantedMovies(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WantedMovies(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	wants := []string{
		"/api/v3/queue?includeUnknownMovieItems=true&includeMovie=true&page=1&pageSize=100",
		"/api/v3/wanted/missing?monitored=true&page=1&pageSize=100&sortKey=movies.lastSearchTime&sortDirection=ascending",
		"/api/v3/wanted/cutoff?monitored=true&page=1&pageSize=100&sortKey=movies.lastSearchTime&sortDirection=ascending",
	}
	if !reflect.DeepEqual(paths, wants) {
		t.Fatalf("request contracts = %#v, want %#v", paths, wants)
	}
}

func TestClientRetriesTransientFailuresOnly(t *testing.T) {
	t.Parallel()
	t.Run("server error", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.UserAgent(); got != "wArrden/dev" {
				t.Errorf("retry User-Agent = %q, want %q", got, "wArrden/dev")
			}
			if attempts.Add(1) < 3 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, `{"version":"1"}`)
		}))
		t.Cleanup(server.Close)
		client := testClient(t, server.URL, config.Radarr, "v3", 2)
		status, text, err := client.Validate(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusOK || text != "OK" || attempts.Load() != 3 {
			t.Fatalf("status=%d text=%q attempts=%d", status, text, attempts.Load())
		}
	})

	t.Run("client error", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)
		client := testClient(t, server.URL, config.Radarr, "v3", 3)
		status, text, err := client.Validate(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusUnauthorized || text != "Unauthorized" || attempts.Load() != 1 {
			t.Fatalf("status=%d text=%q attempts=%d", status, text, attempts.Load())
		}
	})

	t.Run("startup status uses legacy enum name", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(server.Close)
		client := testClient(t, server.URL, config.Radarr, "v3", 0)
		status, text, err := client.Validate(context.Background())
		if err != nil || status != http.StatusBadGateway || text != "BadGateway" {
			t.Fatalf("status=%d text=%q err=%v", status, text, err)
		}
	})

	t.Run("permanent transport error", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return nil, errors.New("invalid transport configuration")
		})
		client := clientWithTransport(t, transport, 3)
		_, _, err := client.Validate(context.Background())
		if err == nil || attempts.Load() != 1 {
			t.Fatalf("err=%v attempts=%d, want error after one attempt", err, attempts.Load())
		}
	})

	t.Run("transient network error", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			if attempts.Add(1) == 1 {
				return nil, &net.DNSError{Err: "temporary", IsTemporary: true}
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})
		client := clientWithTransport(t, transport, 1)
		status, _, err := client.Validate(context.Background())
		if err != nil || status != http.StatusOK || attempts.Load() != 2 {
			t.Fatalf("status=%d err=%v attempts=%d", status, err, attempts.Load())
		}
	})
}

func TestEnsureTagPreservesUnknownJSONFields(t *testing.T) {
	t.Parallel()
	var put map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"id":7,"title":"Show","tags":[2],"future":{"nested":true},"nullable":null}`)
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&put); err != nil {
				t.Errorf("decode PUT: %v", err)
			}
		default:
			t.Errorf("method = %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	client := testClient(t, server.URL, config.Sonarr, "v3", 0)
	changed, err := client.EnsureSeriesTag(context.Background(), 7, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected tag change")
	}
	if string(put["future"]) != `{"nested":true}` || string(put["nullable"]) != "null" {
		t.Fatalf("unknown fields were not retained: %#v", put)
	}
	var tags []int
	if err := json.Unmarshal(put["tags"], &tags); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tags, []int{2, 5}) {
		t.Fatalf("tags = %v", tags)
	}
}

func TestResolveArtistIDsUsesTopLevelArtistIDOnly(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"artistId":0,"artist":{"id":7}},{"artistId":9,"artist":{"id":8}}]`)
	}))
	t.Cleanup(server.Close)
	client := testClient(t, server.URL, config.Lidarr, "v1", 0)
	ids, err := client.ResolveArtistIDs(context.Background(), []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, hasFallback := ids[7]; hasFallback {
		t.Fatal("nested artist ID was used as a fallback")
	}
	if _, ok := ids[9]; !ok || len(ids) != 1 {
		t.Fatalf("resolved IDs = %v, want only 9", ids)
	}
}

func TestEnsureTagLeavesNullResourceUnchanged(t *testing.T) {
	t.Parallel()
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		_, _ = io.WriteString(w, `null`)
	}))
	t.Cleanup(server.Close)
	client := testClient(t, server.URL, config.Sonarr, "v3", 0)
	changed, err := client.EnsureSeriesTag(context.Background(), 7, 5)
	if err != nil {
		t.Fatal(err)
	}
	if changed || puts.Load() != 0 {
		t.Fatalf("changed=%t PUTs=%d, want unchanged without PUT", changed, puts.Load())
	}
}

func TestClientHonorsCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := testClient(t, server.URL, config.Sonarr, "v3", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := client.Validate(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context canceled", err)
	}
}

func TestPerAttemptTimeoutIsRetried(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(ClientOptions{
		Instance:       config.Instance{Kind: config.Sonarr, Name: "Test", URL: parsed, APIKey: "secret"},
		RetryCount:     1,
		AttemptTimeout: 20 * time.Millisecond,
		BaseDelay:      time.Nanosecond,
	})
	t.Cleanup(client.Close)
	status, _, err := client.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || attempts.Load() != 2 {
		t.Fatalf("status=%d attempts=%d", status, attempts.Load())
	}
}

func TestRetryJitterIsBounded(t *testing.T) {
	t.Parallel()
	for range 100 {
		delay := jitter(time.Second, 100)
		if delay < 15*time.Second || delay > 30*time.Second {
			t.Fatalf("delay %s outside bounded jitter range", delay)
		}
	}
}

func TestClientClosesRetryAndReturnedBodies(t *testing.T) {
	t.Parallel()
	transport := &bodyTrackingTransport{}
	parsed, err := url.Parse("http://arr.test")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(ClientOptions{
		Instance:       config.Instance{Kind: config.Sonarr, Name: "Test", URL: parsed, APIKey: "secret"},
		RetryCount:     1,
		AttemptTimeout: time.Second,
		BaseDelay:      time.Nanosecond,
		HTTPClient:     &http.Client{Transport: transport},
	})
	status, _, err := client.Validate(context.Background())
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.bodies) != 2 || !transport.bodies[0].closed || !transport.bodies[1].closed {
		t.Fatalf("response bodies not closed: %#v", transport.bodies)
	}
}

func testClient(t *testing.T, rawURL string, kind config.Kind, version string, retries int) *Client {
	t.Helper()
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(ClientOptions{
		Instance: config.Instance{
			Kind:       kind,
			Name:       "Test",
			URL:        parsed,
			URLText:    rawURL,
			APIVersion: version,
			APIKey:     "secret",
		},
		RetryCount:     retries,
		AttemptTimeout: time.Second,
		BaseDelay:      time.Nanosecond,
	})
	t.Cleanup(client.Close)
	return client
}

type trackedBody struct{ closed bool }

func (*trackedBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

type bodyTrackingTransport struct {
	mu     sync.Mutex
	bodies []*trackedBody
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func clientWithTransport(t *testing.T, transport http.RoundTripper, retries int) *Client {
	t.Helper()
	parsed, err := url.Parse("http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(ClientOptions{
		Instance:       config.Instance{Kind: config.Radarr, Name: "Test", URL: parsed, APIKey: "secret"},
		RetryCount:     retries,
		AttemptTimeout: time.Second,
		BaseDelay:      time.Nanosecond,
		HTTPClient:     &http.Client{Transport: transport},
	})
}

func (t *bodyTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	body := &trackedBody{}
	t.bodies = append(t.bodies, body)
	status := http.StatusBadGateway
	if len(t.bodies) == 2 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: body, Header: make(http.Header)}, nil
}
