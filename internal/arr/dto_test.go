package arr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/neurekadev/warrden/internal/config"
)

func TestHTTPDTOsConvertWithoutLeakingTagsIntoDomain(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue":
			_, _ = io.WriteString(w, `{"totalRecords":1,"records":[{"id":4,"title":"release","errorMessage":null,"artist":{"id":7,"artistName":"Muse"},"album":{"id":8,"title":"Absolution"}}]}`)
		case "/api/v1/wanted/missing":
			_, _ = io.WriteString(w, `{"totalRecords":1,"records":[{"id":9,"title":"top","album":{"id":9,"title":"Origin of Symmetry","artistId":7},"artist":{"id":7,"artistName":"Muse"}}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client := testClient(t, server.URL, config.Lidarr, "v1", 0)
	queue, err := client.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Artist == nil || queue[0].Artist.Name == nil || *queue[0].Artist.Name != "Muse" || queue[0].ErrorMessage != nil {
		t.Fatalf("queue conversion = %#v", queue)
	}
	albums, err := client.WantedAlbums(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].Artist == nil || albums[0].Artist.Name == nil || *albums[0].Artist.Name != "Muse" || albums[0].Album == nil || albums[0].Album.ArtistID != 7 {
		t.Fatalf("album conversion = %#v", albums)
	}

	for _, domain := range []any{Indexer{}, Tag{}, QueueItem{}, Episode{}, Movie{}, Album{}} {
		typeOf := reflect.TypeOf(domain)
		for index := 0; index < typeOf.NumField(); index++ {
			if tag := typeOf.Field(index).Tag.Get("json"); tag != "" {
				t.Errorf("domain %s.%s leaks JSON tag %q", typeOf.Name(), typeOf.Field(index).Name, tag)
			}
		}
	}
}
