package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/neurekadev/warrden/internal/matcher"
	"go.yaml.in/yaml/v3"
)

var yamlLinePattern = regexp.MustCompile(`line (\d+)`)

// Error contains every configuration validation failure found in one pass.
type Error struct {
	Errors []string
}

func (e *Error) Error() string { return strings.Join(e.Errors, "\n") }

// Load reads, validates, and normalizes a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(data)
}

func parse(data []byte) (*Config, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, &Error{Errors: []string{formatYAMLError(err)}}
	}

	var raw rawConfig
	if err := document.Decode(&raw); err != nil {
		return nil, &Error{Errors: []string{formatYAMLError(err)}}
	}

	errors := unknownKeys(&document)
	warnings := make([]string, 0)
	errors = append(errors, validate(&raw, &warnings)...)
	if len(errors) > 0 {
		return nil, &Error{Errors: errors}
	}

	cfg, err := normalize(&raw)
	if err != nil {
		return nil, err
	}
	cfg.warnings = warnings
	return cfg, nil
}

func formatYAMLError(err error) string {
	line := "1"
	if match := yamlLinePattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		line = match[1]
	}
	return fmt.Sprintf("YAML parse error at line %s, column 1: %s", line, err)
}

func validate(raw *rawConfig, warnings *[]string) []string {
	errors := make([]string, 0)
	if len(raw.Instances) == 0 {
		return []string{"No instances defined. The 'instances' list must contain at least one entry."}
	}

	names := make(map[string]struct{}, len(raw.Instances))
	for index := range raw.Instances {
		instance := &raw.Instances[index]
		prefix := fmt.Sprintf("instances[%d]", index)
		kind := Kind(strings.ToLower(instance.Type))

		if strings.TrimSpace(instance.Name) == "" {
			errors = append(errors, prefix+": 'name' is required.")
		} else if validKind(kind) {
			key := strings.ToLower(instance.Name)
			if _, exists := names[key]; exists {
				errors = append(errors, fmt.Sprintf("Duplicate instance name: '%s'. Instance names must be unique.", instance.Name))
			} else {
				names[key] = struct{}{}
			}
		} else {
			errors = append(errors, fmt.Sprintf("%s '%s': 'type' must be 'sonarr', 'radarr', 'lidarr' or 'whisparr'.", prefix, instance.Name))
		}

		if strings.TrimSpace(instance.Type) == "" {
			errors = append(errors, prefix+": 'type' is required.")
		} else if !validKind(kind) {
			errors = append(errors, fmt.Sprintf("%s '%s': 'type' must be 'sonarr', 'radarr', 'lidarr' or 'whisparr'.", prefix, instance.Name))
		}
		if instance.Enabled == nil {
			errors = append(errors, fmt.Sprintf("%s '%s': 'enabled' is required.", prefix, instance.Name))
		}
		if !validURL(instance.URL) {
			errors = append(errors, fmt.Sprintf("%s '%s': 'url' must be a valid http(s) URL.", prefix, instance.Name))
		}
		if strings.TrimSpace(instance.APIKey) == "" {
			errors = append(errors, fmt.Sprintf("%s '%s': 'apiKey' is required.", prefix, instance.Name))
		}
		errors = append(errors, validateAPIVersion(instance, kind, prefix)...)
		errors = append(errors, validateFilter(instance, prefix, warnings)...)
		errors = append(errors, validateJob(instance, kind, "missingSearch", instance.MissingSearch, index, warnings)...)
		errors = append(errors, validateJob(instance, kind, "upgradeSearch", instance.UpgradeSearch, index, warnings)...)
		errors = append(errors, validateJob(instance, kind, "queueCleanup", instance.QueueCleanup, index, warnings)...)
	}

	errors = append(errors, validateRules(raw, warnings)...)
	if raw.LogLevel != nil && strings.TrimSpace(*raw.LogLevel) != "" && !validLogLevel(*raw.LogLevel) {
		errors = append(errors, fmt.Sprintf("'logLevel' must be one of: debug, info, warning, error (got '%s').", *raw.LogLevel))
	}

	enabledInstances, enabledJobs := 0, 0
	for index := range raw.Instances {
		instance := &raw.Instances[index]
		if instance.Enabled == nil || !*instance.Enabled {
			continue
		}
		enabledInstances++
		for _, job := range []*rawJob{instance.MissingSearch, instance.UpgradeSearch, instance.QueueCleanup} {
			if job != nil && job.Enabled != nil && *job.Enabled {
				enabledJobs++
			}
		}
	}
	if enabledInstances == 0 {
		*warnings = append(*warnings, "No instances are enabled — no jobs will be scheduled.")
	} else if enabledJobs == 0 {
		*warnings = append(*warnings, "No jobs are enabled across any enabled instance — no jobs will be scheduled.")
	}

	return errors
}

func validateAPIVersion(instance *rawInstance, kind Kind, prefix string) []string {
	if instance.APIVersion == nil || strings.TrimSpace(*instance.APIVersion) == "" {
		return []string{fmt.Sprintf("%s '%s': 'apiVersion' is required.", prefix, instance.Name)}
	}
	version := strings.ToLower(strings.TrimSpace(*instance.APIVersion))
	switch kind {
	case Sonarr, Radarr:
		if version != "v3" {
			return []string{fmt.Sprintf("%s '%s': 'apiVersion' must be 'v3'.", prefix, instance.Name)}
		}
	case Lidarr:
		if version != "v1" {
			return []string{fmt.Sprintf("%s '%s': 'apiVersion' must be 'v1'.", prefix, instance.Name)}
		}
	case Whisparr:
		if version != "v3" && version != "v3-eros" {
			return []string{fmt.Sprintf("%s '%s': 'apiVersion' must be 'v3' or 'v3-eros'.", prefix, instance.Name)}
		}
	}
	return nil
}

func validateFilter(instance *rawInstance, prefix string, warnings *[]string) []string {
	filter := instance.IndexerFilter
	if filter == nil {
		return nil
	}
	if filter.Enabled == nil {
		return []string{fmt.Sprintf("%s '%s'.indexerFilter: 'enabled' is required.", prefix, instance.Name)}
	}
	if *filter.Enabled && len(filter.Include) == 0 && len(filter.Exclude) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("%s '%s'.indexerFilter: enabled but no include/exclude rules configured — this is equivalent to not having the filter.", prefix, instance.Name))
	}
	return nil
}

func validateJob(instance *rawInstance, kind Kind, key string, job *rawJob, index int, warnings *[]string) []string {
	if job == nil {
		return nil
	}
	prefix := fmt.Sprintf("instances[%d] '%s'.%s", index, instance.Name, key)
	errors := make([]string, 0)
	if job.Enabled == nil {
		errors = append(errors, prefix+": 'enabled' is required.")
	}
	if job.Cron == nil {
		errors = append(errors, prefix+": 'cron' is required.")
	} else if job.Enabled != nil && *job.Enabled && len(strings.Fields(*job.Cron)) != 5 {
		errors = append(errors, prefix+": 'cron' must be a 5-field expression 'min hour dom month dow'.")
	}

	if key != "queueCleanup" {
		if job.MaxResults == nil {
			errors = append(errors, prefix+": 'maxResults' is required.")
		} else if *job.MaxResults < 0 {
			errors = append(errors, prefix+": 'maxResults' must be 0 or greater.")
		}
		if job.Cooldown == nil {
			errors = append(errors, prefix+": 'cooldown' is required.")
		} else if _, err := parseDuration(*job.Cooldown); err != nil {
			errors = append(errors, fmt.Sprintf("%s: invalid 'cooldown' - %s.", prefix, capitalize(err.Error())))
		}
	}

	version := ""
	if instance.APIVersion != nil {
		version = strings.ToLower(strings.TrimSpace(*instance.APIVersion))
	}
	eros := kind == Whisparr && version == "v3-eros"
	searchType := ""
	if job.SearchType != nil {
		searchType = *job.SearchType
	}
	if key != "queueCleanup" {
		switch {
		case kind == Sonarr || (kind == Whisparr && !eros):
			if strings.TrimSpace(searchType) == "" {
				errors = append(errors, fmt.Sprintf("%s: 'searchType' is required for %s instances.", prefix, instance.Type))
			} else if !equalFoldAny(searchType, "episode", "season") {
				errors = append(errors, prefix+": 'searchType' must be 'episode' or 'season'.")
			}
		case kind == Lidarr:
			if strings.TrimSpace(searchType) == "" {
				errors = append(errors, fmt.Sprintf("%s: 'searchType' is required for %s instances.", prefix, instance.Type))
			} else if !equalFoldAny(searchType, "album", "artist") {
				errors = append(errors, prefix+": 'searchType' must be 'album' or 'artist'.")
			}
		case kind == Radarr || eros:
			if strings.TrimSpace(searchType) != "" {
				errors = append(errors, fmt.Sprintf("%s: 'searchType' is not valid for %s instances.", prefix, instance.Type))
			}
		}
	}

	if tag := job.Tagging; tag != nil {
		tagPrefix := prefix + ".tagging"
		if tag.Enabled == nil {
			errors = append(errors, tagPrefix+": 'enabled' is required.")
		}
		if tag.Name == nil {
			errors = append(errors, tagPrefix+": 'name' is required.")
		}
		if tag.Retroactive == nil {
			errors = append(errors, tagPrefix+": 'retroactive' is required.")
		}
		if tag.Enabled != nil && *tag.Enabled && (tag.Name == nil || strings.TrimSpace(*tag.Name) == "") {
			errors = append(errors, tagPrefix+": 'name' must not be empty when tagging is enabled.")
		}
		if tag.Retroactive != nil && *tag.Retroactive {
			*warnings = append(*warnings, tagPrefix+": 'retroactive' is true — set to false after first run to skip unnecessary API calls on each startup.")
		}
	}
	return errors
}

func validateRules(raw *rawConfig, warnings *[]string) []string {
	if raw.QueueCleanupRules == nil {
		return nil
	}
	present := make(map[string]bool)
	for _, instance := range raw.Instances {
		if strings.TrimSpace(instance.Type) != "" {
			present[strings.ToLower(instance.Type)] = true
		}
	}

	errors := make([]string, 0)
	lists := []struct {
		kind  string
		rules []rawRule
	}{
		{"sonarr", raw.QueueCleanupRules.Sonarr},
		{"radarr", raw.QueueCleanupRules.Radarr},
		{"lidarr", raw.QueueCleanupRules.Lidarr},
		{"whisparr", raw.QueueCleanupRules.Whisparr},
	}
	for _, list := range lists {
		if !present[list.kind] {
			continue
		}
		if len(list.rules) == 0 {
			*warnings = append(*warnings, fmt.Sprintf("queueCleanupRules.%s is empty; no queue warnings will be matched for %s instances.", list.kind, list.kind))
			continue
		}
		for index, rule := range list.rules {
			prefix := fmt.Sprintf("queueCleanupRules.%s[%d]", list.kind, index)
			if strings.TrimSpace(rule.Match) == "" {
				errors = append(errors, prefix+": 'match' must not be empty.")
			} else if !matcher.Valid(rule.Match) {
				*warnings = append(*warnings, fmt.Sprintf("%s: '%s' is not a known matcher key and will be skipped. See config.example.yaml for available keys.", prefix, rule.Match))
			}
			action := strings.TrimSpace(rule.Action)
			if action == "" {
				errors = append(errors, prefix+": 'action' is required.")
			} else if !equalFoldAny(action, string(Remove), string(RemoveAndBlocklist), string(None)) {
				errors = append(errors, fmt.Sprintf("%s: 'action' must be 'remove', 'removeAndBlocklist', or 'none', got '%s'.", prefix, rule.Action))
			}
			if rule.Match == "SAMPLE_INDETERMINATE" && equalFoldAny(action, string(Remove), string(RemoveAndBlocklist)) {
				*warnings = append(*warnings, fmt.Sprintf("%s: SAMPLE_INDETERMINATE uses action '%s' — transient rclone/FUSE or network storage read failures can remove or blocklist healthy downloads; use 'none' unless this risk is acceptable.", prefix, rule.Action))
			}
		}
	}
	return errors
}

func normalize(raw *rawConfig) (*Config, error) {
	cfg := &Config{LogLevel: "info"}
	if raw.LogLevel != nil && strings.TrimSpace(*raw.LogLevel) != "" {
		cfg.LogLevel = *raw.LogLevel
	}
	for index := range raw.Instances {
		rawInstance := &raw.Instances[index]
		parsedURL, err := url.Parse(rawInstance.URL)
		if err != nil {
			return nil, fmt.Errorf("parse instance URL: %w", err)
		}
		parsedURL.Scheme = strings.ToLower(parsedURL.Scheme)
		instance := Instance{
			Kind: Kind(strings.ToLower(rawInstance.Type)), Enabled: *rawInstance.Enabled,
			Name: rawInstance.Name, URL: parsedURL, URLText: rawInstance.URL,
			APIVersion: strings.ToLower(strings.TrimSpace(*rawInstance.APIVersion)), APIKey: rawInstance.APIKey,
		}
		if rawInstance.IndexerFilter != nil {
			instance.IndexerFilter = &IndexerFilter{
				Enabled: *rawInstance.IndexerFilter.Enabled,
				Include: append([]string(nil), rawInstance.IndexerFilter.Include...),
				Exclude: append([]string(nil), rawInstance.IndexerFilter.Exclude...),
			}
		}
		instance.MissingSearch, err = normalizeJob(rawInstance.MissingSearch, true)
		if err != nil {
			return nil, err
		}
		instance.UpgradeSearch, err = normalizeJob(rawInstance.UpgradeSearch, true)
		if err != nil {
			return nil, err
		}
		instance.QueueCleanup, err = normalizeJob(rawInstance.QueueCleanup, false)
		if err != nil {
			return nil, err
		}
		cfg.Instances = append(cfg.Instances, instance)
	}
	if rules := raw.QueueCleanupRules; rules != nil {
		cfg.QueueRules = QueueRules{
			Sonarr: normalizeRules(rules.Sonarr), Radarr: normalizeRules(rules.Radarr),
			Lidarr: normalizeRules(rules.Lidarr), Whisparr: normalizeRules(rules.Whisparr),
		}
	}
	return cfg, nil
}

func normalizeJob(raw *rawJob, search bool) (*Job, error) {
	if raw == nil {
		return nil, nil
	}
	job := &Job{Enabled: *raw.Enabled, Cron: *raw.Cron}
	if search {
		job.MaxResults = *raw.MaxResults
		var err error
		job.Cooldown, err = parseDuration(*raw.Cooldown)
		if err != nil {
			return nil, err
		}
		if raw.SearchType != nil {
			job.SearchType = SearchType(strings.ToLower(*raw.SearchType))
		}
	}
	if raw.Tagging != nil {
		job.Tagging = &Tagging{Enabled: *raw.Tagging.Enabled, Name: *raw.Tagging.Name, Retroactive: *raw.Tagging.Retroactive}
	}
	return job, nil
}

func normalizeRules(raw []rawRule) []Rule {
	rules := make([]Rule, 0, len(raw))
	for _, rule := range raw {
		action := Action(rule.Action)
		if strings.EqualFold(rule.Action, string(RemoveAndBlocklist)) {
			action = RemoveAndBlocklist
		}
		if strings.EqualFold(rule.Action, string(Remove)) {
			action = Remove
		}
		if strings.EqualFold(rule.Action, string(None)) {
			action = None
		}
		rules = append(rules, Rule{Match: rule.Match, Action: action})
	}
	return rules
}

func validKind(kind Kind) bool {
	return kind == Sonarr || kind == Radarr || kind == Lidarr || kind == Whisparr
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && equalFoldAny(u.Scheme, "http", "https") && u.Host != ""
}

func validLogLevel(level string) bool {
	return equalFoldAny(strings.TrimSpace(level), "debug", "info", "warning", "error")
}

func equalFoldAny(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func capitalize(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func unknownKeys(document *yaml.Node) []string {
	if len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	errors := checkKeys(root, "", set("logLevel", "instances", "queueCleanupRules"))
	if instances := mappingValue(root, "instances"); instances != nil && instances.Kind == yaml.SequenceNode {
		for index, node := range instances.Content {
			if node.Kind != yaml.MappingNode {
				continue
			}
			prefix := fmt.Sprintf("instances[%d]", index)
			errors = append(errors, checkKeys(node, prefix, set("type", "enabled", "name", "url", "apiVersion", "apiKey", "indexerFilter", "missingSearch", "upgradeSearch", "queueCleanup"))...)
			if filter := mappingValue(node, "indexerFilter"); filter != nil && filter.Kind == yaml.MappingNode {
				errors = append(errors, checkKeys(filter, prefix+".indexerFilter", set("enabled", "include", "exclude"))...)
			}
			for _, key := range []string{"missingSearch", "upgradeSearch", "queueCleanup"} {
				job := mappingValue(node, key)
				if job == nil || job.Kind != yaml.MappingNode {
					continue
				}
				errors = append(errors, checkKeys(job, prefix+"."+key, set("enabled", "cron", "maxResults", "cooldown", "searchType", "tagging"))...)
				if tag := mappingValue(job, "tagging"); tag != nil && tag.Kind == yaml.MappingNode {
					errors = append(errors, checkKeys(tag, prefix+"."+key+".tagging", set("enabled", "name", "retroactive"))...)
				}
			}
		}
	}
	if rules := mappingValue(root, "queueCleanupRules"); rules != nil && rules.Kind == yaml.MappingNode {
		errors = append(errors, checkKeys(rules, "queueCleanupRules", set("sonarr", "radarr", "lidarr", "whisparr"))...)
		for _, kind := range []string{"sonarr", "radarr", "lidarr", "whisparr"} {
			sequence := mappingValue(rules, kind)
			if sequence == nil || sequence.Kind != yaml.SequenceNode {
				continue
			}
			for index, rule := range sequence.Content {
				if rule.Kind == yaml.MappingNode {
					errors = append(errors, checkKeys(rule, fmt.Sprintf("queueCleanupRules.%s[%d]", kind, index), set("match", "action"))...)
				}
			}
		}
	}
	return errors
}

func checkKeys(node *yaml.Node, prefix string, known map[string]struct{}) []string {
	errors := make([]string, 0)
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if _, ok := known[key]; ok {
			continue
		}
		if prefix == "" {
			errors = append(errors, fmt.Sprintf("unknown key '%s'.", key))
		} else {
			errors = append(errors, fmt.Sprintf("%s: unknown key '%s'.", prefix, key))
		}
	}
	return errors
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
