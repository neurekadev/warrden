// Package config loads and validates wArrden's YAML configuration contract.
package config

import (
	"net/url"
	"strings"
	"time"
)

// Kind identifies a supported arr application.
type Kind string

const (
	Sonarr   Kind = "sonarr"   // Sonarr identifies a Sonarr v3 instance.
	Radarr   Kind = "radarr"   // Radarr identifies a Radarr v3 instance.
	Lidarr   Kind = "lidarr"   // Lidarr identifies a Lidarr v1 instance.
	Whisparr Kind = "whisparr" // Whisparr identifies either supported Whisparr v3 shape.
)

// SearchType identifies the aggregation level used by a search job.
type SearchType string

const (
	Episode SearchType = "episode" // Episode selects individual episode searches.
	Season  SearchType = "season"  // Season groups wanted episodes into season searches.
	Album   SearchType = "album"   // Album selects individual album searches.
	Artist  SearchType = "artist"  // Artist groups wanted albums into artist searches.
)

// Action identifies the operation performed for a matching queue rule.
type Action string

const (
	Remove             Action = "remove"             // Remove deletes a matching queue item.
	RemoveAndBlocklist Action = "removeAndBlocklist" // RemoveAndBlocklist deletes and blocklists a match.
	None               Action = "none"               // None leaves a configured matcher inactive.
)

// Config is the validated, normalized application configuration.
type Config struct {
	LogLevel   string
	Instances  []Instance
	QueueRules QueueRules

	warnings []string
}

// Instance is one validated arr instance.
type Instance struct {
	Kind          Kind
	Enabled       bool
	Name          string
	URL           *url.URL
	URLText       string
	APIVersion    string
	APIKey        string
	IndexerFilter *IndexerFilter
	MissingSearch *Job
	UpgradeSearch *Job
	QueueCleanup  *Job
}

// Key returns the case-insensitive runtime key for an instance.
func (i Instance) Key() string { return strings.ToLower(i.Name) }

// IsWhisparrEros reports whether an instance uses Whisparr's Eros API shape.
func (i Instance) IsWhisparrEros() bool {
	return i.Kind == Whisparr && i.APIVersion == "v3-eros"
}

// IndexerFilter restricts automatic searches to named indexers.
type IndexerFilter struct {
	Enabled bool
	Include []string
	Exclude []string
}

// Job is a validated scheduled job definition.
type Job struct {
	Enabled    bool
	Cron       string
	MaxResults int
	Cooldown   time.Duration
	SearchType SearchType
	Tagging    *Tagging
}

// Tagging controls post-search and retroactive tagging.
type Tagging struct {
	Enabled     bool
	Name        string
	Retroactive bool
}

// Rule is an ordered queue-cleanup matcher and action.
type Rule struct {
	Match  string
	Action Action
}

// QueueRules contains rules by arr kind.
type QueueRules struct {
	Sonarr   []Rule
	Radarr   []Rule
	Lidarr   []Rule
	Whisparr []Rule
}

// For returns the rules configured for kind.
func (q QueueRules) For(kind Kind) []Rule {
	switch kind {
	case Sonarr:
		return q.Sonarr
	case Radarr:
		return q.Radarr
	case Lidarr:
		return q.Lidarr
	case Whisparr:
		return q.Whisparr
	default:
		return nil
	}
}

// Warnings returns a copy of the warnings produced while loading configuration.
func (c *Config) Warnings() []string {
	return append([]string(nil), c.warnings...)
}
