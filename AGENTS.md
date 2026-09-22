# AGENTS

## Purpose

wArrden is a Go scheduled job runner that searches for missing/upgrade items and cleans stuck queue entries on Sonarr, Radarr, Lidarr, and Whisparr instances. This file defines console log output formatting standards.

## Naming

The application is named **wArrden** (camelCase with capital `A`). It must **never** be referred to as "warden", "the warden", or "The warden" in any user-facing text: documentation, changelogs, commit messages, UI output, or log messages. The letter case must always be `wArrden`.

## Log Level & Output

Set via `logLevel` in `config.yaml`:

| `logLevel` | Messages Shown |
|---|---|
| `debug` | DEBUG + INFO + WARN + ERROR (most verbose) |
| `info` (default) | INFO + WARN + ERROR |
| `warning` | WARN + ERROR |
| `error` | ERROR only (least verbose) |

All log messages go through `internal/output.Writer` and its specialized writers to the single stdout-backed `io.Writer` supplied by the application. **Direct writes to `os.Stdout` or `os.Stderr`, including `fmt.Print*` calls, are forbidden for log output.** Containers (Docker, etc.) read stdout/stderr through separate pipes and merge them without preserving write order, which would cause tree-structured output to interleave. Writing everything through the synchronized output writer avoids this.

## Log Format Rules

### Shared Rules

- **Stats always come before Results** in all info-level job output.

### Header

Every log entry starts with a header line:

```
[HH:mm:ss LVL] [context]
```

Where:
- `HH:mm:ss` is the 24-hour timestamp of invocation
- `LVL` is the log level marker: `DEBUG`, `INFO`, `WARN`, or `ERROR`
- `context` is `instance.job` for job output, or a system context like `warden.config`, `warden.scheduler`, `cli`

### Tree Structure

All log output uses Unicode box-drawing characters to form a tree:

- ` ├─ ` — intermediate child (not the last)
- ` └─ ` — final child (last in the tree level)
- ` │  ` — vertical connector for nested content under intermediate parent
- `    ` — spacing for nested content under final parent
- ` • ` — bullet for list items

### Debug (DEBUG)

Single message:

```
[07:45:01 DEBUG] [warden.config]
 └─ Loaded config from data/config.yaml
```

With detail:

```
[07:45:01 DEBUG] [series.missing]
 ├─ Fetched 45 wanted episodes
 └─ Cooldown filter: 12 on cooldown, 33 eligible, 3 selected
```

### Warning (WARN)

```
[07:45:01 WARN] [series.missing]
 └─ No enabled indexers — search skipped

[07:45:01 WARN] [series.missing]
 ├─ Search trigger failed for The Boys (2019) - S01E01 - The Name of the Game
 └─ HttpRequestException: Connection refused (192.168.1.100:8989)
```

### Error (ERROR)

```
[07:45:01 ERROR] [series.missing]
 ├─ Missing search job failed
 └─ HttpRequestException: No route to host

[07:45:01 ERROR] [warden.scheduler]
 ├─ Scheduled task error
 └─ *errors.errorString: unknown instance type: unknown
```

### Info (INFO) — Search Jobs (missing / upgrade)

```
[07:45:01 INFO] [sonarr.missing]
 ├─ Cleaning cooldown entries
 ├─ Fetching wanted episodes
 ├─ Applying cooldown filters
 └─ Stats:
    • Total Items:   4
    • On Cooldown:   4
    • Eligible:      0
    • Search Limit:  2
    • Result:        No search performed
```

When items are searched, stats come before results:

```
[07:45:01 INFO] [sonarr.missing]
 ├─ Cleaning cooldown entries
 ├─ Fetching wanted episodes
 ├─ Applying cooldown filters
 ├─ Searching 3 items
 ├─ Stats:
 │  • Total Items:   10
 │  • On Cooldown:   2
 │  • Eligible:      8
 │  • Search Limit:  3
 │  • Result:        Searched 3
 └─ Results:
    • The Boys (2019) - S01E01 - The Name of the Game
    • The Boys (2019) - S01E02 - Cherry
    • Game of Thrones (2011) - S01E01 - Winter Is Coming
```

**Rules:**
- Full stats are shown even when no wanted items are found
- `Result` field: `"No wanted items found"` (0 total), `"No search performed"` (0 searched), or `"Searched N"`
- For Radarr, use `"Fetching wanted movies"` instead of `"Fetching wanted episodes"`

### Info (INFO) — Queue Cleanup Job (queue)

No warning-status items:

```
[07:45:01 INFO] [sonarr.queue]
 └─ Stats:
    • Total Queue:   150
    • Result:        No warning queue items detected
```

With `remove` action matches (stats before results):

```
[07:45:01 INFO] [sonarr.queue]
 ├─ Stats:
 │  • Total Queue:   150
 │  • Warnings:      5
 │  • Matched:       2
 │  • Result:        Removed 2
 └─ Results:
    • The Boys (2019) - S01E01 - The Name of the Game  NOT_QUALITY_UPGRADE
    • Game of Thrones (2011) - S02E03 - Valar Morghulis  SAMPLE
```

With `removeAndBlocklist` action matches:

```
[07:45:01 INFO] [sonarr.queue]
 ├─ Stats:
 │  • Total Queue:   80
 │  • Warnings:      8
 │  • Matched:       4
 │  • Result:        Blocklisted 4
 └─ Results:
    • Game of Thrones (2011) - S02E03 - Valar Morghulis  NO_FILES_ELIGIBLE
```

With mixed actions:

```
[07:45:01 INFO] [sonarr.queue]
 ├─ Stats:
 │  • Total Queue:   100
 │  • Warnings:      10
 │  • Matched:       5
 │  • Result:        Removed 3, Blocklisted 2
 └─ Results:
    • Euphoria.US.S01E02.1080p.AMZN.WEB-DL-Obfuscated  UNEXPECTED_ERROR
    • Game of Thrones (2011) - S02E03 - Valar Morghulis  NO_FILES_ELIGIBLE
    • The Boys (2019) - S01E01 - The Name of the Game  NOT_QUALITY_UPGRADE
```

**Rules:**
- `Warnings` is the count of queue items that present warnings or errors through `TrackedDownloadStatus == "warning"`, `statusMessages`, or `errorMessage`
- `Matched` is the subset of Warnings whose error messages or status messages matched a configured rule
- `Result` reflects the actions taken per match: `"Removed N"`, `"Blocklisted N"`, or `"Removed N, Blocklisted M"` for mixed actions
- Dry-run prefix: `"Would remove N"`, `"Would blocklist N"`, `"Would remove N, Would blocklist M"`
- With no warnings: `"No warning queue items detected"`

## Item Display Format

### Episodes

```
Series Name (Year) - S##E## - Episode Title
```

Examples:
- `The Boys (2019) - S01E01 - The Name of the Game`
- `Game of Thrones (2011) - S01E01 - Winter Is Coming`

Season and episode numbers are zero-padded to 2 digits (`%02d`). A hyphen and space separate the year from the season indicator.

### Movies

```
Movie Title (Year)
```

Examples:
- `Inception (2010)`
- `The Dark Knight (2008)`

Year is only appended when greater than zero.

## Source Files

- Log output: `internal/output/writer.go`, `internal/output/search.go`, `internal/output/results.go`, `internal/output/banner.go`
- Item title formatting: `internal/search/runner.go`, `internal/queue/cleaner.go`
- Queue cleanup matching and actions: `internal/matcher/registry.go`, `internal/config/types.go`, `internal/queue/cleaner.go`
- API client and models: `internal/arr/client.go`, `internal/arr/dto.go`, `internal/arr/types.go`
- Application wiring and scheduling: `internal/app/run.go`, `internal/schedule/scheduler.go`

## API Call Verification via OpenAPI Specs

When writing, modifying, or debugging API calls to any arr instance, verify the endpoint, parameters, request body, and response schema against the official OpenAPI spec for that arr.

### Source URLs

| Application | API Version | OpenAPI Spec URL |
|---|---|---|
| Radarr | v3 | `https://raw.githubusercontent.com/Radarr/Radarr/refs/heads/develop/src/Radarr.Api.V3/openapi.json` |
| Sonarr | v3 | `https://raw.githubusercontent.com/Sonarr/Sonarr/refs/heads/develop/src/Sonarr.Api.V3/openapi.json` |
| Lidarr | v1 | `https://raw.githubusercontent.com/Lidarr/Lidarr/refs/heads/develop/src/Lidarr.Api.V1/openapi.json` |
| Whisparr | v3 / v3-eros | `https://raw.githubusercontent.com/Whisparr/Whisparr/develop/src/Whisparr.Api.V3/openapi.json` |

Whisparr's hosted v3 API docs state that the v3 API docs apply to both v2 versions of Whisparr, with some functionality only available in v2. Use the `develop` OpenAPI spec above for both `v3` and `v3-eros`; branch-specific specs such as `v2-develop` or `eros` may be incomplete for this project’s supported endpoints.

### Workflow

1. Identify which arr and API version the API call targets.
2. If the OpenAPI spec content for that arr/version is not already in the current session context, fetch it from the URL above.
3. Verify the endpoint path, HTTP method, query parameters, request body schema, and response schema match the spec.
4. If the spec is unavailable or the API version differs from what the project expects, ask the user to provide the correct spec before proceeding.

Do not store the fetched OpenAPI specs in the repository; fetch them on demand per session as needed.

## Config Example

`config.example.yaml` is the authoritative reference for all configuration options. It must stay in sync with every code change that adds, removes, or renames a config field, matcher key, or action value.

### Content Rules

- `config.example.yaml` documents every valid configuration field, its type, default, and behavior through YAML comments.
- Every `queueCleanupRules` matcher entry must include a plain-language description and the full arr warning message(s) it targets.
- Every available matcher key is listed under each arr type it applies to, with one of three actions: `remove`, `removeAndBlocklist`, or `none`.
- Matcher keys that are listed but recommended as inactive ship with `action: none`.
- **Duplicate comments must stay identical.** When the same config key appears in multiple instance blocks (e.g. `type`, `cron`, `cooldown`), its comment text must match exactly across all copies. This ensures every instance block is fully self-documenting for copy-paste and prevents comment drift.

### Queue Cleanup Default Policy

Queue cleanup defaults favor aggressive, unattended automation. Classify every matcher by cause:

- Bad or unsuitable releases default to `removeAndBlocklist`.
- Valid but redundant releases default to `remove`: `NOT_QUALITY_UPGRADE`, `NOT_REVISION_UPGRADE`, `NOT_CUSTOM_FORMAT_UPGRADE`, `MOVIE_ALREADY_IMPORTED`, `EPISODE_ALREADY_IMPORTED`, and `ALBUM_ALREADY_IMPORTED`.
- Recoverable environment or metadata conditions default to `none`: `SAMPLE_INDETERMINATE`, `INSUFFICIENT_FREE_SPACE`, `FILE_UNPACKING`, `UNEXPECTED_ERROR`, `LOCKED_FILE`, `DOWNLOAD_CLIENT_ERROR`, and `IMPORT_PATH_INACCESSIBLE`.
- Sonarr and Whisparr also default `TITLE_MISSING`, `TITLE_TBA`, `MISSING_ABSOLUTE_NUMBER`, and `UNVERIFIED_SCENE_MAPPING` to `none`.
- Lidarr also defaults `DEST_FOLDER_NOT_ROOT` to `none`.
- All other applicable matcher keys default to `removeAndBlocklist`.

New matcher keys must be classified using this policy. Every default-policy change must update `config.example.yaml`, its YAML comments, `README.md` when user-facing behavior changes, and the default-action regression coverage in `internal/config/config_test.go`.

`SAMPLE_INDETERMINATE` must remain `none` unless an explicit policy decision changes it. Media-probing failures can affect healthy files, especially on remote mounts, and can also occur on local media.

### Comment Format

Use these YAML comment conventions to keep the config self-documenting:

**Simple key with enumerated choices:**
```yaml
# <description>
# Type: optional
# Default: <value>
# Values: <choice1> | <choice2> | ...
key: <default>
```

**Optional section (commented-out):**
```yaml
# <description>
# Type: optional
# Default: <what happens when unset>
# key:
#   - item
```

**Valid-choices reference:**
```yaml
# Actions: value1 | value2 | value3
```

**Matcher rule entry:**
```yaml
    # <plain-language description>
    # <ArrName>: "<exact warning message>"
    - match: KEY_NAME
      action: remove
```
