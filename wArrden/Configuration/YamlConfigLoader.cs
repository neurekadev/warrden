using wArrden.Services;
using YamlDotNet.Core;
using YamlDotNet.RepresentationModel;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;

namespace wArrden.Configuration;

internal static class YamlConfigLoader
{
    public static AppConfig Load(string path)
    {
        if (!File.Exists(path))
            throw new FileNotFoundException($"Config file not found: {path}");

        var yaml = File.ReadAllText(path);
        AppConfig config;

        try
        {
            var deserializer = new DeserializerBuilder()
                .WithNamingConvention(CamelCaseNamingConvention.Instance)
                .IgnoreUnmatchedProperties()
                .Build();

            config = deserializer.Deserialize<AppConfig>(yaml) ?? new AppConfig();
        }
        catch (YamlException ex)
        {
            var start = ex.Start;
            var loc = $"line {start.Line}, column {start.Column}";
            throw new ConfigurationException(new List<string>
            {
                $"YAML parse error at {loc}: {ex.InnerException?.Message ?? ex.Message}"
            });
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException($"Failed to parse config file '{path}': {ex.Message}", ex);
        }

        var errors = CollectUnknownKeys(yaml);
        errors.AddRange(Validate(config));
        if (errors.Count > 0)
            throw new ConfigurationException(errors);

        Normalize(config);

        return config;
    }

    private static void Normalize(AppConfig config)
    {
        foreach (var inst in config.Instances)
        {
            inst.ApiVersion = NormalizeApiVersion(inst.ApiVersion);

            if (inst.MissingSearch is not null && (inst.IsSonarr || (inst.IsWhisparr && !inst.IsWhisparrV3Eros) || inst.IsLidarr))
                inst.MissingSearch.SearchType = inst.MissingSearch.SearchType!.ToLowerInvariant();

            if (inst.UpgradeSearch is not null && (inst.IsSonarr || (inst.IsWhisparr && !inst.IsWhisparrV3Eros) || inst.IsLidarr))
                inst.UpgradeSearch.SearchType = inst.UpgradeSearch.SearchType!.ToLowerInvariant();
        }
    }

    private static List<string> CollectUnknownKeys(string yaml)
    {
        var errors = new List<string>();
        try
        {
            var yamlStream = new YamlStream();
            using var reader = new StringReader(yaml);
            yamlStream.Load(reader);

            if (yamlStream.Documents.Count == 0)
                return errors;

            var root = (YamlMappingNode)yamlStream.Documents[0].RootNode;

            var rootKnown = new HashSet<string>
                { "logLevel", "instances", "queueCleanupRules" };
            CheckUnknownKeys(root, rootKnown, "", errors);

            if (TryGetChild(root, "instances") is YamlSequenceNode instancesSeq)
            {
                for (int i = 0; i < instancesSeq.Children.Count; i++)
                {
                    if (instancesSeq.Children[i] is YamlMappingNode instNode)
                        CheckInstanceKeys(instNode, i, errors);
                }
            }

            if (TryGetChild(root, "queueCleanupRules") is YamlMappingNode rulesNode)
            {
                var rulesKnown = new HashSet<string>
                    { "sonarr", "radarr", "lidarr", "whisparr" };
                CheckUnknownKeys(rulesNode, rulesKnown, "queueCleanupRules", errors);

                foreach (var arrType in new[] { "sonarr", "radarr", "lidarr", "whisparr" })
                {
                    if (TryGetChild(rulesNode, arrType) is YamlSequenceNode ruleSeq)
                    {
                        var ruleKnown = new HashSet<string> { "match", "action" };
                        for (int i = 0; i < ruleSeq.Children.Count; i++)
                        {
                            if (ruleSeq.Children[i] is YamlMappingNode ruleNode)
                                CheckUnknownKeys(ruleNode, ruleKnown,
                                    $"queueCleanupRules.{arrType}[{i}]", errors);
                        }
                    }
                }
            }
        }
        catch (YamlException)
        {
        }

        return errors;
    }

    private static void CheckInstanceKeys(YamlMappingNode instNode, int idx, List<string> errors)
    {
        var known = new HashSet<string>
        {
            "type", "enabled", "name", "url", "apiVersion", "apiKey",
            "indexerFilter", "missingSearch", "upgradeSearch", "queueCleanup"
        };
        CheckUnknownKeys(instNode, known, $"instances[{idx}]", errors);

        if (TryGetChild(instNode, "indexerFilter") is YamlMappingNode filterNode)
        {
            var filterKnown = new HashSet<string> { "enabled", "include", "exclude" };
            CheckUnknownKeys(filterNode, filterKnown, $"instances[{idx}].indexerFilter", errors);
        }

        var jobKnown = new HashSet<string>
            { "enabled", "cron", "maxResults", "cooldown", "searchType", "tagging" };

        foreach (var jobKey in new[] { "missingSearch", "upgradeSearch", "queueCleanup" })
        {
            if (TryGetChild(instNode, jobKey) is YamlMappingNode jobNode)
            {
                CheckUnknownKeys(jobNode, jobKnown, $"instances[{idx}].{jobKey}", errors);

                if (TryGetChild(jobNode, "tagging") is YamlMappingNode taggingNode)
                {
                    var tagKnown = new HashSet<string> { "enabled", "name", "retroactive" };
                    CheckUnknownKeys(taggingNode, tagKnown,
                        $"instances[{idx}].{jobKey}.tagging", errors);
                }
            }
        }
    }

    private static void CheckUnknownKeys(YamlMappingNode node, HashSet<string> known,
        string prefix, List<string> errors)
    {
        foreach (var kvp in node.Children)
        {
            if (kvp.Key is not YamlScalarNode keyNode) continue;
            var key = keyNode.Value;
            if (key is null) continue;
            if (!known.Contains(key))
                errors.Add(string.IsNullOrEmpty(prefix)
                    ? $"unknown key '{key}'."
                    : $"{prefix}: unknown key '{key}'.");
        }
    }

    private static YamlNode? TryGetChild(YamlMappingNode parent, string childKey)
    {
        foreach (var kvp in parent.Children)
        {
            if (kvp.Key is YamlScalarNode keyNode &&
                string.Equals(keyNode.Value, childKey, StringComparison.Ordinal))
                return kvp.Value;
        }
        return null;
    }

    internal static List<string> Validate(AppConfig config)
    {
        var errors = new List<string>();

        if (config.Instances is null || config.Instances.Count == 0)
        {
            errors.Add("No instances defined. The 'instances' list must contain at least one entry.");
            return errors;
        }

        var usedNames = new HashSet<string>(StringComparer.OrdinalIgnoreCase);

        for (int i = 0; i < config.Instances.Count; i++)
        {
            var inst = config.Instances[i];
            var prefix = $"instances[{i}]";

            if (string.IsNullOrWhiteSpace(inst.Name))
            {
                errors.Add($"{prefix}: 'name' is required.");
            }
            else
            {
                if (inst.IsSonarr || inst.IsRadarr || inst.IsLidarr || inst.IsWhisparr) TrackName(usedNames, inst.Name, errors);
                else
                    errors.Add($"{prefix} '{inst.Name}': 'type' must be 'sonarr', 'radarr', 'lidarr' or 'whisparr'.");
            }

            if (string.IsNullOrWhiteSpace(inst.Type))
                errors.Add($"{prefix}: 'type' is required.");
            else if (!inst.IsSonarr && !inst.IsRadarr && !inst.IsLidarr && !inst.IsWhisparr)
                errors.Add($"{prefix} '{inst.Name}': 'type' must be 'sonarr', 'radarr', 'lidarr' or 'whisparr'.");

            if (inst.Enabled is null)
                errors.Add($"{prefix} '{inst.Name}': 'enabled' is required.");

            if (!IsValidUrl(inst.Url))
                errors.Add($"{prefix} '{inst.Name}': 'url' must be a valid http(s) URL.");

            if (string.IsNullOrWhiteSpace(inst.ApiKey))
                errors.Add($"{prefix} '{inst.Name}': 'apiKey' is required.");

            ValidateApiVersion(errors, inst, prefix);

            ValidateIndexerFilter(errors, config, inst, prefix);

            ValidateJob(errors, inst, "missingSearch", i, config);
            ValidateJob(errors, inst, "upgradeSearch", i, config);
            ValidateJob(errors, inst, "queueCleanup", i, config);
        }

        ValidateQueueCleanupRules(errors, config.QueueCleanupRules, config);

        if (!string.IsNullOrWhiteSpace(config.LogLevel) &&
            !IsValidLogLevel(config.LogLevel))
            errors.Add($"'logLevel' must be one of: debug, info, warning, error (got '{config.LogLevel}').");

        // Jobs on a disabled instance are never scheduled, so only enabled instances can
        // contribute a job that actually runs.
        var enabledInstances = config.Instances.Count(i => i.Enabled == true);
        var enabledJobs = config.Instances
            .Where(i => i.Enabled == true)
            .Sum(i =>
                (i.MissingSearch?.Enabled == true ? 1 : 0) +
                (i.UpgradeSearch?.Enabled == true ? 1 : 0) +
                (i.QueueCleanup?.Enabled == true ? 1 : 0));

        // Both cases leave the scheduler empty. Warn about the outermost cause only, so the
        // operator is pointed at the one setting that has to change.
        if (enabledInstances == 0)
            config.AddWarning("No instances are enabled — no jobs will be scheduled.");
        else if (enabledJobs == 0)
            config.AddWarning("No jobs are enabled across any enabled instance — no jobs will be scheduled.");

        return errors;
    }

    private static void ValidateIndexerFilter(List<string> errors, AppConfig config, InstanceConfig inst, string prefix)
    {
        var filter = inst.IndexerFilter;
        if (filter is null)
            return;

        if (filter.Enabled is null)
        {
            errors.Add($"{prefix} '{inst.Name}'.indexerFilter: 'enabled' is required.");
            return;
        }

        if (filter.Enabled == true &&
            (filter.Include is null || filter.Include.Count == 0) &&
            (filter.Exclude is null || filter.Exclude.Count == 0))
        {
            config.AddWarning($"{prefix} '{inst.Name}'.indexerFilter: enabled but no include/exclude rules configured — this is equivalent to not having the filter.");
        }
    }

    private static void ValidateJob(List<string> errors, InstanceConfig inst, string jobKey, int idx, AppConfig config)
    {
        var job = jobKey switch
        {
            "missingSearch" => inst.MissingSearch,
            "upgradeSearch" => inst.UpgradeSearch,
            "queueCleanup" => inst.QueueCleanup,
            _ => null
        };

        if (job is null) return;

        var prefix = $"instances[{idx}] '{inst.Name}'.{jobKey}";

        if (job.Enabled is null)
            errors.Add($"{prefix}: 'enabled' is required.");

        if (job.Cron is null)
            errors.Add($"{prefix}: 'cron' is required.");
        else if (job.Enabled == true && !IsValidCron(job.Cron))
            errors.Add($"{prefix}: 'cron' must be a 5-field expression 'min hour dom month dow'.");

        if (jobKey != "queueCleanup")
        {
            if (job.MaxResults is null)
                errors.Add($"{prefix}: 'maxResults' is required.");
            else if (job.MaxResults < 0)
                errors.Add($"{prefix}: 'maxResults' must be 0 or greater.");

            if (job.Cooldown is null)
                errors.Add($"{prefix}: 'cooldown' is required.");
            else
            {
                try { DurationParser.Parse(job.Cooldown); }
                catch (Exception ex) { errors.Add($"{prefix}: invalid 'cooldown' - {ex.Message}"); }
            }
        }

        var isWhisparrEros = inst.IsWhisparr && IsApiVersion(inst, "v3-eros");

        if ((inst.IsSonarr || (inst.IsWhisparr && !isWhisparrEros)) && jobKey != "queueCleanup")
        {
            if (string.IsNullOrWhiteSpace(job.SearchType))
            {
                errors.Add($"{prefix}: 'searchType' is required for {inst.Type} instances.");
            }
            else
            {
                var st = job.SearchType;
                if (!string.Equals(st, "episode", StringComparison.OrdinalIgnoreCase) &&
                    !string.Equals(st, "season", StringComparison.OrdinalIgnoreCase))
                {
                    errors.Add($"{prefix}: 'searchType' must be 'episode' or 'season'.");
                }
            }
        }

        if (inst.IsLidarr && jobKey != "queueCleanup")
        {
            if (string.IsNullOrWhiteSpace(job.SearchType))
            {
                errors.Add($"{prefix}: 'searchType' is required for {inst.Type} instances.");
            }
            else
            {
                var st = job.SearchType;
                if (!string.Equals(st, "album", StringComparison.OrdinalIgnoreCase) &&
                    !string.Equals(st, "artist", StringComparison.OrdinalIgnoreCase))
                {
                    errors.Add($"{prefix}: 'searchType' must be 'album' or 'artist'.");
                }
            }
        }

        if ((inst.IsRadarr || isWhisparrEros) && jobKey != "queueCleanup" && !string.IsNullOrWhiteSpace(job.SearchType))
        {
            errors.Add($"{prefix}: 'searchType' is not valid for {inst.Type} instances.");
        }

        if (job.Tagging is not null)
        {
            var tp = $"{prefix}.tagging";

            if (job.Tagging.Enabled is null)
                errors.Add($"{tp}: 'enabled' is required.");
            if (job.Tagging.Name is null)
                errors.Add($"{tp}: 'name' is required.");
            if (job.Tagging.Retroactive is null)
                errors.Add($"{tp}: 'retroactive' is required.");

            if (job.Tagging.Enabled == true && string.IsNullOrWhiteSpace(job.Tagging.Name))
                errors.Add($"{tp}: 'name' must not be empty when tagging is enabled.");

            if (job.Tagging.Retroactive == true)
                config.AddWarning($"{tp}: 'retroactive' is true — set to false after first run to skip unnecessary API calls on each startup.");
        }
    }

    private static void ValidateQueueCleanupRules(List<string> errors, QueueCleanupRulesConfig? rules, AppConfig config)
    {
        if (rules is null) return;

        var presentTypes = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
        foreach (var inst in config.Instances)
        {
            if (!string.IsNullOrWhiteSpace(inst.Type))
                presentTypes.Add(inst.Type);
        }

        ValidateRuleList(errors, config, rules.Sonarr, "sonarr", presentTypes);
        ValidateRuleList(errors, config, rules.Radarr, "radarr", presentTypes);
        ValidateRuleList(errors, config, rules.Lidarr, "lidarr", presentTypes);
        ValidateRuleList(errors, config, rules.Whisparr, "whisparr", presentTypes);
    }

    private static void ValidateRuleList(List<string> errors, AppConfig config, List<QueueCleanupRuleConfig>? list, string type, HashSet<string> presentTypes)
    {
        if (!presentTypes.Contains(type))
            return;

        if (list is null || list.Count == 0)
        {
            config.AddWarning($"queueCleanupRules.{type} is empty; no queue warnings will be matched for {type} instances.");
            return;
        }

        for (int i = 0; i < list.Count; i++)
        {
            var prefix = $"queueCleanupRules.{type}[{i}]";
            var rule = list[i];

            if (string.IsNullOrWhiteSpace(rule.Match))
                errors.Add($"{prefix}: 'match' must not be empty.");
            else if (!QueueCleanupRuleMatchers.IsValidKey(rule.Match))
                config.AddWarning($"{prefix}: '{rule.Match}' is not a known matcher key and will be skipped. See config.example.yaml for available keys.");

            var action = rule.Action?.Trim();
            if (string.IsNullOrWhiteSpace(action))
            {
                errors.Add($"{prefix}: 'action' is required.");
            }
            else if (!string.Equals(action, "remove", StringComparison.OrdinalIgnoreCase) &&
                     !string.Equals(action, "removeAndBlocklist", StringComparison.OrdinalIgnoreCase) &&
                     !string.Equals(action, "none", StringComparison.OrdinalIgnoreCase))
            {
                errors.Add($"{prefix}: 'action' must be 'remove', 'removeAndBlocklist', or 'none', got '{rule.Action}'.");
            }
        }
    }

    private static void TrackName(HashSet<string> usedNames, string name, List<string> errors)
    {
        if (!usedNames.Add(name))
            errors.Add($"Duplicate instance name: '{name}'. Instance names must be unique.");
    }

    private static bool IsValidUrl(string? url) =>
        !string.IsNullOrWhiteSpace(url) &&
        Uri.TryCreate(url, UriKind.Absolute, out var u) &&
        (u.Scheme == "http" || u.Scheme == "https");

    private static void ValidateApiVersion(List<string> errors, InstanceConfig inst, string prefix)
    {
        var version = NormalizeApiVersion(inst.ApiVersion);
        if (string.IsNullOrWhiteSpace(version))
        {
            errors.Add($"{prefix} '{inst.Name}': 'apiVersion' is required.");
            return;
        }

        if (inst.IsSonarr && version != "v3")
            errors.Add($"{prefix} '{inst.Name}': 'apiVersion' must be 'v3'.");
        else if (inst.IsRadarr && version != "v3")
            errors.Add($"{prefix} '{inst.Name}': 'apiVersion' must be 'v3'.");
        else if (inst.IsLidarr && version != "v1")
            errors.Add($"{prefix} '{inst.Name}': 'apiVersion' must be 'v1'.");
        else if (inst.IsWhisparr && version != "v3" && version != "v3-eros")
            errors.Add($"{prefix} '{inst.Name}': 'apiVersion' must be 'v3' or 'v3-eros'.");
    }

    private static bool IsApiVersion(InstanceConfig inst, string version) =>
        string.Equals(NormalizeApiVersion(inst.ApiVersion), version, StringComparison.Ordinal);

    private static string NormalizeApiVersion(string? version) =>
        version?.Trim().ToLowerInvariant() ?? string.Empty;

    private static bool IsValidLogLevel(string? level)
    {
        if (string.IsNullOrWhiteSpace(level)) return false;
        return level.Trim().Equals("debug", StringComparison.OrdinalIgnoreCase) ||
               level.Trim().Equals("info", StringComparison.OrdinalIgnoreCase) ||
               level.Trim().Equals("warning", StringComparison.OrdinalIgnoreCase) ||
               level.Trim().Equals("error", StringComparison.OrdinalIgnoreCase);
    }

    private static bool IsValidCron(string cron)
    {
        if (string.IsNullOrWhiteSpace(cron)) return false;
        var parts = cron.Split(' ', StringSplitOptions.RemoveEmptyEntries);
        return parts.Length == 5;
    }
}
