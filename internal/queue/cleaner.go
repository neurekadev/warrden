// Package queue applies ordered cleanup rules to arr queue warnings.
package queue

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"code.neureka.dev/warrden/warrden/internal/arr"
	"code.neureka.dev/warrden/warrden/internal/config"
	"code.neureka.dev/warrden/warrden/internal/matcher"
	"code.neureka.dev/warrden/warrden/internal/output"
)

type client interface {
	Instance() string
	Queue(context.Context) ([]arr.QueueItem, error)
	DeleteQueue(context.Context, int, bool) error
}

// Cleaner performs one queue cleanup job.
type Cleaner struct {
	client client
	kind   config.Kind
	dryRun bool
	rules  []config.Rule
	output *output.Writer
}

// New creates a queue cleaner.
func New(client client, kind config.Kind, dryRun bool, rules []config.Rule, out *output.Writer) *Cleaner {
	return &Cleaner{client: client, kind: kind, dryRun: dryRun, rules: append([]config.Rule(nil), rules...), output: out}
}

// Run executes one cleanup pass and returns the number of matched items.
func (c *Cleaner) Run(ctx context.Context) (int, error) {
	instance := c.client.Instance()
	contextName := strings.ToLower(instance) + ".queue"
	items, err := c.client.Queue(ctx)
	if err != nil {
		return 0, err
	}
	c.output.Debug(contextName, fmt.Sprintf("Fetched %d queue items", len(items)))

	activeRules := make([]config.Rule, 0, len(c.rules))
	for _, rule := range c.rules {
		if rule.Action != config.None {
			activeRules = append(activeRules, rule)
		}
	}
	if len(activeRules) == 0 {
		c.output.QueueResult(instance, len(items), 0, 0, nil, c.dryRun)
		return 0, nil
	}

	warnings := make([]arr.QueueItem, 0)
	for _, item := range items {
		if strings.EqualFold(item.TrackedDownloadStatus, "warning") || len(item.StatusMessages) > 0 || nonblank(item.ErrorMessage) {
			warnings = append(warnings, item)
		}
	}
	c.output.Debug(contextName, fmt.Sprintf("%d items with warnings/errors out of %d total", len(warnings), len(items)))
	if len(warnings) == 0 {
		c.output.QueueResult(instance, len(items), 0, 0, nil, c.dryRun)
		return 0, nil
	}

	results := make([]output.QueueItem, 0, len(warnings))
	for _, item := range warnings {
		rule, ok := match(item, activeRules, string(c.kind))
		if !ok {
			continue
		}
		blocklist := rule.Action == config.RemoveAndBlocklist
		title := title(item)
		if !c.dryRun {
			if err := c.client.DeleteQueue(ctx, item.ID, blocklist); err != nil {
				c.output.Warn(contextName, fmt.Sprintf("Failed to remove queue item %d — %s", item.ID, title), err.Error())
				continue
			}
		}
		results = append(results, output.QueueItem{Title: title, Rule: rule.Match, Blocklist: blocklist})
	}
	c.output.Debug(contextName, fmt.Sprintf("Matched %d items to cleanup rules", len(results)))
	slices.SortFunc(results, func(a, b output.QueueItem) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	c.output.QueueResult(instance, len(items), len(warnings), len(results), results, c.dryRun)
	return len(results), nil
}

func match(item arr.QueueItem, rules []config.Rule, kind string) (config.Rule, bool) {
	contains := func(value, pattern string) bool {
		return strings.Contains(strings.ToLower(value), strings.ToLower(pattern))
	}
	for _, rule := range rules {
		patterns := matcher.Patterns(rule.Match, kind)
		for _, pattern := range patterns {
			if nonblank(item.ErrorMessage) && contains(*item.ErrorMessage, pattern) {
				return rule, true
			}
			for _, status := range item.StatusMessages {
				if status.Title != nil && contains(*status.Title, pattern) {
					return rule, true
				}
				for _, message := range status.Messages {
					if contains(message, pattern) {
						return rule, true
					}
				}
			}
		}
	}
	return config.Rule{}, false
}

func title(item arr.QueueItem) string {
	if episode := item.Episode; episode != nil {
		title := valueOr(episode.Title, fmt.Sprintf("Episode %d", episode.ID))
		if episode.Series != nil {
			return fmt.Sprintf("%s (%d) - S%02dE%02d - %s", valueOr(episode.Series.Title, ""), episode.Series.Year, episode.SeasonNumber, episode.EpisodeNumber, title)
		}
		return fmt.Sprintf("S%02dE%02d - %s", episode.SeasonNumber, episode.EpisodeNumber, title)
	}
	if movie := item.Movie; movie != nil {
		title := valueOr(movie.Title, fmt.Sprintf("Movie %d", movie.ID))
		if movie.Year > 0 {
			return fmt.Sprintf("%s (%d)", title, movie.Year)
		}
		return title
	}
	if artist := item.Artist; artist != nil {
		name := valueOr(artist.Name, fmt.Sprintf("Artist %d", artist.ID))
		if item.Album != nil && item.Album.Title != nil {
			return name + " - " + *item.Album.Title
		}
		return name
	}
	if item.Title != nil {
		return *item.Title
	}
	return fmt.Sprintf("ID %d", item.ID)
}

func nonblank(value *string) bool { return value != nil && strings.TrimSpace(*value) != "" }

func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
