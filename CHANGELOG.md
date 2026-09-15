# Changelog

All notable changes to Seelex are documented in this file.

The repository is in Developer Alpha. Source builds report <code>dev</code>;
release builds receive their version from the Git tag through ldflags.

The next planned release is <code>v0.0.2</code>. The <code>v0.1.0</code>
line is reserved for the later breaking architectural rewrite and is not used
for this stabilization batch.

## [Unreleased]

### Fixed

- **Compressed-turn archiving now actually persists** (was silently unwired).
  `CompressedTurnArchiver.StoreTurn` asserts a commit-write port on the object
  injected at assembly; the wiring passed `*session.Manager`, which never had
  that method, so every compression returned "durable commit storage is
  unavailable" and the original turns of out-of-window rounds were never
  written — `read_compressed_turn` degraded to an unrecoverable summary. The
  write channel is now workspace-explicit
  (`SaveCommitWorkspace(projectID, sessionID, commit)`, implemented by
  `session.Manager` on top of `Router.SaveCommitWorkspace`) and resolves the
  project by the session's own binding first, falling back to the view
  workspace — the same key the read path uses
  (`LoadToolResultWorkspace(workspaceID, sessionID, ref)`), so the archived ref
  is both writable and readable. Regression coverage: a real-store round trip
  through `Manager` → `Router.LoadToolResultWorkspace`, plus scope/attribution
  assertions on the archiver.

### Changed

- **GUI de-decoration batch**: the conversation no longer tints whole message
  blocks by speaker (EXEC / ADVISOR keep only a left status rule plus the
  speaker name), the shell drops its radial glow for a single `--bg`, and the
  right-rail tab strip loses its decorative gradient. Panel collapse controls
  moved from the topbar into each rail's own header row as `chevron-left` /
  `chevron-right` icons; a collapsed rail keeps a 26px spine with the toggle
  still reachable (Ctrl+B / Ctrl+J and the stored collapsed state unchanged).
  Ad-hoc text glyphs (session row `⋯ ★ ☆ ⑂ ✕`, account marks `● ○`, the
  full-access `✓`, the team drag handle and panel close) are now inline SVG
  icons from the shared registry.
- **Agent Team panel restructure**: the employee library is a standalone
  global block that no longer requires a selected team — employees can be
  created / edited / deleted there directly (`AgentTeamSaveEmployee` /
  `AgentTeamDeleteEmployee`), while teams only answer "who is assembled, in
  what order, who is called". The in-session roster keeps drag-only ordering
  (the `↑` / `↓` buttons are gone; `nextAgentTeamOrder` now only handles
  detach / restore) and the hire / team editors render itemised, numbered
  fields grouped into identity / orchestration / capability / prompt
  sections. The accounts panel is a provider → model cascade (pick the
  provider, then its models) with unchanged `SelectAccount` semantics.
- **Prompt assembly follows the Claude prompt-guideline XML convention**:
  `PromptStack.Render` wraps each layer as `<kind name="…">` instead of bare
  `---` separators, and the seelebridge prompt surface (ADVISOR role prompt +
  output contract, the reviewer context, and the bounded "optimise prompt"
  meta-prompt) is sectioned with `<role>` / `<task>` / `<constraints>` /
  `<output_contract>` / `<rewrite_checklist>` / `<output_format>` tags. The
  JSON contracts parsed by the goal domain and the optimisation result are
  unchanged.

### Changed

- Agent Team libraries are now **global**, not project-scoped. The team
  library (`<root>/team/library.json`), a new employee library
  (`<root>/team/employees.json`) and a new default order
  (`<root>/team/order.json`) sit at the data root; sessions read a deep copy
  (their own roster + lifecycle order) and only write the master back through
  an explicit "confirm · publish loadout to global" action. The legacy
  project-level team library is read through (read-only) when the global one is
  missing, and its entries merge into the global library on the first write —
  old files are never moved or deleted.

### Added

- Global Agent Team master surface: `AgentTeamGlobalConfig`,
  `AgentTeamSaveEmployee`, `AgentTeamDeleteEmployee`,
  `AgentTeamSetDefaultOrder` and `AgentTeamPublishToGlobal` (Bridge + GUI
  "全局母本" panel with the employee library, the default order, a
  copy-vs-master drift hint and the publish action).
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
- Agent Team members can be instantiated in one step and the speaking order is now
  observable. `team.instantiate_role` (`Service`/`Bridge.AgentTeamInstantiateRole`,
  `dto.RoleInstantiation`) materializes a role session idempotently
  (`(team_id, role_name)` derives the same `role_session_id`), writes the role
  config, and lets `join_policy` decide whether the role enters the work order
  (`immediate` / `deferred` / `timer`); the built-in names (`user` / `main`) and
  empty input are refused at the entry. `TeamView.schedule` projects the runtime
  speaking schedule — the next role, round / limit, the user seat
  (`queued` / `member` / `absent`) and the escape reasons (`round_limit` /
  `no_progress` / `no_executor` / `empty_ring` / `external_break`) — from the
  registry order, held per main session (`serviceState.teamRuntimes`) and synced at
  chat start, queue edit and interjection. `GoalGovernanceView.RoundLimit` (`0` =
  explicitly unlimited) exposes the cap the goal coordinator actually enforces, so
  the UI can render "round n/limit". The GUI Agent Team panel is now two itemized
  tables — a member roster (identity / kind / dedicated session / join timing / tool
  policy + the one-step instantiate form) and a team panel (shape / order policy /
  work order / speaking schedule / timed agents) — rendering backend facts only: the
  frontend neither sends nor caches the order.
- `team work` carries visual evidence into the reviewing role's input.
  `goalTurnWorkSummary` parses the public return of `computer_screenshot` (media
  ref, width/height, foreground window title) into a bounded
  `screen: media:… WxH foreground="…"` fragment placed before the tool-name list, so
  ADVISOR reviews what EXEC actually saw instead of only "a screenshot tool ran".
  Only public fields are used (no pixel content), at most two frames, titles
  truncated to 80 runes, and the tool-name table is the part that gets dropped when
  the sentence has to be truncated. Opt-in real-API probe:
  `gui/team_work_computer_use_live_probe_test.go`
  (`SMOKE_TEAM_WORK_COMPUTER_LIVE=1`).

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
- The GUI frontend now only subtracts work and stops sending the same data twice.
  Per-row actions in the session list, account bar, plugin list, command-palette
  inline suggestions, commit log and work tree moved to one delegated listener per
  container (a redraw no longer rebuilds N closures/listeners and leaves no orphan
  listeners); per-row `title`/child-element tooltips were replaced by a single shared
  `#ui-tooltip` host (`[data-tip]` delegation, `\n` line breaks), which turned session
  entries into a two-segment row — a title segment that ellipsizes by column width
  (the data layer no longer chops the text, so same-prefix sessions stay
  distinguishable) plus a "…" action segment whose pin/branch/delete menu only opens
  on click, with the full title, timestamp and token count moving into the bubble;
  and the git log no longer ships `commits` *and* character-art `lines` —
  `WorkspaceGitLog` emits `--topo-order` + `%P` into `dto.GitCommitNode.Parents` and
  the `GitLogLine`/`Graph` fields are gone. All tree and fork drawing now goes
  through the pure-function `gui/frontend/dist/tree-fork.js` (`treeRowAttrs` for
  indent rails, `layoutCommitGraph` + `commitGraphRowHTML` for commit-lane SVG), so
  the Plan tree, subagent tree, work tree and commit log share one geometry and the
  `├─` / `└─` / `│` / `| \ /` character-art connectors are deleted.
- The shell around the conversation changed: the account bar moved from the left
  sidebar to the right sidebar's status subpage (status key/value table + itemized
  account rows) and the left column keeps only the session tree; the left and right
  columns can be collapsed (`Ctrl+B` / `Ctrl+J`, driven by `data-*-collapsed` without
  dropping `--left-w`/`--right-w`, so expanding restores the original width); and
  focus/pressed states are frame-only (1 px outline with a 0-offset halo, pressed
  state as an inset shadow plus 1-2 px displacement, switches drawn with an inset
  shadow) with `prefers-reduced-motion` disabling every transition and animation.

### Fixed

- Session titles no longer degrade to the session ID prefix in the left session
  list. The title write-through added with head-persisted titles was dead on the
  normal path — `materializeDraftSession` writes the title before the session
  layout exists (the store correctly refuses to create a "ghost" session), and
  the atomic persist path (`SaveCommit` → `writeCommitLayout`) never carried the
  title into `head.Meta.Summary` (only `SaveRecordRaw` did, and v8 retired that
  channel) — so every session that had not been re-opened since that change had
  no stored title, and the sidebar fell back to `truncateTitle(sessionID, 5)`
  which reads as `draft…` because pre-assigned draft IDs are literally
  `draft_<nano>_<n>`. Now the catalog resolves titles in three layers: the
  enumerated row, the session head, and — only when both are empty — a
  **one-time bounded backfill** from the session's first user input
  (`sessionstore.FirstUserInputs`: opens one message shard, scans at most 256 KB
  / stops at the first oversized row, skips internal injections such as
  `kind=internal` or `<!-- seelex:`-prefixed user rows, and refuses to pass off a
  mid-conversation question when the first-shard prefix was LRU-trimmed). The
  backfill result is written back through `SaveSessionTitle`, so the next refresh
  (and the next process) reads it header-only: the steady state stays at zero
  body reads (measured on a real store: 0.3 s for 13 legacy sessions on the first
  refresh, ~20 ms per refresh afterwards; a full-shard decode costs 100+ ms per
  session versus ~1 ms for the bounded scan). Each session is probed at most once
  per process and a refresh does at most 16 per round, so a large backlog is
  amortized instead of delaying one refresh.
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
- Full access and permission auto-approval are now owned by the session that
  toggled them. Both the execution gate and the approval surface used to be single
  process-wide switches: toggling full access in session A silently auto-approved
  session B's tool approvals, and starting session B (`syncFullAccessFor` at chat
  start) turned session A's full access back off — what users reported as "I enabled
  full access but I am still prompted / still rejected". `PermissionGate` now
  resolves per session (`SetFullAccessFor` / `FullAccessFor` /
  `effectiveFullAccessLocked`, keeping the empty session ID as the process-level
  legacy surface) and the tool middleware resolves the session from the context;
  `ApprovalBroker` gained `SetPermissionAutoApprovalFor` / `ResolveAllFor(sessionID,
  …)` so auto-approval and "resolve all" only touch that session's pending requests
  (a request with no session attribution deliberately falls back to the process
  switch instead of guessing a session). Auto-approval now answers `"allow"` instead
  of `"always"`: full access is a session-scoped, revocable mode, so it must not
  leave a permanent allow rule in the shared checker that keeps auto-approving other
  sessions after the user turns full access off — persistent rules are only created
  by an explicit `always`. `Service.SetFullAccess` / `Bridge.SetFullAccess` return the
  **effective** value and the toggle renders that receipt instead of inverting the
  stale local flag (a snapshot one revision behind turned "enable" into "disable"),
  and `syncFullAccessFor(sessionID)` writes only that session's gate cell.
- Plan work-table rows show their DAG dependencies. `plan_load` produces a flat node
  list plus an edge set, and plan rows have no parent in the work table (dependencies
  were derived only from tree parents), so the dependency column was permanently
  empty and users could not see what a node was waiting for. `planDependencies` maps a
  node's in-edges (`plan.Edges` with `To == node`) to `plan:<predecessor>` dependency
  IDs, `mergeWorkDependencies` merges registered and derived dependencies (dedup,
  stable sort for deterministic UI and tests, `nil` when empty so the JSON
  `omitempty` surface still holds), and both publish paths use it — the full
  `buildWorkTable` and the single-row `task.changed` increment
  (`workItemForRecord(record, sessionID)` reads the session's active plan), so an
  incremental update can no longer blank the column. `planNodeIDFor` takes the
  record's `SourceID` and falls back to the `plan:` prefix for older records without
  one.
- Effort switching is steady state again: `SwitchEffort` publishes a full
  `runtime.changed` payload instead of `nil` (a nil payload is classified as "cannot
  apply incrementally" and forces a whole-snapshot refresh, which showed up as a
  stuttering effort control and a flickering skill list); switching plugins
  re-applies the user's current effort level instead of rebuilding the
  `EffortManager` (a rebuild was silently overwritten by the default `high`); and the
  effort entry is disabled during a running turn with an explanation, because the
  backend is required to reject the change (G0b/INV-G7) rather than letting the user
  drag it and see a failure toast afterwards.

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
