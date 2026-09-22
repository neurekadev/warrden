package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInstanceVersionAndSearchTypeCombinations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, kind, version, searchType string
		valid                           bool
	}{
		{"sonarr episode", "sonarr", "v3", "episode", true},
		{"sonarr season", "sonarr", "V3", "SEASON", true},
		{"sonarr padded type", "sonarr", "v3", " episode ", false},
		{"sonarr missing", "sonarr", "v3", "", false},
		{"sonarr wrong version", "sonarr", "v1", "episode", false},
		{"radarr", "radarr", "v3", "", true},
		{"radarr search type", "radarr", "v3", "episode", false},
		{"lidarr album", "lidarr", "v1", "album", true},
		{"lidarr artist", "lidarr", "V1", "ARTIST", true},
		{"lidarr wrong type", "lidarr", "v1", "episode", false},
		{"whisparr episode", "whisparr", "v3", "episode", true},
		{"whisparr season", "whisparr", "v3", "season", true},
		{"whisparr eros", "whisparr", "v3-eros", "", true},
		{"whisparr eros type", "whisparr", "v3-eros", "episode", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			searchType := ""
			if test.searchType != "" {
				searchType = "      searchType: '" + test.searchType + "'\n"
			}
			yamlText := fmt.Sprintf("instances:\n  - type: %s\n    enabled: true\n    name: Test\n    url: http://127.0.0.1:1234\n    apiVersion: %s\n    apiKey: key\n    missingSearch:\n      enabled: true\n      cron: '* * * * *'\n      maxResults: 1\n      cooldown: 1h\n%s", test.kind, test.version, searchType)
			_, err := parse([]byte(yamlText))
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestUnknownKeyPathsAtEveryNestedBoundary(t *testing.T) {
	t.Parallel()
	data := []byte(`badRoot: true
instances:
  - type: sonarr
    enabled: true
    name: Series
    url: http://127.0.0.1:8989
    apiVersion: v3
    apiKey: key
    badInstance: true
    indexerFilter:
      enabled: false
      badFilter: true
    missingSearch:
      enabled: true
      cron: '* * * * *'
      maxResults: 1
      cooldown: 1h
      searchType: episode
      tagging:
        enabled: false
        name: searched
        retroactive: false
        badTag: true
queueCleanupRules:
  badRules: []
  sonarr:
    - match: SAMPLE
      action: remove
      badRule: true
`)
	_, err := parse(data)
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("error=%v", err)
	}
	joined := strings.Join(configErr.Errors, "\n")
	for _, want := range []string{
		"unknown key 'badRoot'.",
		"instances[0]: unknown key 'badInstance'.",
		"instances[0].indexerFilter: unknown key 'badFilter'.",
		"instances[0].missingSearch.tagging: unknown key 'badTag'.",
		"queueCleanupRules: unknown key 'badRules'.",
		"queueCleanupRules.sonarr[0]: unknown key 'badRule'.",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestParseExample(t *testing.T) {
	data, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Instances), 5; got != want {
		t.Fatalf("instances=%d, want %d", got, want)
	}
	rulesByKind := map[string][]Rule{"sonarr": cfg.QueueRules.Sonarr, "radarr": cfg.QueueRules.Radarr, "lidarr": cfg.QueueRules.Lidarr, "whisparr": cfg.QueueRules.Whisparr}
	wantCounts := map[string]int{"sonarr": 29, "radarr": 20, "lidarr": 26, "whisparr": 30}
	removeDefaults := map[string]struct{}{
		"NOT_QUALITY_UPGRADE": {}, "NOT_REVISION_UPGRADE": {}, "NOT_CUSTOM_FORMAT_UPGRADE": {},
		"MOVIE_ALREADY_IMPORTED": {}, "EPISODE_ALREADY_IMPORTED": {}, "ALBUM_ALREADY_IMPORTED": {},
	}
	noneDefaults := map[string]struct{}{
		"SAMPLE_INDETERMINATE": {}, "INSUFFICIENT_FREE_SPACE": {}, "FILE_UNPACKING": {},
		"UNEXPECTED_ERROR": {}, "LOCKED_FILE": {}, "DOWNLOAD_CLIENT_ERROR": {}, "IMPORT_PATH_INACCESSIBLE": {},
	}
	noneByKind := map[string]map[string]struct{}{
		"sonarr": {
			"TITLE_MISSING": {}, "TITLE_TBA": {}, "MISSING_ABSOLUTE_NUMBER": {}, "UNVERIFIED_SCENE_MAPPING": {},
		},
		"lidarr": {"DEST_FOLDER_NOT_ROOT": {}},
		"whisparr": {
			"TITLE_MISSING": {}, "TITLE_TBA": {}, "MISSING_ABSOLUTE_NUMBER": {}, "UNVERIFIED_SCENE_MAPPING": {},
		},
	}
	actionCounts := make(map[Action]int)
	for kind, rules := range rulesByKind {
		if len(rules) != wantCounts[kind] {
			t.Errorf("%s rules=%d, want %d", kind, len(rules), wantCounts[kind])
		}
		for _, rule := range rules {
			want := RemoveAndBlocklist
			if _, ok := removeDefaults[rule.Match]; ok {
				want = Remove
			}
			if _, ok := noneDefaults[rule.Match]; ok {
				want = None
			}
			if _, ok := noneByKind[kind][rule.Match]; ok {
				want = None
			}
			if rule.Action != want {
				t.Errorf("%s %s action=%q, want %q", kind, rule.Match, rule.Action, want)
			}
			actionCounts[rule.Action]++
		}
	}
	for action, want := range map[Action]int{RemoveAndBlocklist: 52, Remove: 16, None: 37} {
		if got := actionCounts[action]; got != want {
			t.Errorf("%s defaults=%d, want %d", action, got, want)
		}
	}
	warning := "# WARNING: Stale rclone/FUSE mounts or other network storage read failures can trigger this for healthy files."
	if got := strings.Count(string(data), warning); got != len(rulesByKind) {
		t.Errorf("SAMPLE_INDETERMINATE config warnings=%d, want %d", got, len(rulesByKind))
	}
	if cfg.Instances[0].APIVersion != "v3" {
		t.Errorf("apiVersion=%q", cfg.Instances[0].APIVersion)
	}
	for _, instance := range cfg.Instances {
		if instance.MissingSearch != nil && instance.MissingSearch.Cooldown != 30*24*time.Hour {
			t.Errorf("%s missing cooldown=%s, want 30d", instance.Name, instance.MissingSearch.Cooldown)
		}
		if instance.UpgradeSearch != nil && instance.UpgradeSearch.Cooldown != 90*24*time.Hour {
			t.Errorf("%s upgrade cooldown=%s, want 90d", instance.Name, instance.UpgradeSearch.Cooldown)
		}
	}
}

func TestDurationCompatibility(t *testing.T) {
	tests := map[string]time.Duration{
		"30d": 30 * 24 * time.Hour, "1h30m": 90 * time.Minute, "2d12h30m45s": 60*time.Hour + 30*time.Minute + 45*time.Second,
		"1 h 30 m": 90 * time.Minute, "1d2d": 72 * time.Hour, "ignored 2H suffix": 2 * time.Hour,
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := parseDuration(input)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
	for _, input := range []string{"", "   ", "abc", "30x", "12", "1y", "0d"} {
		t.Run("invalid_"+input, func(t *testing.T) {
			if _, err := parseDuration(input); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := parseDuration("2147483648s"); err == nil || !strings.Contains(err.Error(), "Int32") {
		t.Fatalf("expected Int32 compatibility overflow, got %v", err)
	}
}

func TestParseUnknownKeyPath(t *testing.T) {
	data := []byte(`instances:
  - type: sonarr
    enabled: true
    name: Series
    url: http://127.0.0.1:8989
    apiVersion: v3
    apiKey: key
    missingSearch:
      enabled: true
      cron: '* * * * *'
      maxResults: 1
      cooldown: 1h
      searchType: episode
      badKey: true
`)
	_, err := parse(data)
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("error=%v", err)
	}
	if got := strings.Join(configErr.Errors, "\n"); !strings.Contains(got, "instances[0].missingSearch: unknown key 'badKey'.") {
		t.Fatalf("errors=%s", got)
	}
}

func TestParseNormalizesTypedValues(t *testing.T) {
	data := []byte(`logLevel: WARNING
instances:
  - type: LIDARR
    enabled: true
    name: Music
    url: https://arr.example.test/lidarr
    apiVersion: ' V1 '
    apiKey: key
    missingSearch:
      enabled: true
      cron: '0 1 * * *'
      maxResults: 4
      cooldown: 2h
      searchType: ARTIST
`)
	cfg, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	instance := cfg.Instances[0]
	if cfg.LogLevel != "WARNING" || instance.Kind != Lidarr || instance.APIVersion != "v1" || instance.MissingSearch.SearchType != Artist || instance.MissingSearch.Cooldown != 2*time.Hour {
		t.Fatalf("unexpected normalization: %#v %#v", cfg, instance)
	}
}

func TestOptionsFromEnv(t *testing.T) {
	for key, value := range map[string]string{"DRY_RUN": "TRUE", "HTTP_RETRY_COUNT": "0", "HTTP_TIMEOUT_SECONDS": "45", "TZ": "UTC", "GIT_TAG": "1.2.3"} {
		t.Setenv(key, value)
	}
	opts := OptionsFromEnv()
	if !opts.DryRun || opts.RetryCount != 0 || opts.AttemptTimeout != 45*time.Second || opts.AppVersion != "1.2.3" {
		t.Fatalf("options=%#v", opts)
	}
}

func TestOptionsUseBuildVersionWithoutRuntimeOverride(t *testing.T) {
	t.Setenv("GIT_TAG", "4.2.1")
	if got := OptionsFromEnv().AppVersion; got != "4.2.1" {
		t.Fatalf("build version = %q", got)
	}
	t.Setenv("APP_VERSION", "operator-override")
	if got := OptionsFromEnv().AppVersion; got != "4.2.1" {
		t.Fatalf("APP_VERSION changed build version to %q", got)
	}
	t.Setenv("GIT_TAG", "")
	if got := OptionsFromEnv().AppVersion; got != "dev" {
		t.Fatalf("empty build version = %q", got)
	}
}

func TestOptionsUseInt32CompatibilityAndDefaults(t *testing.T) {
	for _, value := range []string{"", "invalid", "-1", "2147483648"} {
		t.Run("retry_"+value, func(t *testing.T) {
			t.Setenv("HTTP_RETRY_COUNT", value)
			if got := OptionsFromEnv().RetryCount; got != defaultRetryCount {
				t.Fatalf("retry count = %d, want %d", got, defaultRetryCount)
			}
		})
	}
	for _, value := range []string{"", "invalid", "0", "-1", "2147483648"} {
		t.Run("timeout_"+value, func(t *testing.T) {
			t.Setenv("HTTP_TIMEOUT_SECONDS", value)
			if got := OptionsFromEnv().AttemptTimeout; got != defaultTimeoutSecond*time.Second {
				t.Fatalf("timeout = %s, want %ds", got, defaultTimeoutSecond)
			}
		})
	}
}
