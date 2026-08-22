// Package arr implements the supported arr HTTP contracts and domain values.
package arr

import (
	"context"
	"errors"
	"fmt"
)

// Indexer describes an arr indexer's automatic-search state.
type Indexer struct {
	Name                  *string
	EnableAutomaticSearch bool
}

// Tag is an arr tag.
type Tag struct {
	ID    int
	Label string
}

// QueueItem is an item returned by an arr queue endpoint.
type QueueItem struct {
	ID                    int
	Title                 *string
	TrackedDownloadStatus string
	StatusMessages        []StatusMessage
	ErrorMessage          *string
	Episode               *QueueEpisode
	Movie                 *QueueMovie
	Artist                *QueueArtist
	Album                 *QueueAlbum
}

// StatusMessage contains queue warning details.
type StatusMessage struct {
	Title    *string
	Messages []string
}

// QueueSeries identifies an episode's series.
type QueueSeries struct {
	Title *string
	Year  int
}

// QueueEpisode describes an episode nested in a queue item.
type QueueEpisode struct {
	ID            int
	Title         *string
	SeasonNumber  int
	EpisodeNumber int
	Series        *QueueSeries
}

// QueueMovie describes a movie nested in a queue item.
type QueueMovie struct {
	ID    int
	Title *string
	Year  int
}

// QueueArtist describes an artist nested in a queue item.
type QueueArtist struct {
	ID   int
	Name *string
}

// QueueAlbum describes an album nested in a queue item.
type QueueAlbum struct {
	Title *string
}

// Episode is a wanted episode.
type Episode struct {
	ID            int
	SeriesID      int
	Series        *EpisodeSeries
	SeasonNumber  int
	EpisodeNumber int
	Title         *string
}

// EpisodeSeries identifies a wanted episode's series.
type EpisodeSeries struct {
	Title *string
	Year  int
}

// Movie is a wanted movie.
type Movie struct {
	ID    int
	Title *string
	Year  int
}

// Album is a wanted album.
type Album struct {
	ID     int
	Album  *AlbumRecord
	Artist *AlbumArtist
}

// AlbumRecord describes the nested Lidarr album.
type AlbumRecord struct {
	Title    *string
	ArtistID int
}

// AlbumArtist identifies a Lidarr artist.
type AlbumArtist struct {
	ID   int
	Name *string
}

// HTTPError reports a non-success arr response.
type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Response status code does not indicate success: %d (%s).", e.StatusCode, e.Status)
}

// LegacyError returns the externally compatible .NET-style error detail.
func (e *HTTPError) LegacyError() string { return "HttpRequestException: " + e.Error() }

// TransportError wraps an HTTP transport or per-attempt timeout failure.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// LegacyError returns the external .NET-style exception label.
func (e *TransportError) LegacyError() string {
	if errors.Is(e.Err, context.DeadlineExceeded) {
		return "TimeoutRejectedException: " + e.Err.Error()
	}
	return "HttpRequestException: " + e.Err.Error()
}
