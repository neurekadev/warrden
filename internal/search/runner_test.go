package search

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neurekadev/warrden/internal/arr"
	"github.com/neurekadev/warrden/internal/config"
	"github.com/neurekadev/warrden/internal/output"
)

type fakeCooldown struct {
	mu       sync.Mutex
	blocked  map[int]struct{}
	cleaned  []string
	marked   []int
	category string
}

func (f *fakeCooldown) CleanExpired(_ context.Context, _, category string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned = append(f.cleaned, category)
	return nil
}
func (f *fakeCooldown) IDs(context.Context, string, string) (map[int]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := make(map[int]struct{}, len(f.blocked))
	for id := range f.blocked {
		copy[id] = struct{}{}
	}
	return copy, nil
}
func (f *fakeCooldown) Mark(_ context.Context, _, category string, ids []int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.category = category
	f.marked = append([]int(nil), ids...)
	return nil
}

func TestEpisodeSearchPreservesSelectionAndOutputSemantics(t *testing.T) {
	t.Parallel()
	var command []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/wanted/missing":
			_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 3, "records": []arr.Episode{
				{ID: 1, SeriesID: 10, Series: &arr.EpisodeSeries{Title: stringPointer("Zulu"), Year: 2020}, SeasonNumber: 1, EpisodeNumber: 2, Title: stringPointer("Second")},
				{ID: 2, SeriesID: 20, Series: &arr.EpisodeSeries{Title: stringPointer("Cooldown"), Year: 2021}, SeasonNumber: 1, EpisodeNumber: 1, Title: stringPointer("Skipped")},
				{ID: 3, SeriesID: 30, Series: &arr.EpisodeSeries{Title: stringPointer("Alpha"), Year: 2019}, SeasonNumber: 2, EpisodeNumber: 4, Title: stringPointer("Fourth")},
			}})
		case "/api/v3/indexer":
			_ = json.NewEncoder(w).Encode([]arr.Indexer{{Name: stringPointer("Usenet"), EnableAutomaticSearch: true}})
		case "/api/v3/command":
			var body struct {
				Name       string `json:"name"`
				EpisodeIDs []int  `json:"episodeIds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode command: %v", err)
			}
			if body.Name != "EpisodeSearch" {
				t.Errorf("command = %q", body.Name)
			}
			command = append([]int(nil), body.EpisodeIDs...)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(server.Close)

	instance, client := searchClient(t, server.URL, config.Sonarr)
	store := &fakeCooldown{blocked: map[int]struct{}{2: {}}}
	var buffer bytes.Buffer
	runner := New(store, output.New(&buffer, output.Info, time.UTC, nil), nil)
	runner.intN = func(int) int { return 0 }
	job := &config.Job{MaxResults: 2, Cooldown: time.Hour, SearchType: config.Episode}
	if err := runner.Run(context.Background(), client, instance, job, true, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(command, []int{3, 1}) {
		t.Fatalf("command IDs = %v, want [3 1] in displayed order", command)
	}
	if !reflect.DeepEqual(store.marked, []int{3, 1}) || store.category != "Missing" {
		t.Fatalf("marked=%v category=%q", store.marked, store.category)
	}
	text := buffer.String()
	for _, want := range []string{
		"Total Items:   3",
		"On Cooldown:   1",
		"Eligible:      2",
		"Result:        Searched 2",
		"Alpha (2019) - S02E04 - Fourth",
		"Zulu (2020) - S01E02 - Second",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
}

func TestDryRunSkipsIndexerSearchAndCooldownWrite(t *testing.T) {
	t.Parallel()
	var unexpected int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/wanted/missing" {
			unexpected++
			t.Errorf("unexpected dry-run request: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 1, "records": []arr.Movie{{ID: 8, Title: stringPointer("Inception"), Year: 2010}}})
	}))
	t.Cleanup(server.Close)
	instance, client := searchClient(t, server.URL, config.Radarr)
	store := &fakeCooldown{blocked: map[int]struct{}{}}
	var buffer bytes.Buffer
	runner := New(store, output.New(&buffer, output.Info, time.UTC, nil), nil)
	runner.intN = func(int) int { return 0 }
	job := &config.Job{MaxResults: 1, Cooldown: time.Hour}
	if err := runner.Run(context.Background(), client, instance, job, true, true); err != nil {
		t.Fatal(err)
	}
	if unexpected != 0 || len(store.marked) != 0 {
		t.Fatalf("unexpected requests=%d marked=%v", unexpected, store.marked)
	}
	if strings.Contains(buffer.String(), "Results:") || !strings.Contains(buffer.String(), "No search performed") {
		t.Fatalf("unexpected dry-run output:\n%s", buffer.String())
	}
}

func TestSeasonSearchRecordsOnlySuccessfulGroups(t *testing.T) {
	t.Parallel()
	var commands int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/wanted/missing":
			_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 3, "records": []arr.Episode{
				{ID: 1, SeriesID: 1, SeasonNumber: 1, Series: &arr.EpisodeSeries{Title: stringPointer("Alpha"), Year: 2020}},
				{ID: 2, SeriesID: 1, SeasonNumber: 1, Series: &arr.EpisodeSeries{Title: stringPointer("Alpha"), Year: 2020}},
				{ID: 3, SeriesID: 2, SeasonNumber: 3, Series: &arr.EpisodeSeries{Title: stringPointer("Beta"), Year: 2021}},
			}})
		case "/api/v3/indexer":
			_ = json.NewEncoder(w).Encode([]arr.Indexer{{EnableAutomaticSearch: true}})
		case "/api/v3/command":
			commands++
			if commands == 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	instance, client := searchClient(t, server.URL, config.Sonarr)
	store := &fakeCooldown{blocked: map[int]struct{}{}}
	var buffer bytes.Buffer
	runner := New(store, output.New(&buffer, output.Info, time.UTC, nil), nil)
	runner.intN = func(int) int { return 0 }
	job := &config.Job{MaxResults: 2, Cooldown: time.Hour, SearchType: config.Season}
	if err := runner.Run(context.Background(), client, instance, job, true, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.marked, []int{2003}) {
		t.Fatalf("marked = %v, want only successful season key 2003", store.marked)
	}
	if !strings.Contains(buffer.String(), "Search trigger failed for Alpha (2020) - Season 1") {
		t.Fatalf("missing partial-failure warning:\n%s", buffer.String())
	}
}

func stringPointer(value string) *string { return &value }

func TestCheckIndexersFiltersNamesCaseInsensitively(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]arr.Indexer{
			{Name: stringPointer("NZBGeek"), EnableAutomaticSearch: true},
			{Name: stringPointer("Disabled"), EnableAutomaticSearch: false},
		})
	}))
	t.Cleanup(server.Close)
	_, client := searchClient(t, server.URL, config.Radarr)
	ok, detail, err := checkIndexers(context.Background(), client, &config.IndexerFilter{Enabled: true, Include: []string{"nzbgeek"}})
	if err != nil || !ok || detail != "" {
		t.Fatalf("ok=%t detail=%q err=%v", ok, detail, err)
	}
	ok, detail, err = checkIndexers(context.Background(), client, &config.IndexerFilter{Enabled: true, Exclude: []string{"NZBGEEK"}})
	if err != nil || ok || !strings.Contains(detail, "Available: NZBGeek") {
		t.Fatalf("ok=%t detail=%q err=%v", ok, detail, err)
	}
}

func TestEnabledFilterRetainsAutomaticIndexerWithEmptyName(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"","enableAutomaticSearch":true}]`))
	}))
	t.Cleanup(server.Close)
	_, client := searchClient(t, server.URL, config.Radarr)
	ok, detail, err := checkIndexers(context.Background(), client, &config.IndexerFilter{Enabled: true})
	if err != nil || !ok || detail != "" {
		t.Fatalf("ok=%t detail=%q err=%v", ok, detail, err)
	}
}

func TestMovieUpgradeUsesCutoffEndpoint(t *testing.T) {
	t.Parallel()
	var command struct {
		Name     string `json:"name"`
		MovieIDs []int  `json:"movieIds"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/wanted/cutoff":
			_, _ = w.Write([]byte(`{"totalRecords":1,"records":[{"id":8,"title":"Inception","year":2010}]}`))
		case "/api/v3/indexer":
			_, _ = w.Write([]byte(`[{"name":"Usenet","enableAutomaticSearch":true}]`))
		case "/api/v3/command":
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Errorf("decode command: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	instance, client := searchClient(t, server.URL, config.Radarr)
	store := &fakeCooldown{blocked: map[int]struct{}{}}
	runner := New(store, output.New(&bytes.Buffer{}, output.Info, time.UTC, nil), nil)
	runner.intN = func(int) int { return 0 }
	if err := runner.Run(context.Background(), client, instance, &config.Job{MaxResults: 1, Cooldown: time.Hour}, false, false); err != nil {
		t.Fatal(err)
	}
	if command.Name != "MoviesSearch" || !reflect.DeepEqual(command.MovieIDs, []int{8}) || store.category != "Upgrade" || !reflect.DeepEqual(store.marked, []int{8}) {
		t.Fatalf("command=%+v category=%q marked=%v", command, store.category, store.marked)
	}
}

func TestLidarrAlbumAndArtistSearchModes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		searchType config.SearchType
		command    string
		category   string
		marked     []int
	}{
		{name: "album", searchType: config.Album, command: "AlbumSearch", category: "Missing", marked: []int{11}},
		{name: "artist", searchType: config.Artist, command: "ArtistSearch", category: "Missing_Artist", marked: []int{7}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var commandName string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/wanted/missing":
					_, _ = w.Write([]byte(`{"totalRecords":1,"records":[{"id":11,"album":{"id":11,"title":"Absolution","artistId":7},"artist":{"id":7,"artistName":"Muse"}}]}`))
				case "/api/v1/indexer":
					_, _ = w.Write([]byte(`[{"name":"Usenet","enableAutomaticSearch":true}]`))
				case "/api/v1/command":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode command: %v", err)
					}
					commandName, _ = body["name"].(string)
					w.WriteHeader(http.StatusCreated)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			t.Cleanup(server.Close)
			instance, client := searchClient(t, server.URL, config.Lidarr)
			instance.APIVersion = "v1"
			store := &fakeCooldown{blocked: map[int]struct{}{}}
			runner := New(store, output.New(&bytes.Buffer{}, output.Info, time.UTC, nil), nil)
			runner.intN = func(int) int { return 0 }
			job := &config.Job{MaxResults: 1, Cooldown: time.Hour, SearchType: test.searchType}
			if err := runner.Run(context.Background(), client, instance, job, true, false); err != nil {
				t.Fatal(err)
			}
			if commandName != test.command || store.category != test.category || !reflect.DeepEqual(store.marked, test.marked) {
				t.Fatalf("command=%q category=%q marked=%v", commandName, store.category, store.marked)
			}
		})
	}
}

func TestDisplayTitlesDistinguishNullFromEmpty(t *testing.T) {
	t.Parallel()
	if got := movieTitle(arr.Movie{ID: 4}); got != "Movie 4" {
		t.Fatalf("null movie title = %q", got)
	}
	if got := movieTitle(arr.Movie{ID: 4, Title: stringPointer("")}); got != "" {
		t.Fatalf("empty movie title = %q", got)
	}
	if got := episodeTitle(arr.Episode{ID: 8}); got != "Episode 8" {
		t.Fatalf("null episode title = %q", got)
	}
	if got := episodeTitle(arr.Episode{ID: 8, Title: stringPointer("")}); got != "" {
		t.Fatalf("empty episode title = %q", got)
	}
}

func TestAlbumArtistIDPreservesNestedZero(t *testing.T) {
	t.Parallel()
	album := arr.Album{Album: &arr.AlbumRecord{ArtistID: 0}, Artist: &arr.AlbumArtist{ID: 7}}
	if got := albumArtistID(album); got != 0 {
		t.Fatalf("artist ID = %d, want nested album's explicit zero", got)
	}
}

func searchClient(t *testing.T, rawURL string, kind config.Kind) (config.Instance, *arr.Client) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	instance := config.Instance{Kind: kind, Name: "Test", URL: parsed, URLText: rawURL, APIVersion: "v3", APIKey: "secret"}
	client := arr.NewClient(arr.ClientOptions{Instance: instance, AttemptTimeout: time.Second, BaseDelay: time.Nanosecond})
	t.Cleanup(client.Close)
	return instance, client
}
