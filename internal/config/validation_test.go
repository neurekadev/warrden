package config

import (
	"errors"
	"strings"
	"testing"
)

const validSonarrConfig = `instances:
  - type: sonarr
    enabled: true
    name: Series
    url: http://127.0.0.1:8989
    apiVersion: v3
    apiKey: key
    missingSearch:
      enabled: true
      cron: '*/5 * * * *'
      maxResults: 2
      cooldown: 30d
      searchType: episode
`

func TestRequiredConfigurationValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"instances", "instances: []\n", "No instances defined"},
		{"type", strings.Replace(validSonarrConfig, "type: sonarr", "type: unknown", 1), "'type' must be"},
		{"enabled", strings.Replace(validSonarrConfig, "    enabled: true\n", "", 1), "'enabled' is required"},
		{"name", strings.Replace(validSonarrConfig, "name: Series", "name: ''", 1), "'name' is required"},
		{"URL", strings.Replace(validSonarrConfig, "http://127.0.0.1:8989", "ftp://127.0.0.1", 1), "'url' must be a valid http(s) URL"},
		{"API version", strings.Replace(validSonarrConfig, "    apiVersion: v3\n", "", 1), "'apiVersion' is required"},
		{"API key", strings.Replace(validSonarrConfig, "apiKey: key", "apiKey: ' '", 1), "'apiKey' is required"},
		{"log level", "logLevel: trace\n" + validSonarrConfig, "'logLevel' must be one of"},
		{"cron", strings.Replace(validSonarrConfig, "      cron: '*/5 * * * *'\n", "", 1), "'cron' is required"},
		{"cron fields", strings.Replace(validSonarrConfig, "*/5 * * * *", "0 */5 * * * *", 1), "'cron' must be a 5-field expression"},
		{"max results", strings.Replace(validSonarrConfig, "maxResults: 2", "maxResults: -1", 1), "'maxResults' must be 0 or greater"},
		{"cooldown", strings.Replace(validSonarrConfig, "cooldown: 30d", "cooldown: invalid", 1), "invalid 'cooldown'"},
		{"search type", strings.Replace(validSonarrConfig, "      searchType: episode\n", "", 1), "'searchType' is required"},
		{"indexer enabled", validSonarrConfig + "    indexerFilter:\n      include: [one]\n", "indexerFilter: 'enabled' is required"},
		{"tag enabled", validSonarrConfig + "      tagging:\n        name: searched\n        retroactive: false\n", "tagging: 'enabled' is required"},
		{"tag name", validSonarrConfig + "      tagging:\n        enabled: true\n        retroactive: false\n", "tagging: 'name' is required"},
		{"tag retroactive", validSonarrConfig + "      tagging:\n        enabled: false\n        name: searched\n", "tagging: 'retroactive' is required"},
		{"empty enabled tag name", validSonarrConfig + "      tagging:\n        enabled: true\n        name: ''\n        retroactive: false\n", "tagging: 'name' must not be empty"},
		{"empty rule match", validSonarrConfig + "queueCleanupRules:\n  sonarr:\n    - match: ' '\n      action: remove\n", "'match' must not be empty"},
		{"missing rule action", validSonarrConfig + "queueCleanupRules:\n  sonarr:\n    - match: SAMPLE\n", "'action' is required"},
		{"invalid rule action", validSonarrConfig + "queueCleanupRules:\n  sonarr:\n    - match: SAMPLE\n      action: delete\n", "'action' must be"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse([]byte(test.yaml))
			var configErr *Error
			if !errors.As(err, &configErr) {
				t.Fatalf("got %v, want configuration error", err)
			}
			if got := strings.Join(configErr.Errors, "\n"); !strings.Contains(got, test.want) {
				t.Fatalf("errors do not contain %q:\n%s", test.want, got)
			}
		})
	}
}

func TestConfigurationWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"disabled instances", strings.Replace(validSonarrConfig, "enabled: true", "enabled: false", 1), "No instances are enabled"},
		{"no enabled jobs", strings.Split(validSonarrConfig, "    missingSearch:")[0], "No jobs are enabled"},
		{"empty indexer filter", validSonarrConfig + "    indexerFilter:\n      enabled: true\n", "enabled but no include/exclude rules"},
		{"retroactive tagging", validSonarrConfig + "      tagging:\n        enabled: true\n        name: searched\n        retroactive: true\n", "'retroactive' is true"},
		{"unknown matcher", validSonarrConfig + "queueCleanupRules:\n  sonarr:\n    - match: FUTURE_MATCHER\n      action: remove\n", "not a known matcher key"},
		{"empty matcher list", validSonarrConfig + "queueCleanupRules:\n  sonarr: []\n", "queueCleanupRules.sonarr is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := parse([]byte(test.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(cfg.Warnings(), "\n"); !strings.Contains(got, test.want) {
				t.Fatalf("warnings do not contain %q:\n%s", test.want, got)
			}
		})
	}
}

func TestConfigurationDefaults(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "logLevel: ''\n"} {
		cfg, err := parse([]byte(prefix + validSonarrConfig))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.LogLevel != "info" {
			t.Fatalf("log level = %q, want info", cfg.LogLevel)
		}
	}
}
