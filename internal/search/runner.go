// Package search runs missing and upgrade searches across supported arr applications.
package search

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/neurekadev/warrden/internal/arr"
	"github.com/neurekadev/warrden/internal/config"
	"github.com/neurekadev/warrden/internal/output"
	"github.com/neurekadev/warrden/internal/tag"
)

type cooldowns interface {
	CleanExpired(context.Context, string, string, time.Duration) error
	IDs(context.Context, string, string) (map[int]struct{}, error)
	Mark(context.Context, string, string, []int) error
}

// Runner executes one configured search job.
type Runner struct {
	cooldown cooldowns
	output   *output.Writer
	tagger   *tag.Tagger
	intN     func(int) int
}

// New creates a search runner.
func New(cooldown cooldowns, out *output.Writer, tagger *tag.Tagger) *Runner {
	return &Runner{cooldown: cooldown, output: out, tagger: tagger, intN: rand.IntN}
}

// Run executes one missing or upgrade search.
func (r *Runner) Run(ctx context.Context, client *arr.Client, instance config.Instance, job *config.Job, missing, dryRun bool) error {
	category, jobName := "Upgrade", "Upgrade Search"
	if missing {
		category, jobName = "Missing", "Missing Search"
	}
	progress := r.output.Search(client.Instance(), jobName, job.MaxResults)
	progress.Header()

	switch {
	case instance.Kind == config.Sonarr || (instance.Kind == config.Whisparr && !instance.IsWhisparrEros()):
		if job.SearchType == config.Season {
			return r.seasons(ctx, client, category, job, instance.IndexerFilter, dryRun, progress)
		}
		return r.episodes(ctx, client, category, job, instance.IndexerFilter, dryRun, progress)
	case instance.Kind == config.Radarr || instance.IsWhisparrEros():
		return r.movies(ctx, client, category, job, instance.IndexerFilter, dryRun, progress)
	case instance.Kind == config.Lidarr:
		if job.SearchType == config.Artist {
			return r.artists(ctx, client, category, job, instance.IndexerFilter, dryRun, progress)
		}
		return r.albums(ctx, client, category, job, instance.IndexerFilter, dryRun, progress)
	default:
		return fmt.Errorf("no search handler for kind=%q type=%q", category, instance.Kind)
	}
}

func (r *Runner) episodes(ctx context.Context, client *arr.Client, category string, job *config.Job, filterConfig *config.IndexerFilter, dryRun bool, progress *output.Search) error {
	contextName := strings.ToLower(client.Instance()) + "." + strings.ToLower(category)
	progress.Phase("Cleaning cooldown entries")
	if err := r.cooldown.CleanExpired(ctx, client.Instance(), category, job.Cooldown); err != nil {
		return err
	}
	progress.Phase("Fetching wanted episodes")
	wanted, err := client.WantedEpisodes(ctx, category == "Upgrade")
	if err != nil {
		return err
	}
	r.output.Debug(contextName, fmt.Sprintf("Fetched %d wanted episodes", len(wanted)))
	if len(wanted) == 0 {
		progress.Stats(0, 0, 0, 0, true, "")
		return nil
	}

	progress.Phase("Applying cooldown filters")
	ids, err := r.cooldown.IDs(ctx, client.Instance(), category)
	if err != nil {
		return err
	}
	eligible := filter(wanted, func(item arr.Episode) int { return item.ID }, ids)
	choose(eligible, job.MaxResults, r.intN)
	selected := take(eligible, job.MaxResults)
	slices.SortStableFunc(selected, compareEpisodes)
	onCooldown := len(wanted) - len(eligible)
	r.output.Debug(contextName, fmt.Sprintf("Cooldown filter: %d on cooldown, %d eligible, %d selected", onCooldown, len(eligible), len(selected)))
	if len(selected) == 0 || dryRun {
		progress.Stats(len(wanted), onCooldown, len(eligible), chooseCount(dryRun, len(selected)), true, "")
		return nil
	}
	progress.Phase("Checking indexer availability")
	if ok, detail, err := checkIndexers(ctx, client, filterConfig); err != nil {
		return err
	} else if !ok {
		r.output.Warn(contextName, "No enabled indexers — search skipped", detail)
		progress.Stats(len(wanted), onCooldown, len(eligible), 0, true, "No enabled indexers available")
		return nil
	}

	progress.Phase(fmt.Sprintf("Searching %d items", len(selected)))
	progress.Stats(len(wanted), onCooldown, len(eligible), len(selected), false, "")
	progress.Results()
	selectedIDs, seriesIDs := make([]int, 0, len(selected)), make([]int, 0, len(selected))
	for _, episode := range selected {
		progress.Item(episodeTitle(episode))
		selectedIDs = append(selectedIDs, episode.ID)
		seriesIDs = append(seriesIDs, episode.SeriesID)
	}
	if err := client.SearchEpisodes(ctx, selectedIDs); err != nil {
		titles := make([]string, len(selected))
		for index, episode := range selected {
			titles[index] = episodeTitle(episode)
		}
		r.output.Warn(contextName, "Search trigger failed for "+strings.Join(titles, ", "), err.Error())
		progress.Trailer()
		return nil
	}
	if err := r.cooldown.Mark(ctx, client.Instance(), category, selectedIDs); err != nil {
		return err
	}
	r.applyTag(ctx, client, job.Tagging, func(tagID int) { r.tagger.Series(ctx, client, seriesIDs, tagID) })
	progress.Trailer()
	return nil
}

type seasonGroup struct {
	seriesID, number, key int
	series                *arr.EpisodeSeries
}

func (r *Runner) seasons(ctx context.Context, client *arr.Client, category string, job *config.Job, filterConfig *config.IndexerFilter, dryRun bool, progress *output.Search) error {
	contextName := strings.ToLower(client.Instance()) + "." + strings.ToLower(category)
	cooldownCategory := category + "_Season"
	progress.Phase("Cleaning cooldown entries")
	if err := r.cooldown.CleanExpired(ctx, client.Instance(), cooldownCategory, job.Cooldown); err != nil {
		return err
	}
	progress.Phase("Fetching wanted episodes")
	wanted, err := client.WantedEpisodes(ctx, category == "Upgrade")
	if err != nil {
		return err
	}
	if len(wanted) == 0 {
		progress.Stats(0, 0, 0, 0, true, "")
		return nil
	}
	progress.Phase("Grouping by season")
	groups, indexes := make([]seasonGroup, 0), make(map[[2]int]int)
	for _, episode := range wanted {
		identity := [2]int{episode.SeriesID, episode.SeasonNumber}
		if _, exists := indexes[identity]; exists {
			continue
		}
		indexes[identity] = len(groups)
		groups = append(groups, seasonGroup{seriesID: episode.SeriesID, number: episode.SeasonNumber, key: episode.SeriesID*1000 + episode.SeasonNumber, series: episode.Series})
	}
	r.output.Debug(contextName, fmt.Sprintf("Grouped %d episodes into %d seasons", len(wanted), len(groups)))
	progress.Phase("Applying cooldown filters")
	ids, err := r.cooldown.IDs(ctx, client.Instance(), cooldownCategory)
	if err != nil {
		return err
	}
	eligible := filter(groups, func(item seasonGroup) int { return item.key }, ids)
	choose(eligible, job.MaxResults, r.intN)
	selected := take(eligible, job.MaxResults)
	slices.SortStableFunc(selected, func(a, b seasonGroup) int {
		if result := strings.Compare(seriesTitle(a.series), seriesTitle(b.series)); result != 0 {
			return result
		}
		return a.number - b.number
	})
	onCooldown := len(groups) - len(eligible)
	r.output.Debug(contextName, fmt.Sprintf("Season cooldown filter: %d on cooldown, %d eligible, %d selected", onCooldown, len(eligible), len(selected)))
	if len(selected) == 0 || dryRun {
		progress.Stats(len(groups), onCooldown, len(eligible), chooseCount(dryRun, len(selected)), true, "")
		return nil
	}
	progress.Phase("Checking indexer availability")
	if ok, detail, err := checkIndexers(ctx, client, filterConfig); err != nil {
		return err
	} else if !ok {
		r.output.Warn(contextName, "No enabled indexers — search skipped", detail)
		progress.Stats(len(groups), onCooldown, len(eligible), 0, true, "No enabled indexers available")
		return nil
	}
	progress.Phase(fmt.Sprintf("Searching %d seasons", len(selected)))
	progress.Stats(len(groups), onCooldown, len(eligible), len(selected), false, "")
	progress.Results()
	searchedKeys, seriesIDs := make([]int, 0, len(selected)), make([]int, 0, len(selected))
	for _, group := range selected {
		title := seasonTitle(group)
		progress.Item(title)
		if err := client.SearchSeason(ctx, group.seriesID, group.number); err != nil {
			r.output.Warn(contextName, "Search trigger failed for "+title, err.Error())
			continue
		}
		searchedKeys = append(searchedKeys, group.key)
		seriesIDs = append(seriesIDs, group.seriesID)
	}
	if len(searchedKeys) > 0 {
		if err := r.cooldown.Mark(ctx, client.Instance(), cooldownCategory, searchedKeys); err != nil {
			return err
		}
		r.applyTag(ctx, client, job.Tagging, func(tagID int) { r.tagger.Series(ctx, client, seriesIDs, tagID) })
	}
	progress.Trailer()
	return nil
}

func (r *Runner) movies(ctx context.Context, client *arr.Client, category string, job *config.Job, filterConfig *config.IndexerFilter, dryRun bool, progress *output.Search) error {
	contextName := strings.ToLower(client.Instance()) + "." + strings.ToLower(category)
	progress.Phase("Cleaning cooldown entries")
	if err := r.cooldown.CleanExpired(ctx, client.Instance(), category, job.Cooldown); err != nil {
		return err
	}
	progress.Phase("Fetching wanted movies")
	wanted, err := client.WantedMovies(ctx, category == "Upgrade")
	if err != nil {
		return err
	}
	r.output.Debug(contextName, fmt.Sprintf("Fetched %d wanted movies", len(wanted)))
	if len(wanted) == 0 {
		progress.Stats(0, 0, 0, 0, true, "")
		return nil
	}
	progress.Phase("Applying cooldown filters")
	ids, err := r.cooldown.IDs(ctx, client.Instance(), category)
	if err != nil {
		return err
	}
	eligible := filter(wanted, func(item arr.Movie) int { return item.ID }, ids)
	choose(eligible, job.MaxResults, r.intN)
	selected := take(eligible, job.MaxResults)
	slices.SortStableFunc(selected, func(a, b arr.Movie) int { return strings.Compare(nullable(a.Title, ""), nullable(b.Title, "")) })
	onCooldown := len(wanted) - len(eligible)
	r.output.Debug(contextName, fmt.Sprintf("Cooldown filter: %d on cooldown, %d eligible, %d selected", onCooldown, len(eligible), len(selected)))
	if len(selected) == 0 || dryRun {
		progress.Stats(len(wanted), onCooldown, len(eligible), chooseCount(dryRun, len(selected)), true, "")
		return nil
	}
	progress.Phase("Checking indexer availability")
	if ok, detail, err := checkIndexers(ctx, client, filterConfig); err != nil {
		return err
	} else if !ok {
		r.output.Warn(contextName, "No enabled indexers — search skipped", detail)
		progress.Stats(len(wanted), onCooldown, len(eligible), 0, true, "No enabled indexers available")
		return nil
	}
	progress.Phase(fmt.Sprintf("Searching %d items", len(selected)))
	progress.Stats(len(wanted), onCooldown, len(eligible), len(selected), false, "")
	progress.Results()
	selectedIDs := make([]int, 0, len(selected))
	titles := make([]string, 0, len(selected))
	for _, movie := range selected {
		title := movieTitle(movie)
		progress.Item(title)
		titles = append(titles, title)
		selectedIDs = append(selectedIDs, movie.ID)
	}
	if err := client.SearchMovies(ctx, selectedIDs); err != nil {
		r.output.Warn(contextName, "Search trigger failed for "+strings.Join(titles, ", "), err.Error())
		progress.Trailer()
		return nil
	}
	if err := r.cooldown.Mark(ctx, client.Instance(), category, selectedIDs); err != nil {
		return err
	}
	r.applyTag(ctx, client, job.Tagging, func(tagID int) { r.tagger.Movies(ctx, client, selectedIDs, tagID) })
	progress.Trailer()
	return nil
}

func (r *Runner) albums(ctx context.Context, client *arr.Client, category string, job *config.Job, filterConfig *config.IndexerFilter, dryRun bool, progress *output.Search) error {
	contextName := strings.ToLower(client.Instance()) + "." + strings.ToLower(category)
	progress.Phase("Cleaning cooldown entries")
	if err := r.cooldown.CleanExpired(ctx, client.Instance(), category, job.Cooldown); err != nil {
		return err
	}
	progress.Phase("Fetching wanted albums")
	wanted, err := client.WantedAlbums(ctx, category == "Upgrade")
	if err != nil {
		return err
	}
	r.output.Debug(contextName, fmt.Sprintf("Fetched %d wanted albums", len(wanted)))
	if len(wanted) == 0 {
		progress.Stats(0, 0, 0, 0, true, "")
		return nil
	}
	progress.Phase("Applying cooldown filters")
	ids, err := r.cooldown.IDs(ctx, client.Instance(), category)
	if err != nil {
		return err
	}
	eligible := filter(wanted, func(item arr.Album) int { return item.ID }, ids)
	choose(eligible, job.MaxResults, r.intN)
	selected := take(eligible, job.MaxResults)
	slices.SortStableFunc(selected, compareAlbums)
	onCooldown := len(wanted) - len(eligible)
	r.output.Debug(contextName, fmt.Sprintf("Cooldown filter: %d on cooldown, %d eligible, %d selected", onCooldown, len(eligible), len(selected)))
	if len(selected) == 0 || dryRun {
		progress.Stats(len(wanted), onCooldown, len(eligible), chooseCount(dryRun, len(selected)), true, "")
		return nil
	}
	progress.Phase("Checking indexer availability")
	if ok, detail, err := checkIndexers(ctx, client, filterConfig); err != nil {
		return err
	} else if !ok {
		r.output.Warn(contextName, "No enabled indexers — search skipped", detail)
		progress.Stats(len(wanted), onCooldown, len(eligible), 0, true, "No enabled indexers available")
		return nil
	}
	progress.Phase(fmt.Sprintf("Searching %d items", len(selected)))
	progress.Stats(len(wanted), onCooldown, len(eligible), len(selected), false, "")
	progress.Results()
	selectedIDs, artistIDs, titles := make([]int, 0, len(selected)), make([]int, 0, len(selected)), make([]string, 0, len(selected))
	for _, album := range selected {
		title := albumTitle(album)
		progress.Item(title)
		titles = append(titles, title)
		selectedIDs = append(selectedIDs, album.ID)
		if id := albumArtistID(album); id != 0 {
			artistIDs = append(artistIDs, id)
		}
	}
	if err := client.SearchAlbums(ctx, selectedIDs); err != nil {
		r.output.Warn(contextName, "Search trigger failed for "+strings.Join(titles, ", "), err.Error())
		progress.Trailer()
		return nil
	}
	if err := r.cooldown.Mark(ctx, client.Instance(), category, selectedIDs); err != nil {
		return err
	}
	r.applyTag(ctx, client, job.Tagging, func(tagID int) { r.tagger.Artists(ctx, client, artistIDs, tagID) })
	progress.Trailer()
	return nil
}

type artistGroup struct {
	id     int
	artist *arr.AlbumArtist
}

func (r *Runner) artists(ctx context.Context, client *arr.Client, category string, job *config.Job, filterConfig *config.IndexerFilter, dryRun bool, progress *output.Search) error {
	contextName := strings.ToLower(client.Instance()) + "." + strings.ToLower(category)
	cooldownCategory := category + "_Artist"
	progress.Phase("Cleaning cooldown entries")
	if err := r.cooldown.CleanExpired(ctx, client.Instance(), cooldownCategory, job.Cooldown); err != nil {
		return err
	}
	progress.Phase("Fetching wanted albums")
	wanted, err := client.WantedAlbums(ctx, category == "Upgrade")
	if err != nil {
		return err
	}
	if len(wanted) == 0 {
		progress.Stats(0, 0, 0, 0, true, "")
		return nil
	}
	progress.Phase("Grouping by artist")
	groups, indexes := make([]artistGroup, 0), make(map[int]struct{})
	for _, album := range wanted {
		id := albumArtistID(album)
		if id == 0 {
			continue
		}
		if _, exists := indexes[id]; exists {
			continue
		}
		indexes[id] = struct{}{}
		groups = append(groups, artistGroup{id: id, artist: album.Artist})
	}
	r.output.Debug(contextName, fmt.Sprintf("Grouped %d albums into %d artists", len(wanted), len(groups)))
	progress.Phase("Applying cooldown filters")
	ids, err := r.cooldown.IDs(ctx, client.Instance(), cooldownCategory)
	if err != nil {
		return err
	}
	eligible := filter(groups, func(item artistGroup) int { return item.id }, ids)
	choose(eligible, job.MaxResults, r.intN)
	selected := take(eligible, job.MaxResults)
	slices.SortStableFunc(selected, func(a, b artistGroup) int { return strings.Compare(artistName(a), artistName(b)) })
	onCooldown := len(groups) - len(eligible)
	r.output.Debug(contextName, fmt.Sprintf("Artist cooldown filter: %d on cooldown, %d eligible, %d selected", onCooldown, len(eligible), len(selected)))
	if len(selected) == 0 || dryRun {
		progress.Stats(len(groups), onCooldown, len(eligible), chooseCount(dryRun, len(selected)), true, "")
		return nil
	}
	progress.Phase("Checking indexer availability")
	if ok, detail, err := checkIndexers(ctx, client, filterConfig); err != nil {
		return err
	} else if !ok {
		r.output.Warn(contextName, "No enabled indexers — search skipped", detail)
		progress.Stats(len(groups), onCooldown, len(eligible), 0, true, "No enabled indexers available")
		return nil
	}
	progress.Phase(fmt.Sprintf("Searching %d artists", len(selected)))
	progress.Stats(len(groups), onCooldown, len(eligible), len(selected), false, "")
	progress.Results()
	searched := make([]int, 0, len(selected))
	for _, artist := range selected {
		title := artistName(artist)
		progress.Item(title)
		if err := client.SearchArtist(ctx, artist.id); err != nil {
			r.output.Warn(contextName, "Search trigger failed for "+title, err.Error())
			continue
		}
		searched = append(searched, artist.id)
	}
	if len(searched) > 0 {
		if err := r.cooldown.Mark(ctx, client.Instance(), cooldownCategory, searched); err != nil {
			return err
		}
		r.applyTag(ctx, client, job.Tagging, func(tagID int) { r.tagger.Artists(ctx, client, searched, tagID) })
	}
	progress.Trailer()
	return nil
}

func checkIndexers(ctx context.Context, client *arr.Client, filterConfig *config.IndexerFilter) (bool, string, error) {
	indexers, err := client.Indexers(ctx)
	if err != nil {
		return false, "", err
	}
	if filterConfig == nil || !filterConfig.Enabled {
		for _, indexer := range indexers {
			if indexer.EnableAutomaticSearch {
				return true, "", nil
			}
		}
		return false, "No automatic-search indexers found", nil
	}
	enabled := make([]arr.Indexer, 0)
	for _, indexer := range indexers {
		if indexer.EnableAutomaticSearch && indexer.Name != nil {
			enabled = append(enabled, indexer)
		}
	}
	if len(enabled) == 0 {
		return false, "No automatic-search indexers found", nil
	}
	include, exclude := stringSet(filterConfig.Include), stringSet(filterConfig.Exclude)
	remaining := 0
	for _, indexer := range enabled {
		name := strings.ToLower(*indexer.Name)
		if len(include) > 0 {
			if _, ok := include[name]; !ok {
				continue
			}
		}
		if _, blocked := exclude[name]; blocked {
			continue
		}
		remaining++
	}
	if remaining > 0 {
		return true, "", nil
	}
	available := make([]string, len(enabled))
	for index, indexer := range enabled {
		available[index] = *indexer.Name
	}
	prefix := "Available: " + strings.Join(available, ", ") + ". "
	switch {
	case len(include) > 0 && len(exclude) > 0:
		return false, prefix + "Include: " + strings.Join(filterConfig.Include, ", ") + ". Exclude: " + strings.Join(filterConfig.Exclude, ", ") + ".", nil
	case len(include) > 0:
		return false, prefix + "Include filter: " + strings.Join(filterConfig.Include, ", ") + ".", nil
	case len(exclude) > 0:
		return false, prefix + "Exclude filter: " + strings.Join(filterConfig.Exclude, ", ") + ".", nil
	default:
		return false, "No automatic-search indexers found", nil
	}
}

func (r *Runner) applyTag(ctx context.Context, client *arr.Client, tagging *config.Tagging, action func(int)) {
	if tagging == nil || !tagging.Enabled || strings.TrimSpace(tagging.Name) == "" {
		return
	}
	tagID, err := r.tagger.FindOrCreate(ctx, client, tagging.Name)
	if err != nil {
		r.output.Warn(strings.ToLower(client.Instance())+".tagging", fmt.Sprintf("Tagging failed for '%s'", tagging.Name), arr.Detail(err))
		return
	}
	action(tagID)
}

func choose[T any](items []T, count int, intN func(int) int) {
	limit := min(count, len(items))
	for index := 0; index < limit; index++ {
		other := index + intN(len(items)-index)
		items[index], items[other] = items[other], items[index]
	}
}
func filter[T any](items []T, id func(T) int, blocked map[int]struct{}) []T {
	result := make([]T, 0, len(items))
	for _, item := range items {
		if _, exists := blocked[id(item)]; !exists {
			result = append(result, item)
		}
	}
	return result
}
func take[T any](items []T, count int) []T {
	return append([]T(nil), items[:min(count, len(items))]...)
}
func chooseCount(dry bool, count int) int {
	if dry {
		return 0
	}
	return count
}
func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = struct{}{}
	}
	return result
}
func compareEpisodes(a, b arr.Episode) int {
	if result := strings.Compare(seriesTitle(a.Series), seriesTitle(b.Series)); result != 0 {
		return result
	}
	if a.SeasonNumber != b.SeasonNumber {
		return a.SeasonNumber - b.SeasonNumber
	}
	return a.EpisodeNumber - b.EpisodeNumber
}
func compareAlbums(a, b arr.Album) int {
	if result := strings.Compare(albumArtistName(a), albumArtistName(b)); result != 0 {
		return result
	}
	return strings.Compare(albumRecordTitle(a), albumRecordTitle(b))
}
func seriesTitle(series *arr.EpisodeSeries) string {
	if series == nil {
		return ""
	}
	return nullable(series.Title, "")
}
func episodeTitle(episode arr.Episode) string {
	title := nullable(episode.Title, fmt.Sprintf("Episode %d", episode.ID))
	if episode.Series != nil {
		return fmt.Sprintf("%s (%d) - S%02dE%02d - %s", nullable(episode.Series.Title, ""), episode.Series.Year, episode.SeasonNumber, episode.EpisodeNumber, title)
	}
	return title
}
func seasonTitle(group seasonGroup) string {
	name := fmt.Sprintf("Series %d", group.seriesID)
	if group.series != nil {
		name = nullable(group.series.Title, name)
	}
	if group.series != nil && group.series.Year > 0 {
		return fmt.Sprintf("%s (%d) - Season %d", name, group.series.Year, group.number)
	}
	return fmt.Sprintf("%s - Season %d", name, group.number)
}
func movieTitle(movie arr.Movie) string {
	title := nullable(movie.Title, fmt.Sprintf("Movie %d", movie.ID))
	if movie.Year > 0 {
		return fmt.Sprintf("%s (%d)", title, movie.Year)
	}
	return title
}
func albumTitle(album arr.Album) string {
	return fmt.Sprintf("%s - %s", albumArtistName(album), albumRecordTitle(album))
}
func albumArtistName(album arr.Album) string {
	if album.Artist != nil {
		return nullable(album.Artist.Name, "Artist Unknown")
	}
	return "Artist Unknown"
}
func albumRecordTitle(album arr.Album) string {
	if album.Album != nil {
		return nullable(album.Album.Title, fmt.Sprintf("Album %d", album.ID))
	}
	return fmt.Sprintf("Album %d", album.ID)
}
func albumArtistID(album arr.Album) int {
	if album.Album != nil {
		return album.Album.ArtistID
	}
	if album.Artist != nil {
		return album.Artist.ID
	}
	return 0
}
func artistName(group artistGroup) string {
	if group.artist != nil {
		return nullable(group.artist.Name, fmt.Sprintf("Artist %d", group.id))
	}
	return fmt.Sprintf("Artist %d", group.id)
}

func nullable(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
