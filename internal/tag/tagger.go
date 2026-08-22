// Package tag applies and retroactively restores arr tags.
package tag

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"code.neureka.dev/warrden/warrden/internal/arr"
	"code.neureka.dev/warrden/warrden/internal/output"
)

type client interface {
	Instance() string
	Tags(context.Context) ([]arr.Tag, error)
	CreateTag(context.Context, string) (arr.Tag, error)
	EnsureSeriesTag(context.Context, int, int) (bool, error)
	EnsureMovieTag(context.Context, int, int) (bool, error)
	EnsureArtistTag(context.Context, int, int) (bool, error)
	ResolveSeriesIDs(context.Context, []int) (map[int]struct{}, error)
	ResolveArtistIDs(context.Context, []int) (map[int]struct{}, error)
}

type ids interface {
	IDs(context.Context, string, string) (map[int]struct{}, error)
}

// Tagger applies configured tags to searched resources.
type Tagger struct {
	output   *output.Writer
	cooldown ids
}

// New creates a tagger.
func New(out *output.Writer, cooldown ids) *Tagger { return &Tagger{output: out, cooldown: cooldown} }

// FindOrCreate returns the case-insensitive matching tag ID, creating it when absent.
func (t *Tagger) FindOrCreate(ctx context.Context, client client, name string) (int, error) {
	tags, err := client.Tags(ctx)
	if err != nil {
		return 0, err
	}
	contextName := strings.ToLower(client.Instance()) + ".tagging"
	for _, existing := range tags {
		if strings.EqualFold(existing.Label, name) {
			t.output.Debug(contextName, fmt.Sprintf("Found tag '%s' (ID: %d)", name, existing.ID))
			return existing.ID, nil
		}
	}
	t.output.Debug(contextName, fmt.Sprintf("Creating tag '%s'", name))
	created, err := client.CreateTag(ctx, name)
	if err != nil {
		return 0, err
	}
	t.output.Debug(contextName, fmt.Sprintf("Created tag '%s' (ID: %d)", name, created.ID))
	return created.ID, nil
}

// Series tags unique series IDs and returns the number newly tagged.
func (t *Tagger) Series(ctx context.Context, client client, ids []int, tagID int) int {
	return t.resources(ctx, client, "series", unique(ids), tagID)
}

// Movies tags unique movie IDs and returns the number newly tagged.
func (t *Tagger) Movies(ctx context.Context, client client, ids []int, tagID int) int {
	return t.resources(ctx, client, "movie", unique(ids), tagID)
}

// Artists tags unique artist IDs and returns the number newly tagged.
func (t *Tagger) Artists(ctx context.Context, client client, ids []int, tagID int) int {
	return t.resources(ctx, client, "artist", unique(ids), tagID)
}

func (t *Tagger) resources(ctx context.Context, client client, kind string, ids []int, tagID int) int {
	tagged := 0
	for _, id := range ids {
		var applied bool
		var err error
		switch kind {
		case "series":
			applied, err = client.EnsureSeriesTag(ctx, id, tagID)
		case "movie":
			applied, err = client.EnsureMovieTag(ctx, id, tagID)
		case "artist":
			applied, err = client.EnsureArtistTag(ctx, id, tagID)
		}
		if err != nil {
			t.output.Warn(strings.ToLower(client.Instance())+".tagging", fmt.Sprintf("Failed to tag %s %d", kind, id), arr.Detail(err))
			continue
		}
		if applied {
			tagged++
		}
	}
	return tagged
}

// RetroEpisodes resolves stored episode cooldowns and tags their series.
func (t *Tagger) RetroEpisodes(ctx context.Context, client client, instance, category, name string) error {
	ids, err := t.cooldown.IDs(ctx, instance, category)
	if err != nil {
		return err
	}
	contextName := strings.ToLower(instance) + ".retrotag"
	if len(ids) == 0 {
		t.output.Debug(contextName, "No cooldown entries for "+category)
		return nil
	}
	values := keys(ids)
	t.output.Debug(contextName, fmt.Sprintf("Resolving %d episode IDs to series", len(values)))
	series, err := client.ResolveSeriesIDs(ctx, values)
	if err != nil {
		return err
	}
	t.output.Debug(contextName, fmt.Sprintf("Resolved %d unique series", len(series)))
	tagID, err := t.FindOrCreate(ctx, client, name)
	if err != nil {
		return err
	}
	tagged := t.resources(ctx, client, "series", keys(series), tagID)
	t.output.Debug(contextName, fmt.Sprintf("Tagged %d series, %d already had tag", tagged, len(series)-tagged))
	return nil
}

// RetroSeasons tags the series encoded in stored season cooldown keys.
func (t *Tagger) RetroSeasons(ctx context.Context, client client, instance, category, name string) error {
	ids, err := t.cooldown.IDs(ctx, instance, category)
	if err != nil {
		return err
	}
	contextName := strings.ToLower(instance) + ".retrotag"
	if len(ids) == 0 {
		t.output.Debug(contextName, "No cooldown entries for "+category)
		return nil
	}
	series := make(map[int]struct{})
	for id := range ids {
		series[id/1000] = struct{}{}
	}
	t.output.Debug(contextName, fmt.Sprintf("Extracted %d series from %d season entries", len(series), len(ids)))
	tagID, err := t.FindOrCreate(ctx, client, name)
	if err != nil {
		return err
	}
	tagged := t.resources(ctx, client, "series", keys(series), tagID)
	t.output.Debug(contextName, fmt.Sprintf("Tagged %d series, %d already had tag", tagged, len(series)-tagged))
	return nil
}

// RetroMovies tags stored movie cooldown IDs.
func (t *Tagger) RetroMovies(ctx context.Context, client client, instance, category, name string) error {
	return t.retroDirect(ctx, client, instance, category, name, "movie")
}

// RetroAlbums resolves stored album cooldowns and tags their artists.
func (t *Tagger) RetroAlbums(ctx context.Context, client client, instance, category, name string) error {
	ids, err := t.cooldown.IDs(ctx, instance, category)
	if err != nil {
		return err
	}
	contextName := strings.ToLower(instance) + ".retrotag"
	if len(ids) == 0 {
		t.output.Debug(contextName, "No cooldown entries for "+category)
		return nil
	}
	values := keys(ids)
	t.output.Debug(contextName, fmt.Sprintf("Resolving %d album IDs to artists", len(values)))
	artists, err := client.ResolveArtistIDs(ctx, values)
	if err != nil {
		return err
	}
	t.output.Debug(contextName, fmt.Sprintf("Resolved %d unique artists", len(artists)))
	tagID, err := t.FindOrCreate(ctx, client, name)
	if err != nil {
		return err
	}
	tagged := t.resources(ctx, client, "artist", keys(artists), tagID)
	t.output.Debug(contextName, fmt.Sprintf("Tagged %d artists, %d already had tag", tagged, len(artists)-tagged))
	return nil
}

// RetroArtists tags stored artist cooldown IDs.
func (t *Tagger) RetroArtists(ctx context.Context, client client, instance, category, name string) error {
	return t.retroDirect(ctx, client, instance, category, name, "artist")
}

func (t *Tagger) retroDirect(ctx context.Context, client client, instance, category, name, kind string) error {
	ids, err := t.cooldown.IDs(ctx, instance, category)
	if err != nil {
		return err
	}
	contextName := strings.ToLower(instance) + ".retrotag"
	if len(ids) == 0 {
		t.output.Debug(contextName, "No cooldown entries for "+category)
		return nil
	}
	t.output.Debug(contextName, fmt.Sprintf("Found %d cooldown entries (%d unique %ss)", len(ids), len(ids), kind))
	tagID, err := t.FindOrCreate(ctx, client, name)
	if err != nil {
		return err
	}
	tagged := t.resources(ctx, client, kind, keys(ids), tagID)
	t.output.Debug(contextName, fmt.Sprintf("Tagged %d %ss, %d already had tag", tagged, kind, len(ids)-tagged))
	return nil
}

func unique(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
func keys(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
