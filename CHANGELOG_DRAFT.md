# Changelog Draft

## Unreleased

### Added

- Added a local non-decrypting TCP tunnel mode for selected hostnames.
- Added Windows administrator checks and elevation-aware behavior for `hosts` updates and tunnel routing.
- Added automatic `hosts` backups:
  - `hosts.game-dl-tool.original.bak`
  - `hosts.game-dl-tool.bak`
- Added detailed manual trace output in `Details`, including:
  - hop list
  - reverse DNS hostname lookup
  - raw trace output
- Added on-demand GeoIP / network lookup for `Details` and `Trace`.
- Added `ipwho.is` as the default GeoIP provider.
- Added `IPinfo Lite` as an optional fallback GeoIP provider.
- Added local GeoIP result caching in `geoip_cache.json`.
- Added Girls' Frontline 2: Exilium CN default target:
  - `gf2-cn.cdn.sunborngame.com`
- Added grouped game config support for multi-CDN downloaders.
- Added per-game `preferred_provider` selection for grouped download targets.
- Added strict download-provider routing:
  - `auto` keeps all managed download providers available
  - a specific provider keeps only that provider active and blocks the others

### Changed

- Replaced the Fyne UI with a Wails GUI-first application.
- Reworked navigation into a three-step flow: `Domains -> Candidates -> Details`.
- Removed the old CLI fallback and made the app GUI-only.
- Changed candidate selection behavior so selections persist across domain pages and can be applied in batch.
- Changed scan behavior to always run a live scan and refresh `scan_cache.json`.
- Changed trace behavior so trace is manual only from `Details`; scans no longer run trace in the background.
- Changed settings handling to be fully config-driven from `config.json`.
- Changed the game catalog format from legacy game-selection strings and flat domain lists to named game entries with one-to-many domain mapping.
- Changed the game catalog format further to support grouped managed domains and provider-aware download targets.
- Changed the DNS resolver configuration to be editable in the GUI and in `config.json`.
- Changed tunnel behavior to use a random internal listener by default while loopback interception still enters through `443`.
- Changed direct `hosts` mode and tunnel loopback mode to be mutually exclusive per hostname.
- Changed runtime logging from `cache/latest_scan.log` to root-level `latest_scan.log`.
- Changed packaging from direct `go build` output to a Wails-based Windows build flow.
- Changed release packaging to output only:
  - `game-dl-tool.exe`
  - `README.md`
- Changed log and cache file placement to use root-level runtime files instead of writing logs into `cache/`.
- Changed the built-in Wuthering Waves CN target set to focus on download/CDN hostnames only, leaving telemetry, anti-cheat, SDK, and reporting domains outside the managed routing path.
- Changed grouped provider selection in settings from an inline dropdown to a per-game detail dialog.

### Fixed

- Fixed command prompt flashing caused by background `netsh` / `tracert` commands by running them without a visible console window.
- Fixed settings overlay stacking and scroll issues in the GUI.
- Fixed selection scope so batch actions can span multiple domains instead of only the current domain page.
- Fixed missing-cache behavior so absent `scan_cache.json` no longer raises an error.
- Fixed several unused functions / parameters and cleaned up stale code paths left from the previous CLI/Fyne implementation.
- Fixed a settings-page crash caused by incomplete game/provider data coming back from the backend.

### Removed

- Removed the old Fyne GUI implementation and its related theme/resource files.
- Removed the old CLI interactive host-writing flow.
- Removed the `trace while scan` setting.
- Removed the `use_cache` setting.
- Removed several unused helper functions and legacy paths no longer used by the Wails application.

### Notes

- Existing config files that still contain older fields such as `trace` or `use_cache` are tolerated, but those fields are now ignored.
- `IPinfo Lite` fallback requires a token if enabled.
- GeoIP lookups are intentionally not part of the scan path; they only run when a user opens `Details` or starts `Trace`.
- For grouped multi-CDN games, only domains inside `groups[].mode = "manage"` are affected by strict provider routing and blocking.
