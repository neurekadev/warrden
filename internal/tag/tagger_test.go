package tag

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"code.neureka.dev/warrden/warrden/internal/arr"
	"code.neureka.dev/warrden/warrden/internal/output"
)

type fakeClient struct {
	tags       []arr.Tag
	created    []string
	series     []int
	movies     []int
	artists    []int
	resolved   map[int]struct{}
	resolveErr error
}

func (*fakeClient) Instance() string { return "Test" }
func (f *fakeClient) Tags(context.Context) ([]arr.Tag, error) {
	return append([]arr.Tag(nil), f.tags...), nil
}
func (f *fakeClient) CreateTag(_ context.Context, name string) (arr.Tag, error) {
	f.created = append(f.created, name)
	return arr.Tag{ID: 9, Label: name}, nil
}
func (f *fakeClient) EnsureSeriesTag(_ context.Context, id, _ int) (bool, error) {
	f.series = append(f.series, id)
	return true, nil
}
func (f *fakeClient) EnsureMovieTag(_ context.Context, id, _ int) (bool, error) {
	f.movies = append(f.movies, id)
	return true, nil
}
func (f *fakeClient) EnsureArtistTag(_ context.Context, id, _ int) (bool, error) {
	f.artists = append(f.artists, id)
	return true, nil
}
func (f *fakeClient) ResolveSeriesIDs(context.Context, []int) (map[int]struct{}, error) {
	return f.resolved, f.resolveErr
}
func (f *fakeClient) ResolveArtistIDs(context.Context, []int) (map[int]struct{}, error) {
	return f.resolved, f.resolveErr
}

type fakeIDs struct{ values map[int]struct{} }

func (f fakeIDs) IDs(context.Context, string, string) (map[int]struct{}, error) {
	return f.values, nil
}

func TestFindOrCreateIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	tagger := New(output.New(&bytes.Buffer{}, output.Debug, time.UTC, nil), fakeIDs{})
	client := &fakeClient{tags: []arr.Tag{{ID: 4, Label: "wArrden"}}}
	id, err := tagger.FindOrCreate(context.Background(), client, "WARrDEN")
	if err != nil {
		t.Fatal(err)
	}
	if id != 4 || len(client.created) != 0 {
		t.Fatalf("id=%d created=%v", id, client.created)
	}

	client.tags = nil
	id, err = tagger.FindOrCreate(context.Background(), client, "searched")
	if err != nil {
		t.Fatal(err)
	}
	if id != 9 || !reflect.DeepEqual(client.created, []string{"searched"}) {
		t.Fatalf("id=%d created=%v", id, client.created)
	}
}

func TestDirectTaggingPreservesFirstOccurrenceOrder(t *testing.T) {
	t.Parallel()
	tagger := New(output.New(&bytes.Buffer{}, output.Debug, time.UTC, nil), fakeIDs{})
	client := &fakeClient{}
	if got := tagger.Series(context.Background(), client, []int{3, 1, 3, 2}, 7); got != 3 {
		t.Fatalf("tagged=%d", got)
	}
	if !reflect.DeepEqual(client.series, []int{3, 1, 2}) {
		t.Fatalf("series order=%v", client.series)
	}
}

func TestRetroactiveTaggingSortsResolvedResources(t *testing.T) {
	t.Parallel()
	tagger := New(output.New(&bytes.Buffer{}, output.Debug, time.UTC, nil), fakeIDs{values: map[int]struct{}{10: {}, 20: {}}})
	client := &fakeClient{tags: []arr.Tag{{ID: 7, Label: "searched"}}, resolved: map[int]struct{}{5: {}, 2: {}, 9: {}}}
	if err := tagger.RetroEpisodes(context.Background(), client, "Test", "Missing", "searched"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.series, []int{2, 5, 9}) {
		t.Fatalf("series order=%v", client.series)
	}
}

func TestRetroactiveResolutionFailureStopsTagging(t *testing.T) {
	t.Parallel()
	want := errors.New("resolve failed")
	tagger := New(output.New(&bytes.Buffer{}, output.Debug, time.UTC, nil), fakeIDs{values: map[int]struct{}{10: {}}})
	client := &fakeClient{resolveErr: want}
	err := tagger.RetroAlbums(context.Background(), client, "Test", "Missing", "searched")
	if !errors.Is(err, want) || len(client.artists) != 0 {
		t.Fatalf("err=%v artists=%v", err, client.artists)
	}
}
