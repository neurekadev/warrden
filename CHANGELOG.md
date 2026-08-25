# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [5.0.1] - 2026-08-25

### Fixed
- Separate confirmed samples from indeterminate media checks so transient rclone/FUSE or network storage failures do not remove or blocklist healthy downloads by default.

## [5.0.0] - 2026-08-25

### Changed
- Rewrite wArrden in Go and optimize its container runtime, reducing the image footprint by about 88% and measured memory usage by about 86%.
- Move official releases and multi-architecture container images to GitHub and `ghcr.io/neurekadev/warrden`, with verifiable build provenance.
- Update Compose deployments to use the v5 image, load runtime settings from `.env`, and mount `config.yaml` under `/app/data`.
- Use a 90-day upgrade-search cooldown in the example configuration instead of 30 days.
- Require `PUID` and `PGID` to be valid, supplied together, and either both zero or both non-zero.
- Expand telemetry to include anonymous lifecycle events and allow all telemetry to be disabled with `TELEMETRY=false`.

### Removed
- Remove the `APP_VERSION`, `CONFIG_PATH`, and `DATABASE_PATH` environment overrides; wArrden now reads its version from the image and keeps configuration and data under `/app/data`.
- Remove the bundled shell and .NET runtime from the container image.
- Reset saved 4.x search cooldown history during the first 5.0 startup because it cannot be carried into the new Go runtime.

### Fixed
- Restore automatic unexpected-error reporting through the current Beacon service.
- Run one scheduled job at a time so tree-structured log output cannot interleave.

## [4.6.0] - 2026-07-24

### Added
- Warn at startup when nothing will run, because no instance is enabled or because the enabled instances have no enabled jobs.

## [4.5.0] - 2026-07-24

### Changed
- Show a disabled instance in the startup summary alongside its configuration, with the reason it was disabled and what to do to re-enable it.
- Report an instance that cannot be reached at startup as a warning instead of an error, since its jobs keep retrying each cycle.

### Fixed
- Fix wArrden failing to start when every configured instance is disabled.

## [4.4.0] - 2026-07-23

### Added
- Retry transient arr request failures (gateway errors, timeouts, refused connections) with exponential backoff so brief outages recover on their own, tunable via the optional `HTTP_RETRY_COUNT` and `HTTP_TIMEOUT_SECONDS` environment variables.

### Changed
- Validate the API key against an authenticated endpoint at startup, and automatically disable an instance whose key is rejected — at startup or if it is rotated while wArrden is running — so its scheduled jobs stop instead of failing every run.
- Only report unexpected internal errors to Sentry; expected arr conditions (an unreachable instance, gateway errors, timeouts, or a bad API key) are now logged locally without being reported as errors.

## [4.3.1] - 2026-07-21

### Fixed
- Stop spurious `ObjectDisposedException` and canceled-request errors on shutdown by disposing arr clients only after in-flight scheduled jobs finish.

## [4.3.0] - 2026-07-21

### Added
- Report unexpected runtime and job errors to Sentry.

## [4.2.0] - 2026-07-20

### Changed
- Container image registry moved to `registry.neureka.dev` (from `code.neureka.dev`).
- Default the example Sonarr missing search to season.

### Fixed
- Dedupe paginated results from all arr clients so repeated records no longer crash the batched cooldown insert or truncate large fetches early.

## [4.1.4] - 2026-06-01

### Added
- Add MIT license information to the project.

### Fixed
- Prevent failed missing and upgrade searches from being put on cooldown or tagged as searched.
- Apply retroactive search tags to the correct enabled instance when disabled instances are present.
- Clear artist upgrade cooldowns when running the `clear-upgrades` command.
- Show upgrade search debug and warning logs as upgrade messages instead of missing-search messages.
- Honor configured log filtering and the documented 24-hour timestamp format for clear-cooldown command results.

## [4.1.3] - 2026-05-30

### Changed
- Halt startup validation immediately on connection failure or API key rejection instead of retrying, and log both cases as errors instead of warnings.

### Fixed
- Fix instances being skipped during scheduling when a previous instance has queue cleanup enabled but no matching rules.

## [4.1.2] - 2026-05-30

### Fixed
- Prevent instances from being permanently disabled when unreachable at startup. wArrden now retries connections with exponential backoff and recovers automatically once the service becomes reachable.

## [4.1.1] - 2026-05-28

### Changed
- Display runtime info first in the startup banner, before instance sections.

## [4.1.0] - 2026-05-28

### Added
- Display application version in the startup banner, read from the APP_VERSION environment variable.

## [4.0.2] - 2026-05-28

### Fixed
- Fix the startup banner to properly hide disabled instances and show disabled jobs.

## [4.0.1] - 2026-05-28

### Fixed
- Fix queue cleanup only fetching the first 10 download queue items instead of all items.

## [4.0.0] - 2026-05-28

### Added
- Add Whisparr Eros (v3-eros) support for missing and upgrade searches.
- Add optional tagging to search jobs, with a retroactive option to tag already-cooldowned items.
- Add DOWNLOAD_CLIENT_ERROR queue cleanup matcher for download client error states.

### Changed
- Require an API version declaration on each instance, such as v3, v1, or v3-eros.
- Require an `enabled` field on each instance to allow disabling an instance without removing it from config.
- Replace `indexerNames` with a flexible `indexerFilter` supporting include and exclude rules, where exclude takes priority over include.
- Batch search triggers into a single API command per job to reduce request load on arr instances.
- Report a hard error at startup when the config file contains unsupported keys.

### Fixed
- Fix queue cleanup sometimes missing warning items by broadening detection criteria and matching against status message titles.

## [3.1.0] - 2026-05-24

### Added
- Validate instance API keys at startup, with automatic disable of unreachable instances

### Changed
- Reduce memory allocations across HTTP clients, search, and queue cleanup paths

### Fixed
- Route all log output through OutputService to prevent stdout/stderr interleaving in containers
- Respect TZ environment variable for log timestamps, defaulting to UTC when unset
- Install tzdata package in Docker Alpine image for timezone support
- Restore invalid timezone warning display on startup
- Remove double line break after config warnings in startup banner
- Wrap SearchJob parameters to resolve dependency injection constructor ambiguity

## [3.0.0] - 2026-05-23

### Added
- Add named matcher keys for queue cleanup rules with configurable actions
- Add color-coded log output using ANSI escape codes

### Changed
- Rename log level markers from DBG/WRN/ERR to DEBUG/WARN/ERROR

### Fixed
- Fix crash when queue cleanup is scheduled for an instance type with no rules configured
- Fix log output appearing out of order in Docker due to stdout/stderr interleaving

## [2.1.3] - 2026-05-23

### Changed
- Switch Docker image to Alpine Linux for smaller image size

### Fixed
- Fix ARM64 Docker images not being built for ARM architecture

## [2.1.2] - 2026-05-22

### Added
- Add configurable log level (debug, info, warning, error) with tree-formatted console output

### Changed
- Rename queue cleanup Warnings label and distinguish remove vs blocklist actions in output

## [2.1.1] - 2026-05-22

### Fixed
- Fix Lidarr missing/upgrade search silently returning no results despite valid configuration
- Fix crash when optional configuration fields are left empty
- Fix indexer availability check using wrong search endpoint across all instance types

## [2.1.0] - 2026-05-21

### Added
- Add Lidarr and Whisparr support with queue cleanup rules
- Add PUID and PGID support for non-root container execution
- Add clear-missing and clear-upgrades CLI commands for cooldown management

## [2.0.1] - 2026-05-20

### Added
- Add ARM64 multi-arch support for Docker deployments

## [2.0.0] - 2026-05-20

### Added
- Add searchType configuration for episode and season searches on Sonarr instances
- Add per-instance indexer name filter for searches and upgrades
- Make queue cleanup warning matchers configurable through YAML configuration

### Changed
- Reject unknown YAML keys and missing required keys with startup validation errors

### Removed
- Remove legacy environment variable configuration (SONARR_*, RADARR_*), deprecated since 1.1.0

### Fixed
- Fix searches creating cooldown entries when no enabled indexers are available

## [1.1.0] - 2026-05-17

### Added
- YAML-based configuration supporting multiple named instances per arr type
- `CONFIG_PATH` environment variable for specifying a custom config file path
- Config file example with comprehensive instance configuration options

### Changed
- Configuration model from single-instance environment variables to multi-instance YAML

### Deprecated
- Legacy environment variable configuration (`SONARR_*`, `RADARR_*`); emits a warning on startup

## [1.0.0] - 2026-05-16

### Added
- Scheduled missing item search for Sonarr episodes and Radarr movies
- Scheduled upgrade search for finding better-quality versions of existing media
- Automatic queue cleanup to remove stuck or blocked imports
- Cooldown system to avoid re-searching the same items too frequently
- Support for both Radarr and Sonarr instances with independent configuration per job type
- Structured console output with item counts, cooldown status, and result summaries
[Unreleased]: https://github.com/neurekadev/warrden/compare/5.0.1...HEAD
[5.0.1]: https://github.com/neurekadev/warrden/compare/5.0.0...5.0.1
[5.0.0]: https://github.com/neurekadev/warrden/compare/4.6.0...5.0.0
[4.6.0]: https://github.com/neurekadev/warrden/compare/4.5.0...4.6.0
[4.5.0]: https://github.com/neurekadev/warrden/compare/4.4.0...4.5.0
[4.4.0]: https://github.com/neurekadev/warrden/compare/4.3.1...4.4.0

[4.3.1]: https://github.com/neurekadev/warrden/compare/4.3.0...4.3.1
[4.3.0]: https://github.com/neurekadev/warrden/compare/4.2.0...4.3.0
[4.2.0]: https://github.com/neurekadev/warrden/compare/4.1.4...4.2.0
[4.1.4]: https://github.com/neurekadev/warrden/compare/4.1.3...4.1.4
[4.1.3]: https://github.com/neurekadev/warrden/compare/4.1.2...4.1.3
[4.1.2]: https://github.com/neurekadev/warrden/compare/4.1.1...4.1.2
[4.1.1]: https://github.com/neurekadev/warrden/compare/4.1.0...4.1.1
[4.1.0]: https://github.com/neurekadev/warrden/compare/4.0.2...4.1.0
[4.0.2]: https://github.com/neurekadev/warrden/compare/4.0.1...4.0.2
[4.0.1]: https://github.com/neurekadev/warrden/compare/4.0.0...4.0.1
[4.0.0]: https://github.com/neurekadev/warrden/compare/3.1.0...4.0.0
[3.1.0]: https://github.com/neurekadev/warrden/compare/3.0.0...3.1.0
[3.0.0]: https://github.com/neurekadev/warrden/releases/tag/3.0.0
[2.1.3]: https://github.com/neurekadev/warrden/compare/2.1.3...3.0.0
[2.1.2]: https://github.com/neurekadev/warrden/releases/tag/2.1.2
[2.1.1]: https://github.com/neurekadev/warrden/releases/tag/2.1.1
[2.1.0]: https://github.com/neurekadev/warrden/releases/tag/2.1.0
[2.0.1]: https://github.com/neurekadev/warrden/releases/tag/2.0.1
[2.0.0]: https://github.com/neurekadev/warrden/releases/tag/2.0.0
[1.1.0]: https://github.com/neurekadev/warrden/releases/tag/1.1.0
[1.0.0]: https://github.com/neurekadev/warrden/releases/tag/1.0.0
