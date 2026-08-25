package queue

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neurekadev/warrden/internal/arr"
	"github.com/neurekadev/warrden/internal/config"
	"github.com/neurekadev/warrden/internal/output"
)

type fakeClient struct {
	items   []arr.QueueItem
	deletes []deleteCall
	failID  int
}

type deleteCall struct {
	id        int
	blocklist bool
}

func (*fakeClient) Instance() string { return "Sonarr" }
func (f *fakeClient) Queue(context.Context) ([]arr.QueueItem, error) {
	return append([]arr.QueueItem(nil), f.items...), nil
}
func (f *fakeClient) DeleteQueue(_ context.Context, id int, blocklist bool) error {
	if id == f.failID {
		return errors.New("delete failed")
	}
	f.deletes = append(f.deletes, deleteCall{id: id, blocklist: blocklist})
	return nil
}

func TestCleanerAppliesOrderedMixedActions(t *testing.T) {
	t.Parallel()
	client := &fakeClient{items: []arr.QueueItem{
		{ID: 1, TrackedDownloadStatus: "warning", ErrorMessage: stringPointer("Not an upgrade for existing episode"), Episode: episode("The Boys", 2019, 1, 1, "The Name of the Game")},
		{ID: 2, StatusMessages: []arr.StatusMessage{{Messages: []string{"No files found are eligible"}}}, Episode: episode("Game of Thrones", 2011, 2, 3, "Valar Morghulis")},
		{ID: 3, ErrorMessage: stringPointer("Not an upgrade for existing episode and No files found are eligible"), Title: stringPointer("first rule wins")},
		{ID: 4, TrackedDownloadStatus: "warning", ErrorMessage: stringPointer("unmatched warning"), Title: stringPointer("ignored")},
		{ID: 5, Title: stringPointer("healthy")},
	}}
	var buffer bytes.Buffer
	out := output.New(&buffer, output.Info, time.UTC, nil)
	rules := []config.Rule{
		{Match: "NO_FILES_ELIGIBLE", Action: config.RemoveAndBlocklist},
		{Match: "NOT_QUALITY_UPGRADE", Action: config.Remove},
	}
	matched, err := New(client, config.Sonarr, false, rules, out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if matched != 3 {
		t.Fatalf("matched = %d, want 3", matched)
	}
	wantDeletes := []deleteCall{{id: 1}, {id: 2, blocklist: true}, {id: 3, blocklist: true}}
	if !reflect.DeepEqual(client.deletes, wantDeletes) {
		t.Fatalf("deletes = %#v, want %#v", client.deletes, wantDeletes)
	}
	text := buffer.String()
	for _, want := range []string{
		" INFO] [sonarr.queue]",
		" • Total Queue:   5",
		" • Warnings:      4",
		" • Matched:       3",
		" • Result:        Removed 1, Blocklisted 2",
		"The Boys (2019) - S01E01 - The Name of the Game  NOT_QUALITY_UPGRADE",
		"Game of Thrones (2011) - S02E03 - Valar Morghulis  NO_FILES_ELIGIBLE",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, text)
		}
	}
	stats := strings.Index(text, "Stats:")
	results := strings.Index(text, "Results:")
	if stats < 0 || results < stats {
		t.Fatalf("stats must precede results:\n%s", text)
	}
}

func TestCleanerDryRunDoesNotDelete(t *testing.T) {
	t.Parallel()
	client := &fakeClient{items: []arr.QueueItem{{ID: 1, ErrorMessage: stringPointer("Sample"), Title: stringPointer("release")}}}
	var buffer bytes.Buffer
	out := output.New(&buffer, output.Info, time.UTC, nil)
	rules := []config.Rule{{Match: "SAMPLE", Action: config.Remove}}
	matched, err := New(client, config.Sonarr, true, rules, out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if matched != 1 || len(client.deletes) != 0 {
		t.Fatalf("matched=%d deletes=%v", matched, client.deletes)
	}
	if !strings.Contains(buffer.String(), "Would remove 1") {
		t.Fatalf("missing dry-run result:\n%s", buffer.String())
	}
}

func TestCleanerExcludesFailedDeletionsFromResults(t *testing.T) {
	t.Parallel()
	client := &fakeClient{failID: 1, items: []arr.QueueItem{{ID: 1, ErrorMessage: stringPointer("Sample"), Title: stringPointer("release")}}}
	var buffer bytes.Buffer
	out := output.New(&buffer, output.Info, time.UTC, nil)
	matched, err := New(client, config.Sonarr, false, []config.Rule{{Match: "SAMPLE", Action: config.Remove}}, out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if matched != 0 {
		t.Fatalf("matched = %d, want 0", matched)
	}
	if !strings.Contains(buffer.String(), "Failed to remove queue item 1") {
		t.Fatalf("missing deletion warning:\n%s", buffer.String())
	}
}

func TestTitleFallbacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		item arr.QueueItem
		want string
	}{
		{arr.QueueItem{ID: 1, Movie: &arr.QueueMovie{ID: 8, Title: stringPointer("Inception"), Year: 2010}}, "Inception (2010)"},
		{arr.QueueItem{ID: 2, Artist: &arr.QueueArtist{ID: 4, Name: stringPointer("Muse")}, Album: &arr.QueueAlbum{Title: stringPointer("Absolution")}}, "Muse - Absolution"},
		{arr.QueueItem{ID: 3, Title: stringPointer("Release.Name")}, "Release.Name"},
		{arr.QueueItem{ID: 4}, "ID 4"},
		{arr.QueueItem{ID: 5, Title: stringPointer("")}, ""},
		{arr.QueueItem{ID: 6, Movie: &arr.QueueMovie{ID: 6, Title: stringPointer("")}}, ""},
	}
	for _, test := range tests {
		if got := title(test.item); got != test.want {
			t.Errorf("title(%+v) = %q, want %q", test.item, got, test.want)
		}
	}
}

func episode(series string, year, season, number int, title string) *arr.QueueEpisode {
	return &arr.QueueEpisode{Title: stringPointer(title), SeasonNumber: season, EpisodeNumber: number, Series: &arr.QueueSeries{Title: stringPointer(series), Year: year}}
}

func stringPointer(value string) *string { return &value }
