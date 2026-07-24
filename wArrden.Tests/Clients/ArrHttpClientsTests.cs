using Microsoft.Extensions.DependencyInjection;
using wArrden.Clients.Http;
using wArrden.Configuration;

namespace wArrden.Tests;

public class ArrHttpClientsTests
{
    private static InstanceConfig Instance(string name, bool? enabled) => new()
    {
        Type = "sonarr",
        Name = name,
        Enabled = enabled,
        Url = $"http://{name}.test:8989",
        ApiKey = $"{name}-key"
    };

    private static IHttpClientFactory BuildFactory(params InstanceConfig[] instances)
    {
        var config = new AppConfig { Instances = instances.ToList() };
        var services = new ServiceCollection();
        services.AddArrHttpClients(config, new WardenOptions());
        return services.BuildServiceProvider().GetRequiredService<IHttpClientFactory>();
    }

    [Fact]
    public void AllInstancesDisabled_StillRegistersHttpClientFactory()
    {
        // Regression: the factory used to be registered only as a side effect of the per-instance
        // AddHttpClient calls, so a config with every instance disabled left it unresolvable and
        // crashed the host on startup.
        var factory = BuildFactory(Instance("sonarr", false), Instance("radarr", false));

        Assert.NotNull(factory);
    }

    [Fact]
    public void NoInstances_StillRegistersHttpClientFactory()
    {
        var factory = BuildFactory();

        Assert.NotNull(factory);
    }

    [Fact]
    public void InstanceMissingEnabledFlag_StillRegistersHttpClientFactory()
    {
        var factory = BuildFactory(Instance("sonarr", null));

        Assert.NotNull(factory);
    }

    [Fact]
    public void EnabledInstance_ClientCarriesBaseAddressAndApiKey()
    {
        var factory = BuildFactory(Instance("sonarr", true));

        var client = factory.CreateClient("sonarr");

        Assert.Equal(new Uri("http://sonarr.test:8989/"), client.BaseAddress);
        Assert.Equal("sonarr-key", Assert.Single(client.DefaultRequestHeaders.GetValues("X-Api-Key")));
        // The resilience pipeline owns per-attempt timeouts, so the client itself must not time out.
        Assert.Equal(Timeout.InfiniteTimeSpan, client.Timeout);
    }

    [Fact]
    public void DisabledInstance_GetsNoConfiguredClient()
    {
        var factory = BuildFactory(Instance("sonarr", true), Instance("radarr", false));

        // Unregistered names fall back to an unconfigured client rather than throwing.
        Assert.Null(factory.CreateClient("radarr").BaseAddress);
        Assert.NotNull(factory.CreateClient("sonarr").BaseAddress);
    }
}
