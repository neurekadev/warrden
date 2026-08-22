package config

type rawConfig struct {
	LogLevel          *string        `yaml:"logLevel"`
	Instances         []rawInstance  `yaml:"instances"`
	QueueCleanupRules *rawQueueRules `yaml:"queueCleanupRules"`
}

type rawQueueRules struct {
	Sonarr   []rawRule `yaml:"sonarr"`
	Radarr   []rawRule `yaml:"radarr"`
	Lidarr   []rawRule `yaml:"lidarr"`
	Whisparr []rawRule `yaml:"whisparr"`
}

type rawRule struct {
	Match  string `yaml:"match"`
	Action string `yaml:"action"`
}

type rawInstance struct {
	Type          string     `yaml:"type"`
	Enabled       *bool      `yaml:"enabled"`
	Name          string     `yaml:"name"`
	URL           string     `yaml:"url"`
	APIVersion    *string    `yaml:"apiVersion"`
	APIKey        string     `yaml:"apiKey"`
	IndexerFilter *rawFilter `yaml:"indexerFilter"`
	MissingSearch *rawJob    `yaml:"missingSearch"`
	UpgradeSearch *rawJob    `yaml:"upgradeSearch"`
	QueueCleanup  *rawJob    `yaml:"queueCleanup"`
}

type rawFilter struct {
	Enabled *bool    `yaml:"enabled"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type rawJob struct {
	Enabled    *bool       `yaml:"enabled"`
	Cron       *string     `yaml:"cron"`
	MaxResults *int        `yaml:"maxResults"`
	Cooldown   *string     `yaml:"cooldown"`
	SearchType *string     `yaml:"searchType"`
	Tagging    *rawTagging `yaml:"tagging"`
}

type rawTagging struct {
	Enabled     *bool   `yaml:"enabled"`
	Name        *string `yaml:"name"`
	Retroactive *bool   `yaml:"retroactive"`
}
