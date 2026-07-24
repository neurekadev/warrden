using wArrden.Configuration;
using wArrden.Services;

namespace wArrden.Tests;

public class OutputServiceTests
{
    private readonly StringWriter _writer;
    private readonly OutputService _output;

    public OutputServiceTests()
    {
        _writer = new StringWriter();
        _output = new OutputService { Out = _writer };
    }

    [Fact]
    public void WriteBanner_ContainsStartupAndReady()
    {
        var config = new AppConfig();
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("wArrden", output);
        Assert.Contains("system.startup", output);
        Assert.Contains("system.ready", output);
    }

    [Fact]
    public void WriteBanner_NoEnabledInstances_ShowsTheConfigWarning()
    {
        // End-to-end for the silent-idle case: validation records the warning and the startup
        // banner is what actually puts it in front of the operator.
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr",
                    Enabled = false,
                    Name = "Series",
                    Url = "http://localhost:8989",
                    ApiKey = "abc123",
                    ApiVersion = "v3",
                    QueueCleanup = new JobConfig { Enabled = true, Cron = "*/5 * * * *" }
                }
            }
        };
        YamlConfigLoader.Validate(config);

        OutputService.WriteBanner(config, new WardenOptions(), TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("warden.config", output);
        Assert.Contains("No instances are enabled", output);
        Assert.Contains("system.ready", output);
    }

    [Fact]
    public void WriteBanner_WithInstances_ShowsInstanceNames()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr", Name = "Series", Url = "http://localhost:8989",
                    ApiKey = "key", Enabled = true,
                    QueueCleanup = new JobConfig { Enabled = true, Cron = "*/5 * * * *" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("Series", output);
        Assert.Contains("http://localhost:8989", output);
        Assert.Contains("Queue Cleanup", output);
    }

    [Fact]
    public void WriteBanner_ShowsRuntimeSection()
    {
        var config = new AppConfig();
        var opts = new WardenOptions { DryRun = "true" };

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("Runtime", output);
        Assert.Contains("Timezone", output);
        Assert.Contains("Local Time", output);
        Assert.Contains("UTC Offset", output);
        Assert.Contains("Dry Run", output);
        Assert.Contains("true", output);
    }

    [Fact]
    public void WriteBanner_RuntimeAppearsBeforeInstances()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr", Name = "Series", Url = "http://localhost:8989",
                    ApiKey = "key", Enabled = true
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        var runtimeIndex = output.IndexOf("─ Runtime", StringComparison.Ordinal);
        var instanceIndex = output.IndexOf("Series (series)", StringComparison.Ordinal);
        var rulesIndex = output.IndexOf("Queue Cleanup Rules", StringComparison.Ordinal);

        Assert.True(runtimeIndex < instanceIndex, "Runtime section should appear before instance sections");
        Assert.True(instanceIndex < rulesIndex, "Instance sections should appear before Queue Cleanup Rules");
    }

    [Fact]
    public void WriteQueueResult_NoMatches_ShowsNoBlocked()
    {
        _output.WriteQueueResult("TestSonarr", 150, 0, 0,
            Array.Empty<(int, string, string, bool)>(), false);

        var output = _writer.ToString();
        Assert.Contains("Total Queue", output);
        Assert.Contains("150", output);
        Assert.Contains("No warning queue items detected", output);
    }

    [Fact]
    public void WriteQueueResult_HeaderUsesTimeOnlyTimestamp()
    {
        _output.WriteQueueResult("TestSonarr", 150, 0, 0,
            Array.Empty<(int, string, string, bool)>(), false);

        var header = _writer.ToString().Split(Environment.NewLine)[0];
        Assert.Matches(@"^\[\d{2}:\d{2}:\d{2} INFO\] \[testsonarr\.queue\]$", header);
    }

    [Fact]
    public void WriteQueueResult_WithMatches_Live()
    {
        var items = new List<(int, string, string, bool)>
        {
            (0, "The Boys (2019) - S01E01", "NOT_AN_UPGRADE", false)
        };

        _output.WriteQueueResult("TestSonarr", 150, 5, 1,
            items, false);

        var output = _writer.ToString();
        Assert.Contains("Warnings", output);
        Assert.Contains("5", output);
        Assert.Contains("Matched", output);
        Assert.Contains("Removed 1", output);
        Assert.Contains("The Boys", output);
        Assert.Contains("NOT_AN_UPGRADE", output);
    }

    [Fact]
    public void WriteClearCooldownsResult_SingleInstance()
    {
        _output.WriteClearCooldownsResult("cli.series.clear-missing", "Missing",
            new[] { ("Series", 1) });

        var output = _writer.ToString();
        var header = output.Split(Environment.NewLine)[0];
        Assert.Matches(@"^\[\d{2}:\d{2}:\d{2} INFO\] \[cli\.series\.clear-missing\]$", header);
        Assert.Contains("Type:       Missing", output);
        Assert.Contains("Cleared:    1 entry", output);
        Assert.DoesNotContain("/", header);
    }

    [Fact]
    public void WriteClearCooldownsResult_MultipleInstances()
    {
        _output.WriteClearCooldownsResult("cli.clear-upgrades", "Upgrade",
            new[] { ("Series", 2), ("Movies", 1) });

        var output = _writer.ToString();
        Assert.Contains("Type:       Upgrade", output);
        Assert.Contains("Series:     2 entries", output);
        Assert.Contains("Movies:     1 entry", output);
        Assert.Contains("Cleared:    3 entries", output);
    }

    [Fact]
    public void WriteQueueResult_WithMatches_DryRun()
    {
        var items = new List<(int, string, string, bool)>
        {
            (0, "Test Item", "SAMPLE", false)
        };

        _output.WriteQueueResult("TestSonarr", 150, 2, 1,
            items, true);

        var output = _writer.ToString();
        Assert.Contains("Would remove 1", output);
    }

    [Fact]
    public void WriteQueueResult_WithBlocklistMatches_Live()
    {
        var items = new List<(int, string, string, bool)>
        {
            (0, "The Boys (2019) - S01E01", "No files found are eligible", true)
        };

        _output.WriteQueueResult("TestSonarr", 150, 5, 1,
            items, false);

        var output = _writer.ToString();
        Assert.Contains("Blocklisted 1", output);
    }

    [Fact]
    public void WriteQueueResult_WithBlocklistMatches_DryRun()
    {
        var items = new List<(int, string, string, bool)>
        {
            (0, "Test Item", "No files found are eligible", true)
        };

        _output.WriteQueueResult("TestSonarr", 150, 2, 1,
            items, true);

        var output = _writer.ToString();
        Assert.Contains("Would blocklist 1", output);
    }

    [Fact]
    public void WriteQueueResult_WithMixedActions_ShowsBoth()
    {
        var items = new List<(int, string, string, bool)>
        {
            (0, "Item One", "NOT_AN_UPGRADE", false),
            (0, "Item Two", "No files found", true)
        };

        _output.WriteQueueResult("TestSonarr", 150, 5, 2,
            items, false);

        var output = _writer.ToString();
        Assert.Contains("Removed 1, Blocklisted 1", output);
    }

    [Fact]
    public void WriteQueueResult_Overflow_ShowsPlusMore()
    {
        var items = new List<(int, string, string, bool)>(); // empty items, but matched > 0

        _output.WriteQueueResult("TestSonarr", 150, 10, 5,
            items, false);

        var output = _writer.ToString();
        Assert.Contains("+5 more", output);
    }

    [Theory]
    [InlineData("Missing Search", "sonarr.missing")]
    [InlineData("Upgrade Search", "sonarr.upgrade")]
    [InlineData("Queue Cleanup", "sonarr.queue")]
    public void InstanceJobLabel_MapsCorrectly(string job, string expectedSuffix)
    {
        var writer = new OutputService.SearchOutputWriter(
            "Sonarr", job, 10, TimeZoneInfo.Utc, _writer);
        writer.WriteHeader();

        var output = _writer.ToString();
        Assert.Contains($"sonarr.{expectedSuffix.Split('.')[1]}", output);
    }

    [Fact]
    public void WriteStats_NoWantedItems_ShowsCorrectResult()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteStats(0, 0, 0, 0, true);

        var output = _writer.ToString();
        Assert.Contains("No wanted items found", output);
    }

    [Fact]
    public void WriteStats_NoSearchPerformed_ShowsCorrectResult()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteStats(5, 5, 0, 0, true);

        var output = _writer.ToString();
        Assert.Contains("No search performed", output);
    }

    [Fact]
    public void WriteStats_SearchedItems_ShowsCorrectResult()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteStats(8, 2, 6, 3, false);

        var output = _writer.ToString();
        Assert.Contains("Searched 3", output);
    }

    [Fact]
    public void WriteStats_IsLast_UsesFinalTreeConnector()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteStats(5, 0, 5, 3, true);

        var output = _writer.ToString();
        Assert.Contains("└─ Stats:", output);
    }

    [Fact]
    public void WriteStats_NotLast_UsesIntermediateTreeConnector()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteStats(5, 0, 5, 3, false);

        var output = _writer.ToString();
        Assert.Contains("├─ Stats:", output);
    }

    [Fact]
    public void WriteBanner_SonarrSearchJobs_ShowsSearchType()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr", Name = "Series", Url = "http://localhost:8989",
                    ApiKey = "key", Enabled = true,
                    MissingSearch = new JobConfig { Enabled = true, Cron = "*/5 * * * *", SearchType = "episode" },
                    UpgradeSearch = new JobConfig { Enabled = true, Cron = "*/10 * * * *", SearchType = "season" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("(episode)", output);
        Assert.Contains("(season)", output);
    }

    [Fact]
    public void WriteBanner_RadarrSearchJobs_DoesNotShowSearchType()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "radarr", Name = "Movies", Url = "http://localhost:7878",
                    ApiKey = "key", Enabled = true,
                    MissingSearch = new JobConfig { Enabled = true, Cron = "*/5 * * * *", SearchType = "episode" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.DoesNotContain("(episode)", output);
    }

    [Fact]
    public void WriteBanner_DisabledInstance_ShowsStatusAndAction_KeepsCrons()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr", Name = "Series", Url = "http://localhost:8989",
                    ApiKey = "key", Enabled = true,
                    MissingSearch = new JobConfig { Enabled = true, Cron = "*/5 * * * *" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer,
            disableReason: key => key == "series" ? "API key rejected (401 Unauthorized)" : null);

        var output = _writer.ToString();
        Assert.Contains("DISABLED — API key rejected (401 Unauthorized)", output);
        Assert.Contains("Fix the API key and restart wArrden", output);
        // Crons are kept, not hidden.
        Assert.Contains("*/5 * * * *", output);
    }

    [Fact]
    public void WriteBanner_EnabledInstance_ShowsNoDisabledStatus()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "sonarr", Name = "Series", Url = "http://localhost:8989",
                    ApiKey = "key", Enabled = true
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer,
            disableReason: _ => null);

        var output = _writer.ToString();
        Assert.DoesNotContain("DISABLED", output);
        Assert.DoesNotContain("Fix the API key and restart", output);
    }

    [Fact]
    public void SearchOutputWriter_SetPhase_WritesPhaseLine()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.SetPhase("Fetching wanted episodes");

        var output = _writer.ToString();
        Assert.Contains("├─ Fetching wanted episodes", output);
    }

    [Fact]
    public void SearchOutputWriter_WriteItem_WritesFormattedItem()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.StartResults();
        writer.WriteItem("The Boys (2019) - S01E01 - The Name of the Game");

        var output = _writer.ToString();
        Assert.Contains("• The Boys (2019) - S01E01 - The Name of the Game", output);
    }

    [Fact]
    public void SearchOutputWriter_StartResults_WritesResultsHeader()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.StartResults();

        var output = _writer.ToString();
        Assert.Contains("└─ Results:", output);
    }

    [Fact]
    public void SearchOutputWriter_WriteTrailer_WritesTrailerLine()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer);
        writer.WriteTrailer();

        var output = _writer.ToString();
        Assert.NotEmpty(output);
    }

    [Fact]
    public void WriteBanner_WhisparrSearchJobs_ShowsSearchType()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "whisparr", Name = "Adult", Url = "http://localhost:6969",
                    ApiKey = "key", Enabled = true,
                    MissingSearch = new JobConfig { Enabled = true, Cron = "*/5 * * * *", SearchType = "episode" },
                    UpgradeSearch = new JobConfig { Enabled = true, Cron = "*/10 * * * *", SearchType = "season" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("(episode)", output);
        Assert.Contains("(season)", output);
    }

    [Fact]
    public void WriteBanner_LidarrSearchJobs_ShowsSearchType()
    {
        var config = new AppConfig
        {
            Instances = new List<InstanceConfig>
            {
                new()
                {
                    Type = "lidarr", Name = "Music", Url = "http://localhost:8686",
                    ApiKey = "key", ApiVersion = "1", Enabled = true,
                    MissingSearch = new JobConfig { Enabled = true, Cron = "*/5 * * * *", SearchType = "album" },
                    UpgradeSearch = new JobConfig { Enabled = true, Cron = "*/10 * * * *", SearchType = "artist" }
                }
            }
        };
        var opts = new WardenOptions();

        OutputService.WriteBanner(config, opts, TimeZoneInfo.Utc, _writer);

        var output = _writer.ToString();
        Assert.Contains("(album)", output);
        Assert.Contains("(artist)", output);
    }

    [Fact]
    public void WriteDebug_MessageOnly_WritesFormattedDebugLine()
    {
        _output.MinimumLevel = LogLevel.Debug;
        _output.WriteDebug("warden.config", "Loaded config");

        var output = _writer.ToString();
        Assert.Contains("DEBUG", output);
        Assert.Contains("warden.config", output);
        Assert.Contains("└─ Loaded config", output);
    }

    [Fact]
    public void WriteDebug_WithDetail_WritesTreeStructure()
    {
        _output.MinimumLevel = LogLevel.Debug;
        _output.WriteDebug("series.missing", "Fetched 45 episodes", "12 on cooldown, 33 eligible");

        var output = _writer.ToString();
        Assert.Contains("DEBUG", output);
        Assert.Contains("series.missing", output);
        Assert.Contains("├─ Fetched 45 episodes", output);
        Assert.Contains("└─ 12 on cooldown, 33 eligible", output);
    }

    [Fact]
    public void WriteWarning_MessageOnly_WritesFormattedLine()
    {
        _output.WriteWarning("series.missing", "No enabled indexers");

        var output = _writer.ToString();
        Assert.Contains("WARN", output);
        Assert.Contains("series.missing", output);
        Assert.Contains("└─ No enabled indexers", output);
    }

    [Fact]
    public void WriteWarning_WithDetail_WritesTreeStructure()
    {
        _output.WriteWarning("series.missing", "Search trigger failed", "HttpRequestException: Connection refused");

        var output = _writer.ToString();
        Assert.Contains("WARN", output);
        Assert.Contains("series.missing", output);
        Assert.Contains("├─ Search trigger failed", output);
        Assert.Contains("└─ HttpRequestException: Connection refused", output);
    }

    [Fact]
    public void WriteError_MessageOnly_WritesFormattedLine()
    {
        _output.WriteError("warden.scheduler", "Task failed");

        var output = _writer.ToString();
        Assert.Contains("ERROR", output);
        Assert.Contains("warden.scheduler", output);
        Assert.Contains("└─ Task failed", output);
    }

    [Fact]
    public void WriteError_WithException_WritesExceptionDetail()
    {
        var ex = new InvalidOperationException("Unknown instance type: foo");

        _output.WriteError("warden.scheduler", "Scheduled task error", ex);

        var output = _writer.ToString();
        Assert.Contains("ERROR", output);
        Assert.Contains("warden.scheduler", output);
        Assert.Contains("├─ Scheduled task error", output);
        Assert.Contains("└─ InvalidOperationException: Unknown instance type: foo", output);
    }

    [Fact]
    public void WriteDebug_Suppressed_WhenMinimumIsInfo()
    {
        _output.MinimumLevel = LogLevel.Info;
        _output.WriteDebug("warden.core", "Should not appear");

        var output = _writer.ToString();
        Assert.Empty(output);
    }

    [Fact]
    public void WriteDebug_Shown_WhenMinimumIsDebug()
    {
        _output.MinimumLevel = LogLevel.Debug;
        _output.WriteDebug("warden.core", "Should appear");

        var output = _writer.ToString();
        Assert.Contains("DEBUG", output);
    }

    [Fact]
    public void WriteWarning_Suppressed_WhenMinimumIsError()
    {
        _output.MinimumLevel = LogLevel.Error;

        _output.WriteWarning("warden.core", "Should not appear");

        Assert.Empty(_writer.ToString());
    }

    [Fact]
    public void WriteError_Shown_WhenMinimumIsError()
    {
        _output.MinimumLevel = LogLevel.Error;

        _output.WriteError("warden.core", "Should appear");

        Assert.Contains("ERROR", _writer.ToString());
    }

    [Fact]
    public void WriteQueueResult_Suppressed_WhenMinimumIsWarning()
    {
        _output.MinimumLevel = LogLevel.Warning;
        _output.WriteQueueResult("TestSonarr", 150, 0, 0,
            Array.Empty<(int, string, string, bool)>(), false);

        var output = _writer.ToString();
        Assert.Empty(output);
    }

    [Fact]
    public void WriteQueueResult_Shown_WhenMinimumIsInfo()
    {
        _output.MinimumLevel = LogLevel.Info;
        _output.WriteQueueResult("TestSonarr", 150, 0, 0,
            Array.Empty<(int, string, string, bool)>(), false);

        var output = _writer.ToString();
        Assert.Contains("INFO", output);
    }

    [Fact]
    public void SearchOutputWriter_Suppressed_WhenShouldLogIsFalse()
    {
        var writer = new OutputService.SearchOutputWriter(
            "test", "Missing Search", 10, TimeZoneInfo.Utc, _writer, shouldLog: false);
        writer.WriteHeader();
        writer.SetPhase("Testing");
        writer.WriteStats(5, 0, 5, 3, true);
        writer.StartResults();
        writer.WriteItem("Test Item");
        writer.WriteTrailer();

        var output = _writer.ToString();
        Assert.Empty(output);
    }
}
