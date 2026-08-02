# Changelog

All notable changes to Seelex are documented in this file.

## [v0.1.0-alpha.3] - 2026-08-02

### Added

- Added subagent node detail page: click the `…` button on a plan node card to
  open a detail modal with event timeline (queued → running → completed with
  heartbeats), status, elapsed, and output.
- Added Dify-style graph semantics to the plan DSL: nodes embed downstream
  branch rows (target, condition labels, parallel-fork markers) instead of a
  flat nodelist + edgelist summary.
- Wired the subagent context loop into production: parent evidence injection
  (`SetNodeParentEvidence`) and post-execution merge-back
  (`SeelexAgentNode.mergeBack` → `merger.MergeBack` → parent session history);
  `seelexctx.ExportSnapshot` combines engine + trace providers.
- Split configuration: `seele.yaml` is permission-only (now actually loaded
  by `setupPermissionGate`, fallback to the built-in allowlist) and
  `seelex.yaml` carries window + limits runtime parameters.
- Added event-based process observation to the live smoke test
  (`smokeObserver` subscribes to the EventHub stream: tool start/complete,
  streamed output totals, errors).

### Changed

- Session resume loads only the tail history window (probe total via
  `ReadRange(limit<=0)` = manifest-only, then parse the covering shards)
  instead of full history; large-session switches parse 1-2 shards instead
  of all.
- `merger.MergeBack` now inherits the child Goal when the parent Goal is
  empty.

### Fixed

- Subagent merge-back loop was disconnected in production (parent evidence
  never injected, results never merged back); closed with end-to-end test
  `TestSubAgentMergeBackToParent`.

## [v0.1.0-alpha.2] - 2026-08-02

### Added

- Added tasklist gate: a loaded Plan renders as a frontend checklist, with
  `task_check_node` for in-progress per-node checkmarks as the agent moves
  between nodes (distinct from plan-run event-driven node checkmarks).
- Added `read_compressed_turn` read-back handle for compressed turn originals.
- Added windowed session-history reads (manifest shard counts) and parallel
  resume loading for faster large-session opens.
- Added TUI paste handling that submits the real pasted content.

### Changed

- Migrated the underlying kernel to Seele v0.0.8 (agent/session/tools/workplan
  components); local source reference via `replace` directive.
- Removed mandatory-plan gating; Plan is an optional tool with tasklist/plan
  execution modes chosen per task.
- Rewrote README for the new kernel; documented module navigation and storage
  model.
- Restructured context package into snapshot/provider/compactor/merger
  sub-packages with a sliding-window compaction model.
- Updated accounts configuration to role-based layout with flash-tier agents.

### Fixed

- Fixed context-compaction audit findings: cumulative compact boundaries,
  orphan retention, window rounds wiring, frame summarization, range checks.
- Fixed GUI copy/paste so pasted content is submitted instead of swallowed.
- Compressed-turn loss is now reversible through persisted originals.

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
