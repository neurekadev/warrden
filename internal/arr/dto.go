package arr

import "encoding/json"

type indexerDTO struct {
	Name                  *string `json:"name"`
	EnableAutomaticSearch bool    `json:"enableAutomaticSearch"`
}

func (d indexerDTO) domain() Indexer {
	return Indexer(d)
}

type tagDTO struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

func (d tagDTO) domain() Tag { return Tag(d) }

type queueItemDTO struct {
	ID                    int                `json:"id"`
	Title                 *string            `json:"title"`
	TrackedDownloadStatus string             `json:"trackedDownloadStatus"`
	StatusMessages        []statusMessageDTO `json:"statusMessages"`
	ErrorMessage          *string            `json:"errorMessage"`
	Episode               *queueEpisodeDTO   `json:"episode"`
	Movie                 *queueMovieDTO     `json:"movie"`
	Artist                *queueArtistDTO    `json:"artist"`
	Album                 *queueAlbumDTO     `json:"album"`
}

type statusMessageDTO struct {
	Title    *string  `json:"title"`
	Messages []string `json:"messages"`
}

type queueSeriesDTO struct {
	Title *string `json:"title"`
	Year  int     `json:"year"`
}

type queueEpisodeDTO struct {
	ID            int             `json:"id"`
	Title         *string         `json:"title"`
	SeasonNumber  int             `json:"seasonNumber"`
	EpisodeNumber int             `json:"episodeNumber"`
	Series        *queueSeriesDTO `json:"series"`
}

type queueMovieDTO struct {
	ID    int     `json:"id"`
	Title *string `json:"title"`
	Year  int     `json:"year"`
}

type queueArtistDTO struct {
	ID   int     `json:"id"`
	Name *string `json:"artistName"`
}

type queueAlbumDTO struct {
	Title *string `json:"title"`
}

func (d queueItemDTO) domain() QueueItem {
	item := QueueItem{
		ID: d.ID, Title: cloneString(d.Title), TrackedDownloadStatus: d.TrackedDownloadStatus,
		ErrorMessage: cloneString(d.ErrorMessage),
	}
	item.StatusMessages = make([]StatusMessage, len(d.StatusMessages))
	for index, status := range d.StatusMessages {
		item.StatusMessages[index] = StatusMessage{Title: cloneString(status.Title), Messages: append([]string(nil), status.Messages...)}
	}
	if d.Episode != nil {
		item.Episode = &QueueEpisode{
			ID: d.Episode.ID, Title: cloneString(d.Episode.Title), SeasonNumber: d.Episode.SeasonNumber,
			EpisodeNumber: d.Episode.EpisodeNumber,
		}
		if d.Episode.Series != nil {
			item.Episode.Series = &QueueSeries{Title: cloneString(d.Episode.Series.Title), Year: d.Episode.Series.Year}
		}
	}
	if d.Movie != nil {
		item.Movie = &QueueMovie{ID: d.Movie.ID, Title: cloneString(d.Movie.Title), Year: d.Movie.Year}
	}
	if d.Artist != nil {
		item.Artist = &QueueArtist{ID: d.Artist.ID, Name: cloneString(d.Artist.Name)}
	}
	if d.Album != nil {
		item.Album = &QueueAlbum{Title: cloneString(d.Album.Title)}
	}
	return item
}

type episodeDTO struct {
	ID            int               `json:"id"`
	SeriesID      int               `json:"seriesId"`
	Series        *episodeSeriesDTO `json:"series"`
	SeasonNumber  int               `json:"seasonNumber"`
	EpisodeNumber int               `json:"episodeNumber"`
	Title         *string           `json:"title"`
}

type episodeSeriesDTO struct {
	Title *string `json:"title"`
	Year  int     `json:"year"`
}

func (d episodeDTO) domain() Episode {
	episode := Episode{
		ID: d.ID, SeriesID: d.SeriesID, SeasonNumber: d.SeasonNumber,
		EpisodeNumber: d.EpisodeNumber, Title: cloneString(d.Title),
	}
	if d.Series != nil {
		episode.Series = &EpisodeSeries{Title: cloneString(d.Series.Title), Year: d.Series.Year}
	}
	return episode
}

type movieDTO struct {
	ID    int     `json:"id"`
	Title *string `json:"title"`
	Year  int     `json:"year"`
}

func (d movieDTO) domain() Movie { return Movie{ID: d.ID, Title: cloneString(d.Title), Year: d.Year} }

type albumDTO struct {
	ID     int             `json:"id"`
	Album  *albumRecordDTO `json:"album"`
	Artist *albumArtistDTO `json:"artist"`
}

type albumRecordDTO struct {
	Title    *string `json:"title"`
	ArtistID int     `json:"artistId"`
}

type albumArtistDTO struct {
	ID   int     `json:"id"`
	Name *string `json:"artistName"`
}

type episodeParentDTO struct {
	SeriesID int `json:"seriesId"`
}

type albumParentDTO struct {
	ArtistID int `json:"artistId"`
}

func (d albumDTO) domain() Album {
	album := Album{ID: d.ID}
	if d.Album != nil {
		album.Album = &AlbumRecord{Title: cloneString(d.Album.Title), ArtistID: d.Album.ArtistID}
	}
	if d.Artist != nil {
		album.Artist = &AlbumArtist{ID: d.Artist.ID, Name: cloneString(d.Artist.Name)}
	}
	return album
}

type pageDTO[T any] struct {
	Page         int `json:"page"`
	PageSize     int `json:"pageSize"`
	TotalRecords int `json:"totalRecords"`
	Records      []T `json:"records"`
}

type document map[string]json.RawMessage

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
