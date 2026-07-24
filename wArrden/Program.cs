using System.Net;
using wArrden.Clients;
using wArrden.Clients.Http;
using wArrden.Configuration;
using wArrden.Data;
using wArrden.Invocables;
using wArrden.Services;
using Coravel;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;
using Sentry;

var opts = new WardenOptions
{
    DryRun = GetEnv("DRY_RUN"),
    Timezone = GetEnv("TZ"),
    AppVersion = GetEnv("APP_VERSION"),
    DatabasePath = GetEnv("DATABASE_PATH") ?? "data/warden.db",
    HttpRetryCount = GetEnv("HTTP_RETRY_COUNT"),
    HttpTimeoutSeconds = GetEnv("HTTP_TIMEOUT_SECONDS")
};

try
{
    SentrySdk.Init(o =>
    {
        o.Dsn = "https://ca3bcb7569913062d079984cce25219e@beacon.neureka.dev/2";
        if (!string.IsNullOrWhiteSpace(opts.AppVersion))
            o.Release = opts.AppVersion;
    });
}
catch
{
    // Error reporting must never prevent wArrden from starting.
}

var configPath = GetEnv("CONFIG_PATH") ?? "data/config.yaml";

if (!File.Exists(configPath))
{
    new OutputService { MinimumLevel = wArrden.Services.LogLevel.Error }.WriteError("cli", $"Config file not found: {configPath}");
    Environment.Exit(1);
    return;
}

AppConfig config;
try
{
    config = YamlConfigLoader.Load(configPath);
}
catch (ConfigurationException ex)
{
    var errorOutput = new OutputService { MinimumLevel = wArrden.Services.LogLevel.Error };
    foreach (var error in ex.Errors)
        errorOutput.WriteError("warden.config", error);
    Environment.Exit(1);
    return;
}

var startupOutput = new OutputService { MinimumLevel = ParseLogLevel(config.LogLevel) };
var logLevelLabel = config.LogLevel ?? "info";
startupOutput.WriteDebug("warden.config", $"Loaded config from {configPath}");
startupOutput.WriteDebug("warden.config", $"Log level set to {logLevelLabel}");

var startupTimeZone = ResolveTimezone(opts.Timezone, out var tzWarning);
if (tzWarning is not null)
    config.AddWarning(tzWarning);
startupOutput.TimeZone = startupTimeZone;

var dbPath = Path.GetFullPath(opts.DatabasePath);
var dbDir = Path.GetDirectoryName(dbPath);
if (dbDir is not null)
    Directory.CreateDirectory(dbDir);

startupOutput.WriteDebug("warden.config", $"Database path: {dbPath}");

if (args.Length > 0)
{
    var command = args[0];

    if (command is "clear-missing" or "clear-upgrades")
    {
        var category = command == "clear-missing" ? "Missing" : "Upgrade";
        var targets = ResolveTargets(config, args.Length > 1 ? args[1] : null, startupOutput);
        if (targets is null) return;

        await RunClearCooldownsCommand(dbPath, category, targets, opts, startupOutput);
        return;
    }

    startupOutput.WriteError("cli", $"Unknown command: {args[0]}");
    startupOutput.WriteError("cli", "Available commands: clear-missing [instance], clear-upgrades [instance]");
    Environment.Exit(1);
    return;
}

var builder = Host.CreateApplicationBuilder(args);

builder.Logging.AddFilter("Microsoft", Microsoft.Extensions.Logging.LogLevel.Warning);
builder.Logging.AddFilter("Microsoft.EntityFrameworkCore", Microsoft.Extensions.Logging.LogLevel.Warning);
builder.Logging.AddFilter("System", Microsoft.Extensions.Logging.LogLevel.Warning);
builder.Logging.SetMinimumLevel(Microsoft.Extensions.Logging.LogLevel.Warning);

builder.Services.AddDbContextPool<WardenDbContext>(o => o.UseSqlite($"Data Source={dbPath}"));
builder.Services.AddSingleton(opts);
builder.Services.AddScheduler();
builder.Services.AddSingleton<ICooldownService, CooldownService>();
builder.Services.AddSingleton(new OutputService { MinimumLevel = ParseLogLevel(config.LogLevel), TimeZone = startupTimeZone });
builder.Services.AddSingleton<SearchService>();
builder.Services.AddSingleton<TaggingService>();
builder.Services.AddSingleton<InstanceHealthTracker>();

builder.Services.AddArrHttpClients(config, opts);

var host = builder.Build();

var httpClientFactory = host.Services.GetRequiredService<IHttpClientFactory>();
var healthTracker = host.Services.GetRequiredService<InstanceHealthTracker>();

IArrClient CreateArrClient(InstanceConfig inst)
{
    var http = httpClientFactory.CreateClient(inst.InstanceKey);
    return inst switch
    {
        { IsSonarr: true } => ArrClientFactory.CreateSonarr(http, inst.ApiVersion!, inst.Name),
        { IsRadarr: true } => ArrClientFactory.CreateRadarr(http, inst.ApiVersion!, inst.Name),
        { IsLidarr: true } => ArrClientFactory.CreateLidarr(http, inst.ApiVersion!, inst.Name),
        { IsWhisparr: true } => ArrClientFactory.CreateWhisparr(http, inst.ApiVersion!, inst.Name),
        _ => throw new InvalidOperationException($"Unknown instance type: {inst.Type}")
    };
}

using (var scope = host.Services.CreateScope())
{
    var db = scope.ServiceProvider.GetRequiredService<WardenDbContext>();
    await db.Database.EnsureCreatedAsync();
    await db.Database.ExecuteSqlRawAsync("PRAGMA journal_mode=WAL");
}

async Task ValidateInstanceAsync(InstanceConfig inst)
{
    using var client = CreateArrClient(inst);

    var (status, msg, detail) = await ValidateOnceAsync(client, inst.Name, inst.Url, CancellationToken.None);

    // A rejected API key is a misconfiguration, not a transient outage: disable the instance so
    // its scheduled jobs never run (and never flood) until the operator fixes the key and restarts.
    // The reason is stored on the tracker and surfaced inline in the startup banner.
    if (status == ValidationStatus.AuthFailed)
        healthTracker.Disable(inst.InstanceKey, msg ?? "API key rejected");
    else if (status == ValidationStatus.Unreachable && msg is not null)
        // Not an auth failure — the instance stays enabled and retries; report it as a warning.
        config.AddValidationWarning(msg, detail);
}

var validationTasks = new List<Task>();
foreach (var inst in config.Instances)
{
    if (inst.Enabled != true) continue;
    validationTasks.Add(ValidateInstanceAsync(inst));
}
await Task.WhenAll(validationTasks);

var schedulerOutput = host.Services.GetRequiredService<OutputService>();
var clients = new List<(InstanceConfig Instance, IArrClient Client)>(config.Instances.Count);

host.Services.UseScheduler(scheduler =>
{
    foreach (var inst in config.Instances)
    {
        if (inst.Enabled != true) continue;
        var client = CreateArrClient(inst);
        clients.Add((inst, client));

        var instanceKey = inst.InstanceKey;
        // Every scheduled job for this instance is gated on its health: once an instance is
        // disabled (bad API key at startup or a 401 mid-run), its jobs stop running.
        Func<Task<bool>> isInstanceEnabled = () => Task.FromResult(healthTracker.IsEnabled(instanceKey));
        var instanceType = InstanceType(inst);
        var searchInstanceType = SearchInstanceType(inst);

        schedulerOutput.WriteDebug("warden.scheduler", $"Scheduling jobs for {inst.Name} ({instanceType})");

        if (inst.MissingSearch?.Enabled == true)
        {
            scheduler
                .ScheduleWithParams<SearchJob>(new SearchJobParams(
                    client, "missing", searchInstanceType,
                    inst.MissingSearch.MaxResults!.Value, inst.MissingSearch.Cooldown!,
                    inst.MissingSearch.SearchType ?? "", opts.IsDryRun, inst.IndexerFilter,
                    inst.MissingSearch.Tagging, instanceKey
                ))
                .Cron(inst.MissingSearch.Cron!)
                .PreventOverlapping($"{instanceKey}_missing")
                .When(isInstanceEnabled);
        }

        if (inst.UpgradeSearch?.Enabled == true)
        {
            scheduler
                .ScheduleWithParams<SearchJob>(new SearchJobParams(
                    client, "upgrade", searchInstanceType,
                    inst.UpgradeSearch.MaxResults!.Value, inst.UpgradeSearch.Cooldown!,
                    inst.UpgradeSearch.SearchType ?? "", opts.IsDryRun, inst.IndexerFilter,
                    inst.UpgradeSearch.Tagging, instanceKey
                ))
                .Cron(inst.UpgradeSearch.Cron!)
                .PreventOverlapping($"{instanceKey}_upgrade")
                .When(isInstanceEnabled);
        }

        if (inst.QueueCleanup?.Enabled == true)
        {
            var rules = GetRulesForType(config.QueueCleanupRules, instanceType);
            if (rules is null)
            {
                schedulerOutput.WriteWarning($"{instanceKey}.queue", "Queue cleanup is enabled but no rules are configured for this instance type — job will not be scheduled.");
                continue;
            }

            scheduler
                .ScheduleWithParams<QueueJob>(new QueueJobParams(client, instanceType, opts.IsDryRun, rules, instanceKey))
                .Cron(inst.QueueCleanup.Cron!)
                .PreventOverlapping($"{instanceKey}_queue")
                .When(isInstanceEnabled);
        }
    }
})
.OnError(ex =>
{
    schedulerOutput.WriteError("warden.scheduler", "Scheduled task error", ex);
});

var lifetime = host.Services.GetRequiredService<IHostApplicationLifetime>();
lifetime.ApplicationStopped.Register(() =>
{
    foreach (var (_, client) in clients)
        client.Dispose();
});

OutputService.WriteBanner(config, opts, startupTimeZone, disableReason: healthTracker.GetDisableReason);

await RunRetroactiveTagging(
    clients.Where(c => healthTracker.IsEnabled(c.Instance.InstanceKey)).ToList(), host);

await host.RunAsync();

static string? GetEnv(string name) => Environment.GetEnvironmentVariable(name);

static wArrden.Services.LogLevel ParseLogLevel(string? level)
{
    if (string.IsNullOrWhiteSpace(level)) return wArrden.Services.LogLevel.Info;
    return level.Trim().Equals("debug", StringComparison.OrdinalIgnoreCase) ? wArrden.Services.LogLevel.Debug :
           level.Trim().Equals("warning", StringComparison.OrdinalIgnoreCase) ? wArrden.Services.LogLevel.Warning :
           level.Trim().Equals("error", StringComparison.OrdinalIgnoreCase) ? wArrden.Services.LogLevel.Error :
           wArrden.Services.LogLevel.Info;
}

static string InstanceType(InstanceConfig inst)
{
    if (inst.IsSonarr) return "sonarr";
    if (inst.IsRadarr) return "radarr";
    if (inst.IsLidarr) return "lidarr";
    if (inst.IsWhisparr) return "whisparr";
    throw new InvalidOperationException($"Unknown instance type: {inst.Type}");
}

static string SearchInstanceType(InstanceConfig inst)
{
    if (inst.IsWhisparrV3Eros) return "whisparr-eros";
    return InstanceType(inst);
}

static List<QueueCleanupRule>? GetRulesForType(QueueCleanupRulesConfig? config, string type)
{
    var list = type switch
    {
        "sonarr" => config?.Sonarr,
        "radarr" => config?.Radarr,
        "lidarr" => config?.Lidarr,
        "whisparr" => config?.Whisparr,
        _ => null
    };
    if (list is null || list.Count == 0) return null;
    return list
        .Where(r => !string.Equals(r.Action, "none", StringComparison.OrdinalIgnoreCase))
        .Select(r => new QueueCleanupRule(
            r.Match,
            string.Equals(r.Action, "removeAndBlocklist", StringComparison.OrdinalIgnoreCase)
        ))
        .ToList();
}

static List<InstanceConfig>? ResolveTargets(AppConfig config, string? instanceArg, OutputService output)
{
    if (instanceArg is null)
        return config.Instances;

    var match = config.Instances.FirstOrDefault(i =>
        string.Equals(i.Name, instanceArg, StringComparison.OrdinalIgnoreCase));
    if (match is not null)
        return new List<InstanceConfig> { match };

    output.WriteError("cli", $"Unknown instance: {instanceArg}");
    output.WriteError("cli", $"Available instances: {string.Join(", ", config.Instances.Select(i => i.Name))}");
    Environment.Exit(1);
    return null;
}

static async Task RunClearCooldownsCommand(string dbPath, string category, List<InstanceConfig> targets, WardenOptions opts, OutputService output)
{
    var services = new ServiceCollection();
    services.AddDbContextPool<WardenDbContext>(o => o.UseSqlite($"Data Source={dbPath}"));
    services.AddSingleton(output);
    services.AddSingleton<ICooldownService, CooldownService>();

    var sp = services.BuildServiceProvider();
    using var scope = sp.CreateScope();
    var db = scope.ServiceProvider.GetRequiredService<WardenDbContext>();
    await db.Database.ExecuteSqlRawAsync("PRAGMA journal_mode=WAL");

    var cooldown = scope.ServiceProvider.GetRequiredService<ICooldownService>();

    var counts = new List<(string Instance, int Count)>(targets.Count);
    foreach (var inst in targets)
    {
        var count = await cooldown.ClearAllAsync(category, inst.Name, CancellationToken.None);
        counts.Add((inst.Name, count));
        output.WriteDebug($"cli.{targets[0].Name.ToLowerInvariant()}.clear", $"Cleared {count} cooldown entries for {inst.Name}");
    }

    var isSingle = targets.Count == 1;
    var jobKey = category == "Missing" ? "clear-missing" : "clear-upgrades";
    var headerLabel = isSingle
        ? $"cli.{targets[0].Name.ToLowerInvariant()}.{jobKey}"
        : $"cli.{jobKey}";

    output.WriteClearCooldownsResult(headerLabel, category, counts);
}

static TimeZoneInfo ResolveTimezone(string? tzId, out string? warning)
{
    if (!string.IsNullOrWhiteSpace(tzId))
    {
        // Strip Docker-style colon prefix (e.g. ":America/Los_Angeles")
        if (tzId.StartsWith(':'))
            tzId = tzId[1..];

        try
        {
            warning = null;
            return TimeZoneInfo.FindSystemTimeZoneById(tzId);
        }
        catch (Exception ex)
        {
            warning = $"Invalid timezone '{tzId}' — falling back to UTC: {ex.Message}";
            return TimeZoneInfo.Utc;
        }
    }

    warning = null;
    return TimeZoneInfo.Utc;
}

static async Task RunRetroactiveTagging(List<(InstanceConfig Instance, IArrClient Client)> clients, IHost host)
{
    var tagging = host.Services.GetRequiredService<TaggingService>();
    var output = host.Services.GetRequiredService<OutputService>();

    foreach (var (inst, client) in clients)
    {
        var jobs = new (JobConfig? Job, string JobKey, string TagName)[]
        {
            (inst.MissingSearch, "missing", inst.MissingSearch?.Tagging?.Name ?? ""),
            (inst.UpgradeSearch, "upgrade", inst.UpgradeSearch?.Tagging?.Name ?? ""),
        };

        foreach (var (job, jobKey, tagName) in jobs)
        {
            if (job?.Tagging?.Enabled != true || job.Tagging?.Retroactive != true || string.IsNullOrWhiteSpace(tagName))
                continue;

            var context = $"{inst.InstanceKey}.retrotag_{jobKey}";
            output.WriteDebug(context, $"Retroactive tagging started for '{tagName}'");

            try
            {
                if (inst.IsSonarr || (inst.IsWhisparr && !inst.IsWhisparrV3Eros))
                {
                    var prefix = jobKey == "missing" ? "Missing" : "Upgrade";
                    await tagging.RunRetroactiveEpisodeAsync(client, inst.Name, prefix, tagName, CancellationToken.None);
                    await tagging.RunRetroactiveSeasonAsync(client, inst.Name, $"{prefix}_Season", tagName, CancellationToken.None);
                }
                else if (inst.IsRadarr || inst.IsWhisparrV3Eros)
                {
                    var prefix = jobKey == "missing" ? "Missing" : "Upgrade";
                    await tagging.RunRetroactiveMovieAsync(client, inst.Name, prefix, tagName, CancellationToken.None);
                }
                else if (inst.IsLidarr)
                {
                    var prefix = jobKey == "missing" ? "Missing" : "Upgrade";
                    await tagging.RunRetroactiveAlbumAsync(client, inst.Name, prefix, tagName, CancellationToken.None);
                    await tagging.RunRetroactiveArtistAsync(client, inst.Name, $"{prefix}_Artist", tagName, CancellationToken.None);
                }
            }
            catch (Exception ex)
            {
                output.WriteWarning(context,
                    $"Retroactive tagging failed for '{tagName}'",
                    $"{ex.GetType().Name}: {ex.Message}");
            }
        }
    }
}

static async Task<(ValidationStatus Status, string? Message, string? Detail)> ValidateOnceAsync(
    IArrClient client, string instanceName, string instanceUrl,
    CancellationToken ct)
{
    try
    {
        var code = await client.ValidateApiKeyAsync(ct);

        if ((int)code is >= 200 and < 300)
            return (ValidationStatus.Ok, null, null);

        // A rejected key is a misconfiguration — disable the instance and surface the exact cause.
        if (code is HttpStatusCode.Unauthorized or HttpStatusCode.Forbidden)
            return (ValidationStatus.AuthFailed, $"API key rejected ({(int)code} {code})", null);

        // Any other non-2xx isn't an auth problem: leave the instance enabled to retry, but warn.
        return (ValidationStatus.Unreachable,
            $"Unexpected response from {instanceName} ({instanceUrl}): {(int)code} {code} — jobs will retry each cycle",
            null);
    }
    catch (OperationCanceledException) when (ct.IsCancellationRequested)
    {
        throw;
    }
    catch (Exception ex)
    {
        return (ValidationStatus.Unreachable,
            $"Could not connect to {instanceName} ({instanceUrl}) — jobs will retry each cycle",
            $"{ex.GetType().Name}: {ex.Message}");
    }
}
