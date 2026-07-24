using Microsoft.Extensions.DependencyInjection;
using wArrden.Configuration;

namespace wArrden.Clients.Http;

/// <summary>
/// Registers one resilient named HttpClient per enabled instance, keyed by InstanceKey. Each
/// carries the instance's base address + API key, an infinite HttpClient.Timeout (the resilience
/// pipeline owns per-attempt timeouts), and shared retry/backoff for transient arr failures.
/// </summary>
public static class ArrHttpClients
{
    public static IServiceCollection AddArrHttpClients(
        this IServiceCollection services, AppConfig config, WardenOptions opts)
    {
        // Registered unconditionally: the per-instance calls below are the only other thing that
        // registers IHttpClientFactory, so a config with every instance disabled would otherwise
        // leave it unresolvable and crash the host on startup.
        services.AddHttpClient();

        foreach (var inst in config.Instances)
        {
            if (inst.Enabled != true) continue;

            var apiKey = inst.ApiKey;
            var baseAddress = new Uri(inst.Url.TrimEnd('/') + "/");

            services.AddHttpClient(inst.InstanceKey, http =>
                {
                    http.BaseAddress = baseAddress;
                    http.DefaultRequestHeaders.Add("X-Api-Key", apiKey);
                    http.Timeout = Timeout.InfiniteTimeSpan;
                })
                .ConfigurePrimaryHttpMessageHandler(() =>
                    new SocketsHttpHandler { PooledConnectionLifetime = TimeSpan.FromMinutes(15) })
                .AddArrResilience(opts.HttpRetryCountValue, TimeSpan.FromSeconds(opts.HttpTimeoutSecondsValue));
        }

        return services;
    }
}
