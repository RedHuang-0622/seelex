# Changelog

All notable changes to Seelex are documented in this file.

## [v0.1.0-alpha.1] - 2026-07-23

### Added

- Added an opt-in Wails desktop GUI alongside the existing Bubble Tea TUI.
- Added structured GUI support for chat streaming, tool calls, approvals,
  plugins, accounts, effort levels, plans, skills, and paged history.
- Added a tracked `config/accounts.example.yaml` for clean installations.
- Added tag-driven release automation and SHA-256 checksums.
- Added the MIT license.
- Added safe session resume with full history replacement and selected-session
  persistence routing.
- Added safe GUI Markdown rendering, collapsible `<think>` reasoning blocks,
  runtime activity animation, and visible queued follow-up messages.

### Changed

- Changed the default permission mode from `full_access` to `manual`.
- Added `-frontend tui|gui` and `-version` command-line flags.
- Unified source and linker-injected version information.
- Release packages now copy only the public account example and never copy the
  developer's local `config` directory.
- External LLM smoke tests now require `SEELEX_RUN_LLM_SMOKE=1` explicitly.

### Known limitations

- The GUI is an Alpha and currently uses the platform WebView; the TUI remains
  the default frontend.
- CAD and Dev professional-plugin end-to-end workflows are not yet release
  acceptance gates.
