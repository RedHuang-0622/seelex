# Changelog

All notable changes to Seelex are documented in this file.

The repository is in Developer Alpha. Source builds report <code>dev</code>;
release builds receive their version from the Git tag through ldflags.

The next planned release is <code>v0.0.2</code>. The <code>v0.1.0</code>
line is reserved for the later breaking architectural rewrite and is not used
for this stabilization batch.

## [Unreleased]

### Added

- Queued input editing: while a turn is running, the GUI/TUI queue entries now
  offer reorder (move up / move down) and "recall to composer" actions. Recall
  pops the entry from the session queue (dropping its engine-side payload) and
  returns the original display text to the input box for editing; reordering
  changes the position only (`seq` identity is preserved) and both operations
  run under the same `Core.ViewMu` serialization as enqueue/promotion, so the
  running turn itself is never touched. New application API:
  `Service.ReorderQueuedInput` / `Service.RecallQueuedInput` (with explicit
  `ErrQueueNotRunning` / `ErrQueueIndexOutOfRange` / `ErrQueueSessionNotFound`
  semantics), bridged to the GUI as `Bridge.ReorderQueuedInput` /
  `Bridge.RecallQueuedInput`. The list rendered to the user and the list the edit
  operations act on are the same index space: `ChatState.InputQueue` /
  `QueuedCount` are derived from `session.SessionUnit.QueueProjection`, so the row
  the user points at is exactly the item that moves or is recalled (previously the
  projection filtered by payload type, so it could be shorter than the queue and
  the indices could target the wrong entry).
- Session titles are now persisted with the session head
  (`metadata/message.json` → `head.Meta.Summary`) and read back by project
  enumeration, so titles survive a restart instead of being guessed from session
  message bodies: refreshing the catalog for title-less sessions used to open
  every session's history window (`sessionNameFromTail`; measured 6 body reads for
  3 sessions) and now performs zero body reads. New store surface
  `SetSessionTitleWorkspace` / `SessionTitleWorkspace` (header-only: never opens a
  message shard) exposed to the application as the optional
  `session_runtime.SessionTitlePort`; `SetSessionTitleLocked` writes the title
  through on every change, the derived session record carries it, and it is
  preserved when a corrupted message head is rebuilt from its shards.
- Loaded session content is now LRU-managed as a second memory layer
  (`limits.loaded_content_limit`, default 12) on top of the resident engine
  bundle LRU (`resident_limit`): a session whose view is not switched to and
  which is not running (running / queued / awaiting approval / restoring are
  never candidates) releases its visible conversation window
  (`View.ContentUnloaded`; `application/core/content_lru.go`). Only the
  in-memory copy goes away — the window flags (total/offset/has-more), session
  facts, title and statistics stay, the session is flushed to disk before
  eviction, the left session list keeps its entry and title (the catalog
  refresh reads headers only, zero body reads), and re-activating the session or
  paging history cold-reloads the very same window
  (`ensureSessionContent` / `reloadSessionContent`).
- New `Bridge.SessionInputIndex(sessionID)` returns the **full-session** index of
  user inputs (round/seq + bounded summary + whether the entry is inside the
  currently loaded window). It is lightweight and body-free (derived from the
  durable session facts), so inputs that were never loaded into the renderer —
  and sessions that are not resident at all — are still indexed.

### Changed

- Chat errors now surface the raw `runChat` error text instead of the rewritten
  presentation copy. `Chat.Error`, the conversation `error` row, and the
  `EventError` payload all carry `err.Error()` verbatim, so a field failure can
  be diagnosed from the UI. The classified presentation (`presentedError`, and
  the `unclassified_error` diagnostic log line) still applies to the tool-error
  path (`presentToolError` → `ToolCall.Error`); raw error text stays view/event
  side and never enters provider context, because the transcript assembly only
  carries `user`/`assistant`/`tool` events.
- The conversation rail is now an index of **user inputs only** and covers the
  whole session: every tick is one user input taken from the authoritative
  full-session index (ticks for assistant steps / thinking / tools / system rows
  and the old "fall back to assistant steps when the window has no user turn"
  behaviour are gone), loaded inputs are positioned by measured geometry while
  unloaded ones are placed by a deterministic ratio, and clicking a tick that is
  not loaded yet read-backs that page first and only then scrolls and highlights
  (`planInputLocate` / `locateInput`); without a read-back channel the click only
  reports the situation instead of silently doing nothing. Keyboard navigation
  (↑/↓, PgUp/PgDn, Home/End) now applies to user-input ticks only.
- The work-table entry detail modal keeps exactly three authoritative surfaces —
  session transcript, context snapshot, and feature instrumentation — and is
  user-resizable with a resize handle (same interaction as the work table modal);
  the transcript panel auto-fits the host width (`width:100%` +
  `overflow-wrap:anywhere` + `min-width:0`), so long lines, code blocks, tables
  and URLs no longer overflow horizontally.
- The trajectory context axis can be paged: the wheel pages through the axis
  (accumulated steps per notch), `Shift`+wheel changes the page size in fixed
  steps with visible page/page-size feedback, and the paging window is computed
  by pure functions (`resolveAxisPage` / `axisPageWindow` / `axisPageForIndex`).
  Paging into history that is not loaded yet reports it and reuses the existing
  "load earlier" channel instead of silently jumping.
- `LoadMoreHistory` now pages by a full history window and persists the paging
  state in the session view (the Snapshot is a read-only mirror), and new
  `LoadLatestHistory` returns to the newest page after browsing earlier history.
- GUI history loading restores the reading position by anchoring the top visible
  message by key instead of by `scrollHeight` deltas, and disables CSS smooth
  scrolling for programmatic positioning.
- Reworked the repository entry documentation around verifiable Harness
  behavior, technical decisions, current limitations, and DeepSeek-compatible
  configuration.
- Started the engineering-trust remediation covering release consistency,
  error boundaries, concurrency, testing, and open-source governance.

### Fixed

- Rebuilt the trajectory multi-lane score so every block's lane and position has
  a single, explainable meaning: lane definitions and their order live in
  `AXIS_LANES` (one fixed lane per response kind), block width expresses the
  record's relative weight on the axis only, and lane assignment/positioning go
  through `axisBlocks` — the previous version mixed timeline-order positioning
  with size-proportional widths, which made the axis look arbitrary.
- Fixed paged history being wiped on the next session mirror: the visible window
  of a long session lost the page loaded by `LoadMoreHistory` and fell back to
  the tail window, so "load earlier" repeated the same page and reloaded the
  same messages.
- Fixed new messages re-anchoring the visible window to the tail while the user
  is reading earlier history (and appended streaming deltas landing on the wrong
  visible message); the window now stays anchored and the client reports that
  newer content is available below.
- Fixed the visible window of legacy sessions (no session record) reporting the
  loaded page size as the total message count, which made `has_more_history`
  permanently false and early history unreachable.
- Fixed the client reducer appending out-of-window messages while the window is
  anchored on earlier history, which inserted a gap between the window and the
  tail.
- Removed stale README claims about a local <code>go.work</code> and a
  <code>replace</code> directive. Seelex currently resolves Seele v0.1.1 from
  the Go module graph.
- Replaced the stale source release identifier with the neutral
  <code>dev</code> version. Tagged builds remain authoritative.

### Known issues

- Second round in an existing session can fail before any LLM request is sent,
  leaving only a visible error row. Recorded from the field on 2026-09-14;
  `repro_second_round_engine_released_test.go` reproduces two candidate shapes
  with the production assembly (session engine released after round one; round
  one closed by the terminal `task_needs_user_decision` tool) and neither trips
  the red condition today, so the real trigger shape is unconfirmed. The defect
  is shelved until it re-occurs; chat errors now surface the raw error text (see
  `Changed`), which is what the next field occurrence should be diagnosed from.
  The two shapes stay in the suite as regression guards.

## [v0.0.1_release] - 2026-08-03

This historical pre-release tag repackaged the v0.0.1 source state and added
the Windows GUI archive. Its name predates the SemVer validation now used for
new releases.

### Added

- Windows GUI package in addition to the cross-platform CLI archives.
- Subagent node detail view with queued/running/terminal event timeline.
- Dify-style Plan branch visualization.

## [v0.0.1] - 2026-08-03

First public Developer Alpha release.

### Added

- Bubble Tea TUI and opt-in Wails desktop GUI.
- Streaming Chat, Tool Calling, approval interactions, task terminal tools,
  Plugin/Skill/MCP switching, account selection, Effort levels, Plan state,
  paged history, and session resume.
- Optional WorkPlan DAG execution with isolated Subagent sessions, parallel
  branches, deterministic account routing, parent evidence injection, and
  child-to-parent merge-back.
- Context pipeline with Prompt Stack, sliding history window, compression,
  reversible compressed-turn archives, and immutable Tool Result references.
- Project-scoped file and shell tools, PathGate permission rules, and
  <code>manual</code> as the default permission mode.
- JSON, SQLite, PostgreSQL, and Redis session storage contracts.
- Tag-driven cross-platform release automation, SHA-256 checksums, CI, race
  tests, coverage artifacts, GUI protocol tests, and release safety checks.
- MIT License and a public account configuration template.

### Changed

- Migrated to the refactored Seele runtime modules and then pinned the public
  <code>github.com/RedHuang-0622/Seele v0.1.1</code> dependency.
- Made Plan optional: ordinary requests enter the main ReAct loop directly,
  while complex tasks may load a validated DAG.
- Split permission rules into <code>seele.yaml</code> and runtime window/limit
  parameters into <code>seelex.yaml</code>.
- Changed session resume to load only the required tail shards instead of the
  complete history.
- Changed release packaging to include only
  <code>config/accounts.example.yaml</code>; private account configuration is
  never part of a public build.

### Fixed

- Connected the production Subagent context loop: parent evidence is injected
  before execution and child findings/decisions/progress merge back afterward.
- Repaired context compaction boundaries, orphan retention, frame
  summarization, range checks, and reversible compressed-turn recovery.
- Fixed GUI and TUI paste handling, safe Markdown rendering, and streaming
  state synchronization.

### Known limitations

- The project is a Developer Alpha; CLI, configuration, and persistence
  contracts may still evolve.
- The GUI depends on the platform WebView and remains Alpha.
- Plan/Subagent orchestration is in-process and is not an A2A protocol
  implementation.
- Standardized coding benchmark results are not yet published.
