<div align="center">

<img src="Logo.png" alt="Logo" height="128px" />

# wArrden

[![Release](https://img.shields.io/github/v/release/neurekadev/warrden?style=flat-square&label=Release&color=F43F5E&logo=github&logoColor=F43F5E)](https://github.com/neurekadev/warrden/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/neurekadev/warrden/ci.yml?branch=main&style=flat-square&label=CI&color=8B5CF6&logo=githubactions&logoColor=8B5CF6)](https://github.com/neurekadev/warrden/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/neurekadev/warrden?style=flat-square&label=License&color=14B8A6&logo=opensourceinitiative&logoColor=14B8A6)](./LICENSE.md)
[![AI](https://img.shields.io/badge/AI-assisted-5786FE?style=flat-square&logo=deepseek&logoColor=5786FE)](https://github.com/neurekadev/warrden)
[![Stars](https://img.shields.io/github/stars/neurekadev/warrden?style=flat-square&label=Stars&color=EAB308&logo=googlegemini&logoColor=EAB308)](https://github.com/neurekadev/warrden)

wArrden makes it easy to maintain your media libraries by finding missing or upgradeable content, as well as detecting and clearing stuck imports from supported arr queues.

</div>

> [!CAUTION]
> Images at `registry.neureka.dev/warrden/warrden` are no longer updated. Use `ghcr.io/neurekadev/warrden`.

## Quickstart

> [!TIP]
> The `config.example.yaml` file can look overwhelming, but you don't need to understand every option to get started. Just add your arr URL and API key, then enable the instance — the defaults handle the rest.

1. Download `compose.yaml` and the environment template:

   ```
   curl -O https://raw.githubusercontent.com/neurekadev/warrden/refs/heads/main/compose.yaml
   curl -o .env https://raw.githubusercontent.com/neurekadev/warrden/refs/heads/main/.env.example
   ```

2. Download the example config as `config.yaml`, then edit it with your Sonarr/Radarr URLs and API keys:

   ```
   curl -o config.yaml https://raw.githubusercontent.com/neurekadev/warrden/refs/heads/main/config.example.yaml
   ```

3. Start the container:
   ```
   docker compose up -d
   ```

## CLI Usage

| Command                                         | Description                                                                                 |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `docker exec warrden clear-missing [instance]`  | Clears all missing search cooldowns. If `instance` is omitted, clears across all instances. |
| `docker exec warrden clear-upgrades [instance]` | Clears all upgrade search cooldowns. If `instance` is omitted, clears across all instances. |

## Features

wArrden supports multiple instances of each arr type, so you can manage separate libraries (movies, series, anime, music) independently with their own schedules and cooldowns.

| Supported |       API       | Queue Cleanup | Missing Search | Upgrade Search |
| --------- | :-------------: | :-----------: | :------------: | :------------: |
| Radarr    |      `v3`       |      ✔️       |       ✔️       |       ✔️       |
| Sonarr    |      `v3`       |      ✔️       |       ✔️       |       ✔️       |
| Lidarr    |      `v1`       |      ✔️       |       ✔️       |       ✔️       |
| Whisparr  | `v3`, `v3-eros` |      ✔️       |       ✔️       |       ✔️       |

## Missing Search

Periodically searches for monitored content that has never been downloaded. Limits the number of searches per run and ensures the same item isn't re-searched too often.

## Upgrade Search

Periodically searches for monitored content that already exists on disk but has a better custom format score. Uses the same limits and cooldown behavior as Missing Search.

## Queue Cleanup

Detects stuck imports caused by common errors (wrong episode, not an upgrade, sample, corrupt file) and removes or blocklists them so the same release won't download again.

## Telemetry

wArrden reports unexpected application errors and anonymous installation lifecycle
events to the project's Beacon service. A random installation UUID is generated once
in `data/install-id` and reused after container recreation; Beacon hashes that ID during
analytics ingestion. Install analytics contain only lifecycle event names, timestamps,
the wArrden release, the runtime platform, and clean-shutdown duration and reason.

## Why Use wArrden?

Radarr and Sonarr primarily rely on RSS feeds to detect newly uploaded releases every 15 minutes. While this works well for new uploads, many users assume it also continuously searches and reevaluates their entire library — it does not.

This creates a few common gaps in automation:

- If your server, indexers, or download clients are offline for a period of time, releases uploaded during that window may be missed entirely.
- Adding new indexers later does not retroactively search for older missing or upgradeable content. Only newly uploaded releases seen through RSS are picked up.
- Changes to Custom Formats (CFs) or scoring rules do not trigger automatic upgrades for existing media. Improved scoring only applies to future RSS releases.

Over time, this can leave libraries with permanently missing content or media that no longer matches your preferred quality and scoring standards.

wArrden fills those gaps by periodically rechecking your library and automating the cleanup work that would otherwise require manual intervention.
