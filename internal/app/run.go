// Package app wires wArrden's process lifecycle from concrete capabilities.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/neurekadev/warrden/internal/arr"
	"github.com/neurekadev/warrden/internal/config"
	"github.com/neurekadev/warrden/internal/cooldown"
	"github.com/neurekadev/warrden/internal/health"
	"github.com/neurekadev/warrden/internal/identity"
	"github.com/neurekadev/warrden/internal/output"
	"github.com/neurekadev/warrden/internal/queue"
	"github.com/neurekadev/warrden/internal/schedule"
	"github.com/neurekadev/warrden/internal/search"
	"github.com/neurekadev/warrden/internal/tag"
	"github.com/neurekadev/warrden/internal/telemetry"
)

const telemetryShutdownTimeout = 3 * time.Second

type errorReporter interface {
	Capture(error, string, string)
	Flush(context.Context) bool
	Recover()
}

type lifecycleAnalytics interface {
	Start(context.Context)
	Stop(context.Context, string)
}

type outputDebugger interface {
	Debug(string, string, ...string)
}

type runDependencies struct {
	loadInstallID func(string) (string, error)
	newReporter   func(string, string, string) errorReporter
	newAnalytics  func(string, string, string, outputDebugger) lifecycleAnalytics
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		loadInstallID: telemetry.LoadOrCreateInstallID,
		newReporter: func(release, environment, installID string) errorReporter {
			return telemetry.New(release, environment, installID)
		},
		newAnalytics: func(installID, release, platform string, debug outputDebugger) lifecycleAnalytics {
			return telemetry.NewAnalytics(installID, release, platform, debug)
		},
	}
}

// Run executes wArrden and returns its process exit code.
func Run(ctx context.Context, args []string, stdout io.Writer) int {
	return run(ctx, args, stdout, defaultRunDependencies())
}

func run(ctx context.Context, args []string, stdout io.Writer, dependencies runDependencies) int {
	opts := config.OptionsFromEnv()
	earlyOutput := output.New(stdout, output.Error, time.UTC, nil)
	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			earlyOutput.ErrorText("cli", "Config file not found: "+configPath)
		} else {
			var configErr *config.Error
			if errors.As(err, &configErr) {
				for _, message := range configErr.Errors {
					earlyOutput.ErrorText("warden.config", message)
				}
			} else {
				earlyOutput.ErrorText("warden.config", err.Error())
			}
		}
		return 1
	}
	startupOutput := output.New(stdout, output.ParseLevel(cfg.LogLevel), time.UTC, nil)
	startupOutput.Debug("warden.config", "Loaded config from "+configPath)
	startupOutput.Debug("warden.config", "Log level set to "+cfg.LogLevel)

	location, timezoneWarning := resolveTimezone(opts.Timezone)
	runtimeWarnings := cfg.Warnings()
	if timezoneWarning != "" {
		runtimeWarnings = append(runtimeWarnings, timezoneWarning)
	}
	out := output.New(stdout, output.ParseLevel(cfg.LogLevel), location, nil)

	databasePath, err := filepath.Abs(databasePath)
	if err != nil {
		out.ErrorText("warden.config", "Invalid database path", err.Error())
		return 1
	}
	out.Debug("warden.config", "Database path: "+databasePath)
	if err := identity.Prepare(filepath.Dir(databasePath)); err != nil {
		out.ErrorText("warden.config", "Container identity setup failed", err.Error())
		return 1
	}
	legacyPath, err := filepath.Abs(legacyDatabasePath)
	if err != nil {
		out.ErrorText("warden.config", "Invalid legacy database path", err.Error())
		return 1
	}
	migrated, err := migrateLegacyDatabase(legacyPath, databasePath)
	if err != nil {
		out.ErrorText("warden.database", "Legacy database migration failed", err.Error())
		return 1
	}
	if migrated {
		out.Debug("warden.database", "Renamed legacy database to "+databasePath)
	}

	var analytics lifecycleAnalytics
	if opts.TelemetryEnabled {
		installPath, err := filepath.Abs(installIDPath)
		if err != nil {
			out.Warn("warden.telemetry", "Telemetry disabled — installation ID path is invalid", err.Error())
		}
		installID := ""
		if err == nil {
			installID, err = dependencies.loadInstallID(installPath)
			if err != nil {
				out.Warn("warden.telemetry", "Telemetry disabled — installation ID is unavailable", err.Error())
			}
		}
		environment := deploymentEnvironment(opts.AppVersion)
		reporter := dependencies.newReporter(opts.AppVersion, environment, installID)
		defer func() {
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			reporter.Flush(flushCtx)
		}()
		defer reporter.Recover()
		out = output.New(stdout, output.ParseLevel(cfg.LogLevel), location, reporter)
		analytics = dependencies.newAnalytics(installID, opts.AppVersion, runtime.GOOS+"-"+runtime.GOARCH, out)
	}

	args = aliasArgs(args)
	if len(args) > 0 {
		switch args[0] {
		case "clear-missing", "clear-upgrades":
			return runClear(ctx, args, cfg, databasePath, out)
		default:
			out.ErrorText("cli", "Unknown command: "+args[0])
			out.ErrorText("cli", "Available commands: clear-missing [instance], clear-upgrades [instance]")
			return 1
		}
	}
	return runDaemon(ctx, cfg, opts, databasePath, location, out, runtimeWarnings, analytics)
}

func runDaemon(ctx context.Context, cfg *config.Config, opts config.Options, databasePath string, location *time.Location, out *output.Writer, runtimeWarnings []string, analytics lifecycleAnalytics) (exitCode int) {
	if analytics != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryShutdownTimeout)
			defer cancel()
			reason := "shutdown"
			if exitCode != 0 {
				reason = "error"
			}
			analytics.Stop(shutdownCtx, reason)
		}()
	}
	store, reset, err := cooldown.Open(ctx, databasePath, out)
	if err != nil {
		out.ErrorText("warden.database", "Database initialization failed", err.Error())
		return 1
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			out.ErrorText("warden.database", "Database shutdown failed", closeErr.Error())
			exitCode = 1
		}
	}()
	if reset {
		out.Warn("warden.database", "Legacy cooldown database detected — cooldown history was reset")
	}

	tracker := health.New()
	httpClient := arr.NewHTTPClient()
	defer httpClient.CloseIdleConnections()
	clients := make(map[string]*arr.Client)
	for _, instance := range cfg.Instances {
		if !instance.Enabled {
			continue
		}
		clients[instance.Key()] = arr.NewClient(arr.ClientOptions{Instance: instance, RetryCount: opts.RetryCount, AttemptTimeout: opts.AttemptTimeout, HTTPClient: httpClient})
	}
	runtimeWarnings = append(runtimeWarnings, validateInstances(ctx, cfg, clients, tracker)...)

	tagger := tag.New(out, store)
	runner := search.New(store, out, tagger)
	scheduler, err := schedule.New(location)
	if err != nil {
		out.ErrorText("warden.scheduler", "Scheduler initialization failed", err.Error())
		return 1
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if shutdownErr := scheduler.Shutdown(shutdownCtx); shutdownErr != nil {
			out.ErrorText("warden.scheduler", "Scheduled task error", shutdownErr.Error())
			exitCode = 1
		}
	}()
	if err := addJobs(cfg, opts, clients, tracker, runner, out, scheduler); err != nil {
		out.ErrorText("warden.scheduler", "Scheduled task error", err.Error())
		return 1
	}

	out.Banner(cfg, opts, tracker.Reason, runtimeWarnings)
	runRetroactive(ctx, cfg, clients, tracker, tagger, out)
	scheduler.Start()
	if analytics != nil {
		analytics.Start(ctx)
	}
	<-ctx.Done()
	return 0
}

func deploymentEnvironment(release string) string {
	value := strings.ToLower(strings.TrimSpace(release))
	switch {
	case value == "" || value == "dev":
		return "development"
	case strings.HasPrefix(value, "edge-"):
		return "edge"
	default:
		return "production"
	}
}

func addJobs(cfg *config.Config, opts config.Options, clients map[string]*arr.Client, tracker *health.Tracker, runner *search.Runner, out *output.Writer, scheduler *schedule.Scheduler) error {
	for _, instance := range cfg.Instances {
		if !instance.Enabled {
			continue
		}
		client := clients[instance.Key()]
		out.Debug("warden.scheduler", fmt.Sprintf("Scheduling jobs for %s (%s)", instance.Name, instance.Kind))
		addSearch := func(name string, job *config.Job, missing bool) error {
			if job == nil || !job.Enabled {
				return nil
			}
			contextName := instance.Key() + "." + name
			return scheduler.Add(instance.Key()+"_"+name, job.Cron, func(ctx context.Context) error {
				if !tracker.Enabled(instance.Key()) {
					return nil
				}
				return runner.Run(ctx, client, instance, job, missing, opts.DryRun)
			}, func(err error) { reportFailure(out, tracker, instance, contextName, name+" search job failed", err) })
		}
		if err := addSearch("missing", instance.MissingSearch, true); err != nil {
			return err
		}
		if err := addSearch("upgrade", instance.UpgradeSearch, false); err != nil {
			return err
		}
		if instance.QueueCleanup != nil && instance.QueueCleanup.Enabled {
			rules := cfg.QueueRules.For(instance.Kind)
			if len(rules) == 0 {
				out.Warn(instance.Key()+".queue", "Queue cleanup is enabled but no rules are configured for this instance type — job will not be scheduled.")
				continue
			}
			cleaner := queue.New(client, instance.Kind, opts.DryRun, rules, out)
			if err := scheduler.Add(instance.Key()+"_queue", instance.QueueCleanup.Cron, func(ctx context.Context) error {
				if !tracker.Enabled(instance.Key()) {
					return nil
				}
				_, err := cleaner.Run(ctx)
				return err
			}, func(err error) {
				reportFailure(out, tracker, instance, instance.Key()+".queue", "Queue cleanup job failed", err)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateInstances(ctx context.Context, cfg *config.Config, clients map[string]*arr.Client, tracker *health.Tracker) []string {
	var wait sync.WaitGroup
	warnings := make([][]string, len(cfg.Instances))
	for index, instance := range cfg.Instances {
		if !instance.Enabled {
			continue
		}
		index := index
		instance := instance
		wait.Add(1)
		go func() {
			defer wait.Done()
			code, status, err := clients[instance.Key()].Validate(ctx)
			if err != nil {
				warnings[index] = []string{fmt.Sprintf("Could not connect to %s (%s) — jobs will retry each cycle", instance.Name, instance.URLText), arr.Detail(err)}
				return
			}
			if code >= 200 && code < 300 {
				return
			}
			if code == http.StatusUnauthorized || code == http.StatusForbidden {
				tracker.Disable(instance.Key(), fmt.Sprintf("API key rejected (%d %s)", code, status))
				return
			}
			warnings[index] = []string{fmt.Sprintf("Unexpected response from %s (%s): %d %s — jobs will retry each cycle", instance.Name, instance.URLText, code, status)}
		}()
	}
	wait.Wait()
	flattened := make([]string, 0)
	for _, instanceWarnings := range warnings {
		flattened = append(flattened, instanceWarnings...)
	}
	return flattened
}

func reportFailure(out *output.Writer, tracker *health.Tracker, instance config.Instance, contextName, message string, err error) {
	if arr.AuthFailure(err) {
		var httpErr *arr.HTTPError
		_ = errors.As(err, &httpErr)
		reason := "API key rejected"
		if httpErr != nil {
			reason = fmt.Sprintf("API key rejected (%d %s)", httpErr.StatusCode, httpErr.Status)
		}
		if tracker.Disable(instance.Key(), reason) {
			out.Warn(contextName, fmt.Sprintf("Instance %s disabled — %s; fix the API key and restart wArrden", instance.Name, reason), arr.Detail(err))
		}
		return
	}
	if arr.EnvironmentalFailure(err) {
		out.Warn(contextName, message, arr.Detail(err))
		return
	}
	out.Error(contextName, message, err)
}

func runClear(ctx context.Context, args []string, cfg *config.Config, databasePath string, out *output.Writer) (exitCode int) {
	targets := cfg.Instances
	if len(args) > 1 {
		targets = nil
		for _, instance := range cfg.Instances {
			if strings.EqualFold(instance.Name, args[1]) {
				targets = []config.Instance{instance}
				break
			}
		}
		if len(targets) == 0 {
			out.ErrorText("cli", "Unknown instance: "+args[1])
			names := make([]string, len(cfg.Instances))
			for index, instance := range cfg.Instances {
				names[index] = instance.Name
			}
			out.ErrorText("cli", "Available instances: "+strings.Join(names, ", "))
			return 1
		}
	}
	store, reset, err := cooldown.Open(ctx, databasePath, out)
	if err != nil {
		out.ErrorText("warden.database", "Database initialization failed", err.Error())
		return 1
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			out.ErrorText("warden.database", "Database shutdown failed", closeErr.Error())
			exitCode = 1
		}
	}()
	if reset {
		out.Warn("warden.database", "Legacy cooldown database detected — cooldown history was reset")
	}
	category, command := "Upgrade", "clear-upgrades"
	if args[0] == "clear-missing" {
		category, command = "Missing", "clear-missing"
	}
	counts := make([]output.ClearCount, 0, len(targets))
	for _, instance := range targets {
		count, err := store.Clear(ctx, category, instance.Name)
		if err != nil {
			out.ErrorText("cli", "Failed to clear cooldowns", err.Error())
			return 1
		}
		counts = append(counts, output.ClearCount{Instance: instance.Name, Count: count})
		out.Debug("cli."+strings.ToLower(targets[0].Name)+".clear", fmt.Sprintf("Cleared %d cooldown entries for %s", count, instance.Name))
	}
	label := "cli." + command
	if len(targets) == 1 {
		label = "cli." + strings.ToLower(targets[0].Name) + "." + command
	}
	out.ClearResult(label, category, counts)
	return 0
}

func aliasArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	base := strings.ToLower(filepath.Base(args[0]))
	if base == "clear-missing" || base == "clear-upgrades" {
		return append([]string{base}, args[1:]...)
	}
	return args[1:]
}

func resolveTimezone(raw string) (*time.Location, string) {
	if strings.TrimSpace(raw) == "" {
		return time.UTC, ""
	}
	lookup := strings.TrimPrefix(raw, ":")
	location, err := time.LoadLocation(lookup)
	if err != nil {
		return time.UTC, fmt.Sprintf("Invalid timezone '%s' — falling back to UTC: The time zone ID '%s' was not found on the local computer.", lookup, lookup)
	}
	return location, ""
}
