package app

import (
	"context"
	"fmt"
	"strings"

	"code.neureka.dev/warrden/warrden/internal/arr"
	"code.neureka.dev/warrden/warrden/internal/config"
	"code.neureka.dev/warrden/warrden/internal/health"
	"code.neureka.dev/warrden/warrden/internal/output"
	"code.neureka.dev/warrden/warrden/internal/tag"
)

func runRetroactive(ctx context.Context, cfg *config.Config, clients map[string]*arr.Client, tracker *health.Tracker, tagger *tag.Tagger, out *output.Writer) {
	for _, instance := range cfg.Instances {
		if !instance.Enabled || !tracker.Enabled(instance.Key()) {
			continue
		}
		client := clients[instance.Key()]
		jobs := []struct {
			job           *config.Job
			key, category string
		}{{instance.MissingSearch, "missing", "Missing"}, {instance.UpgradeSearch, "upgrade", "Upgrade"}}
		for _, entry := range jobs {
			if entry.job == nil || entry.job.Tagging == nil || !entry.job.Tagging.Enabled || !entry.job.Tagging.Retroactive || strings.TrimSpace(entry.job.Tagging.Name) == "" {
				continue
			}
			name := entry.job.Tagging.Name
			contextName := instance.Key() + ".retrotag_" + entry.key
			out.Debug(contextName, fmt.Sprintf("Retroactive tagging started for '%s'", name))
			var err error
			switch {
			case instance.Kind == config.Sonarr || (instance.Kind == config.Whisparr && !instance.IsWhisparrEros()):
				err = tagger.RetroEpisodes(ctx, client, instance.Name, entry.category, name)
				if err == nil {
					err = tagger.RetroSeasons(ctx, client, instance.Name, entry.category+"_Season", name)
				}
			case instance.Kind == config.Radarr || instance.IsWhisparrEros():
				err = tagger.RetroMovies(ctx, client, instance.Name, entry.category, name)
			case instance.Kind == config.Lidarr:
				err = tagger.RetroAlbums(ctx, client, instance.Name, entry.category, name)
				if err == nil {
					err = tagger.RetroArtists(ctx, client, instance.Name, entry.category+"_Artist", name)
				}
			}
			if err != nil {
				out.Warn(contextName, fmt.Sprintf("Retroactive tagging failed for '%s'", name), arr.Detail(err))
			}
		}
	}
}
