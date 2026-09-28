# Changelog

All notable changes to Seelex are documented in this file.

The repository is in Developer Alpha. Source builds report <code>dev</code>;
release builds receive their version from the Git tag through ldflags.

The current release is <code>v0.1.0</code>. The stabilization batch that was
planned under the <code>v0.0.2</code> label ships under this number; the
breaking architectural rewrite is not yet scheduled and will take its own
version when it lands.

## [Unreleased]

### Changed

- **The host side of context folding no longer has an "in-loop" path — Seele replaced the
  whole-round session lock with a turn gate plus a short critical section.** `session/inloop.go` is
  gone upstream (with `Session.InLoopFrom`, `HistoryIfAvailable` and `WithHistoryPublisher`): a turn
  takes a capacity-1 gate instead of holding `Session.mu` from function entry to exit, the working
  history lives behind a short critical section (`session/state.go`), `History()` never blocks, and
  a `ReplaceHistory()` submitted while a turn runs is queued and applied at the turn's next
  checkpoint (before the model call, after the assistant row, before/after each tool result).
  Consequences here: `contract.InLoopEngine`, `EnginePort.HistoryInLoop`/`ReplaceHistoryInLoop`/
  `SetSystemPromptInLoop`, `context_runtime.loopHistoryChannel` + `InLoopChannelFrom`, the
  `prepareOptions.inLoop` field and every `inLoop` parameter are deleted — folding reads and writes
  through the ordinary session-routed methods (`foldHistory`/`replaceFoldHistory`). An in-turn fold
  still takes effect for *that same turn's* next request; it now lands at a checkpoint
  (`state.drain()` runs right before the tool result is appended) instead of being written under the
  lock. The same simplification applies out of turn: a fold aimed at a session whose turn is in
  flight is handed to the engine to queue (`EnginePort.queueSessionHistory`) instead of being parked
  until the turn exits and a fresh engine is installed — `pendingHistory` now only covers engines
  without that capability, and `withInFlightTail` applies to every path (the engine refuses a
  replacement that drops the in-flight `tool_call` unit). Subagent node records read `History()`
  directly. Until Seele tags this, `go.mod` carries a temporary local `replace` (same precedent as
  the 2026-09-15 permission model and the 2026-09-26 InLoop round); drop it and re-vendor when the
  tag lands. Reversal alarm for the model change: `TestEngineHistoryFromToolHandlerReturnsPromptly`
  (in-turn history reads used to self-block; they must return promptly).
- **The message queue and the composer are one card language, and "activated" no longer means a
  glow.** Queue entries are now a card stack peeking over the composer's top edge (`--queue-peek`
  overlap; each card reserves that much bottom padding so the overlap eats whitespace, not text), the
  composer is the card at the bottom of the deck, and the click/focus shadow changed from an accent
  glow (`--focus-glow`) to a lifted card shadow (`--shadow-lift`, added to both themes): a glow reads
  as "selected / error", not "I am typing here". Drop targets (dragging a file out of the explorer)
  highlight with the accent border. Teeth: `TestEmbeddedChatQueueCardStack` (`gui/bridge_test.go`).

### Added

- **A time boundary between the serial and the background command entries.** `bash` and
  `bash_read` are serial: while one runs, the turn cannot do anything else, so a command measured
  in minutes should not hold it. A serial call that declares a `timeout` above `serialBashBudget`
  (5 minutes) is now refused, and the error names the entry that takes it — `bash_bg` (results via
  `job_manage(op=fetch)`, stop with `op=kill`). The check only runs while the job surface is on:
  with `limits.async_exec.enabled: false` there is no `bash_bg` to dispatch to, so the old rule (an
  explicit `timeout` wins) stands; a call that declares no `timeout` is unaffected — there is no
  declared time to audit (its bound is the description plus the prompt rule). The number is stated
  in all three tool descriptions and in a new `Long-Running Commands` system-prompt section, all of
  them derived from the one constant: `TestSerialBashBudgetAgreesWithPrompt`
  (`seelebridge/tools/serial_bash_budget_test.go`) renders the prompt asset and fails when prose and
  code drift apart.
- **The subprocess contract is now one interface: `Add` + the four named management actions
  `Status` / `Fetch` / `Kill` / `Done`, all returning `[]byte`.** `seelebridge/tools/job_contract.go`
  declares `JobTool`; three kinds of job share one registry and one state machine (`process` =
  `bash_bg`, `inline` = `read_batch`, `subagent` = `fork_subagents{async:true}`). `Done` is part of
  the interface — it is the contract's terminal action, not an internal hook: the execution body
  migrates the terminal state (`finish` / `CompleteJob`, passive) and the model retires the settled
  row (`job_manage(op=done)`, active), both idempotent (a job migrates exactly once). `op=done` on a
  running job is refused — the terminal state is only ever decided by the execution body; use `kill`.
  Payloads are `[]byte` (JSON): the tool boundary converts to `string` once, where the framework's
  `ToolHandler` requires it.
- **`bash` splits into a family, and the read-only face carries a server-side guard.** `bash`
  (serial, write-class, no more `background`) / `bash_read` (read-only, no approval — but every
  command goes through `security.ClassifyCommand`, which fails closed: unknown first word, compound
  commands, redirects, variable expansion and write subcommands are refused, never silently
  executed) / `bash_bg` (managed background: `Add` returns an acceptance receipt) / `read_batch`
  (N read jobs in one call, dispatch-and-return) / `job_manage` (`op=observe|fetch|kill|done`). The
  old `async_output` / `async_kill` names are gone with **zero residue** in code and config,
  enforced by `TestRetiredJobToolNamesLeaveNoResidue`.
- **Completed jobs are backfilled into the work-table trace block with a bounded summary.** The
  row stays visible after the job ends carrying `state + exit + lines + bytes + bounded tail`
  (≤512 bytes, cut on a UTF-8 boundary), instead of the row silently disappearing; the block
  (markers + title + hint included) stays within `workTableTraceMaxLines`, and dropped completed
  rows are summarised in one line rather than vanishing. The model's `fetch`/`done` retires the row
  and the fetched result becomes the single source of truth. `Notified` is the idempotence key.
- **Files can be dragged out of the explorer and queued for sending.** A work-tree file row is
  `draggable` and carries its workspace-relative path (`data-file-drag`); the composer and the message
  queue are drop targets. A drop onto an empty composer is submitted through the ordinary composer
  path (a running session queues it server-side — that is where the stacked queue cards come from); a
  drop onto a non-empty draft only appends the reference, so the user's sentence is never cut short
  and never sent on their behalf. The mapping is pure and DOM-free
  (`gui/frontend/dist/file-drop.js`: `normalizeDropPath` / `dropPaths` / `fileQueueText` / `dropPlan`),
  pinned by `gui/frontend/dist/file-drop.test.mjs`: only workspace-relative paths are accepted —
  absolute paths and `..` are dropped, and a bare OS file *name* is refused rather than turned into an
  instruction to read a file that does not exist.
- **A file detail panel can be edited and saved with Ctrl+S.** The panel grew the write half of the read
  face it already had: `Bridge.WorkspaceWriteFile` → `Service.WorkspaceWriteFile` → the *optional*
  `WorkspaceFileWritePort` (declared apart from the read-only port, so a read-only host must fail loudly
  instead of silently growing a write ability), and the write itself is an atomic publish in `workspace`
  (`temp file + rename`, the same visibility boundary as the read face). Saving is a user action, so it
  does not pass the main agent's tier or approval — and it is not taken on its own report: the file is
  read back through the same path and the editor baseline becomes what came back, so an outside change is
  named rather than hidden. Only Ctrl+S (or the toolbar's save) writes; the buffer and the on-disk baseline
  are two separate facts, the chip carries a dirty mark, and exiting the edit, closing a chip or collapsing
  the drawer asks 保存 / 不保存 / 取消 first — cancel and a failed save both leave the buffer exactly as it
  was. The file's encoding facts survive the save (EOL style and BOM are recorded on load and restored on
  write), and a file that is not UTF-8 (GBK, UTF-16), whose read was truncated, or that is not text-like
  gets no edit entry point at all: saving one would silently rewrite the file's encoding. Teeth:
  `workspace/writefile_test.go`, `application/core/workspace_file_usecase_test.go`
  (`TestWorkspaceWriteFile*`), `gui/bridge_test.go:TestEmbeddedFilePreviewEditWiring`,
  `gui/frontend/dist/file-preview.test.mjs` (edit eligibility, EOL/BOM round-trip, dirty judgement),
  `gui/frontend/dist/file-preview-controller.test.mjs` (Ctrl+S is the only write, read-back baseline,
  dirty close guard).
- **The fold can now pay for a real Chapter 2 — behind a switch that ships closed.**
  `limits.context_compaction_summary` (`enabled` / `input_tokens` / `chapter2_tokens`, negative values
  fail loudly in `LoadLimits`) gates a prefix-replay thick summary for the fold that runs right before
  a request: `MainCompactionDAG` (`seelebridge/runtime_context.go`) is the same compaction DAG the
  controller path uses, except that it *does* inject a `seelexctx.PrefixReplaySummarizer` (QuickChat,
  the compressor's own construction path), and `replayInputTokens` honours the configured shard budget
  while its zero value keeps the derived "account window × 3/4" — a knob whose zero must mean "behave
  as before", not "stop sharding". Closed is closed: with the block missing or `enabled: false` the fold
  is the local deterministic one (frame `summary_source=local`) and not a single extra model call is
  sent, because opening it buys an *unattended paid call* — once per fold, once per shard once the
  overflow exceeds the shard budget — and its payoff is that Chapter 2 stops being a metadata
  projection: "Errors and fixes / Pending / Next step" are no longer a constant `(none)`, which is what
  gives `search_history`'s deterministic lexical first pass something to match. The in-turn controller
  path still injects no summarizer: its `ev.History` is not yet proven byte-identical to the wire
  request, and a replay that misses the prefix cache pays full price for nothing. **Live since the
  assembly-fold frame push landed** (see the entry under **Fixed**): the fold path now reaches
  `MainCompactionDAG` through `PushCompactionFrame`, so `enabled: true` changes behavior — the stack
  frame's `summary_source` reads `replay`, and the frame body embeds that summary verbatim.

### Fixed

- **An assembly-layer fold never pushed its frame onto the session's compaction stack, so the three
  paths that read that stack were dead for every session whose folds happen only there.** `190049e`
  added the capability face (`context_runtime.CompactionIndexPort`), the landing site
  (`seelebridge.Runtime.PushCompactionFrame`) and the adapter — but no caller: `Deps` had no
  injection point and the coordinator never probed for one (`git grep PushCompactionFrame` hit only
  those three). The compaction stack therefore stayed empty for the fold path that actually runs
  most often (soft line / `/compact` / `compact_context`), which silently disabled every reader: the
  memory-block first pass returns nil, `search_history` degrades to a tail scan, and gap coverage
  skips itself. The assembler now probes the runtime for that narrow optional interface
  (`application/core/service_assembler.go`) — a failed probe means "no index", which is the correct
  behavior and must not force every fake/harness to grow an empty method. The fold landing site
  calls `pushCompactionFrame` once (`context_runtime/compaction_index.go`), taking every input from
  the fold itself instead of recomputing it: the overflow is `transcript[retainedFrom:compressedTo]`
  (anything before `retainedFrom` is already covered by earlier frames, and re-feeding it would make
  search hit the same content twice), the replay material is `existing` (the previous real request's
  history bytes — rebuilding it from the event stream would lose byte identity, and with it the
  prefix cache the replay was buying) and the range is the recorded `TranscriptPrefixRange` value
  (`EventSeq` is the authoritative fact on this side; unit indices are reverse-looked-up by the
  receiver — the two are not subtraction). A seventh gate (`index`) joins the authoritative order
  `judge → assemble → replace → index → frame → store → record`, with the frontend label table
  pinned to that order by a test; the frame body carries `segment_id` / `summary_source` and embeds
  the stack frame's summary verbatim. Degradations are booked separately, because "never tried"
  (index face not assembled → `index=unavailable`), "tried and failed" (`index=error err=…`) and
  "nothing to push" (`index=skipped reason=no_overflow`) are three different facts — and a failed
  push never interrupts the fold or the request. Teeth: `TestFoldPushesCompactionFrameIntoIndex`,
  `TestFoldWithoutIndexFaceReportsDegradedGate`, `TestFoldPushFailureIsReportedNotFatal`,
  `TestFoldWithoutOverflowReportsSkippedNotUnavailable`
  (`application/core/context_compact_index_test.go`) and `TestFrontendGateLabelsMatchBackendOrder`.
  Side effect: `limits.context_compaction_summary` is no longer a switch that does nothing when
  opened (see the entry under **Changed**).
- **A fold's record vanished on the very next round, and a message sent while browsing history was
  swallowed.** Both are state-ownership bugs behind one field report ("after a fold the next round's
  compaction is nowhere to be found; my input is gone and nothing on screen moves until the round
  ends, while 'return to latest' does nothing"):
  - `continuationTaskExecutionState` used the *turn* continuation predicate (`IsContinuableStatus`)
    as the gate for *session* context facts. A turn that ends normally (`completed`) and a
    cold-loaded session whose maintenance identity has been withdrawn (`idle`) are both
    non-continuable, so the next round started from a blank state: `ContextCompactions` — the single
    source behind the status panel's "context compaction" entry, the trajectory compaction mark and
    the conversation's fold divider — plus `ContextVersion` and `ContextRetainedFrom` were dropped.
    The retained window start is the expensive one: the next assembly counts the already-folded
    prefix again, so a long session crosses the soft threshold every round ("one message, one
    compaction record"). Session context facts are now inherited unconditionally; only turn facts
    stay behind the continuable gate. Regression tests:
    `TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance`,
    `TestReproContextFactsSurviveCompletedTurnBoundary`
    (`application/core/context_compact_across_rounds_repro_test.go`).
  - A `user` row appended while the visible window was not tail-aligned only advanced
    `TotalMessages` and never entered the window — correct on its own, since writing it would punch
    a hole between the window and the tail — but the renderer's reducer refuses out-of-window
    `message.added` for exactly the same reason, so the round the user had just started existed in
    neither place. The input looked swallowed and the whole round stayed invisible until it was
    persisted at round end or the user pressed "return to latest" (which cannot reach unpublished
    in-flight rows either). A user row is now the boundary case: appending it ends browsing first
    (`endBrowsingForNewTurn` resets the window in place to "this message is the tail"), so the
    window is tail-aligned again and the round streams where the user is looking; assistant/tool
    rows keep the old rule. Regression tests: `TestReproSubmitWhileBrowsingKeepsUserRowInWindow`,
    `TestReproSubmitWhileBrowsingEndsBrowsingState`
    (`application/core/session_history_browsing_submit_repro_test.go`). Known boundary (unchanged):
    rows already in flight when the user pages *away* mid-round are only recoverable once that round
    is persisted — documented in `application/core/README-session.md` §9.
- **The stop button could not stop a running foreground tool call.** Cancelling a turn aborted the
  engine loop but left the tool's descendants alive: `executeScopedBash` relied on
  `exec.CommandContext`'s default cancel, which kills only the shell it started, while the
  grandchildren that shell forked kept running *and* kept holding the output pipe — so `cmd.Wait`
  could not return until they exited and "stop" meant "stop, then wait another thirty seconds". A
  build-like command that spawns a grandchild reproduced it deterministically: the marker the
  grandchild writes 2s later still appeared, and the tool call only returned after the assertion
  window. The synchronous path (`bash` / `bash_read`, including the docker-recovery retry) now uses
  the same termination primitive the background execution domain already had — `newScopedCommand` =
  `security.ProcessTree` + `ConfigureProcessTree` + `cmd.Cancel` + `WaitDelay` — so stopping (and the
  tool `timeout`) terminates the **whole tree**. Background jobs are deliberately untouched: they
  drop the turn context via `context.WithoutCancel`, so the stop button never kills a `bash_bg` /
  `read_batch` / `subagent` job. The queued inputs of a stopped turn are still all sent — the queue
  is promoted into the next turn as before. Regression tests:
  `TestStopTerminatesForegroundToolRun`, `TestStopLeavesBackgroundSubprocessRunning`
  (`seelebridge/tools/stop_foreground_run_test.go`) and
  `TestStopFlushesQueuedInputsIntoTheNextTurn` (`application/core/service_stop_flush_test.go`).
- **Three frontend stalls behind "the session looks frozen and the newest messages are not
  visible"** — found while auditing the compaction path end to end, each with a regression test:
  - The conversation's top sentinel auto-loaded an older page whenever its `IntersectionObserver`
    fired, including while the user was parked at the tail (160px `rootMargin`, container shown
    again after `display:none`, layout shifts). Loading an older page slides the visible window
    back, so the newest rows silently left the DOM. Auto-loading is now gated by
    `shouldAutoLoadOlder()` in `conversation-view.js`: never while the view follows the tail; the
    explicit "load earlier" button still goes through the host command.
  - `resync.required` was handled *after* the `delivery_seq` dedup test, so a host instruction whose
    watermark was equal to or below the renderer's applied watermark was swallowed as a duplicate:
    the full reload never happened, the view stayed on stale content, and the acknowledgment told
    the host there was nothing left to re-deliver. Authority-alignment events are now handled before
    dedup and set the watermark to the value the host supplied.
  - The ready baseline only reset the applied watermark when the session id changed. A *same-id*
    re-subscription (Bridge rebuilds the subscription, the view session stays) restarts
    `delivery_seq` at 1, so the stale watermark swallowed the new subscription's first events and
    reported that high watermark to the host, which then stopped re-delivering: permanent silence
    with no error to look at. `seelex:ready` now goes through `client.acceptBaseline()`, which
    resets the applied watermark — and only that entry point does: an ordinary mid-subscription
    refresh must keep it, or events re-delivered from the replay window would be applied twice
    (streaming deltas would double).
- **`EnginePort.History()`/`RawHistory()` no longer call into the engine while holding `port.mu`.**
  The two alias readers still held the process-wide read lock across `engine.History()`; the same
  S3b shape as `RawHistoryFor`, but reachable by user commands (`/history`, workspace switch) rather
  than by folding. The engine call now happens outside the lock, matching the rest of the package.

- **Two load-sensitive test flakes that made the full suite red intermittently** (both reproduced,
  neither caused by the contract work above — the first lives in the framework's telemetry
  projection, the second in a pre-existing repro test):
  - `seelebridge.TestSessionLifecycleEventsLLMAndToolIntentEffect` paired "the last `llm.before`"
    with "the last `llm.after`" from `Tracer().Query`. That view is **not order-stable**:
    `MemoryTracer.Query` walks `trace.spans` (a map, randomised iteration) and only *stable*-sorts
    by timestamp, while the Windows clock is coarse — a fast round's before/after land in the same
    tick and inherit map order. Measured: 40 sessions × 200 queries → 40/40 sessions showed multiple
    view orders, and the old criterion mismatched **111** times while reading the framework's
    `trace.Operations` (grouped by `CorrelationID`, order-independent) mismatched **0** times. The
    test now asserts intent-effect pairing through `Operations` and dumps the event list on failure.
  - `TestWorkspaceSwitchConcurrentWithBackgroundPersist` fired 8 concurrent `Submit` calls in its
    final phase and then returned: `Submit` returning is not the turn finishing, so `t.TempDir()`
    cleanup raced the still-running background writes (`RemoveAll …: The directory is not empty.`).
    It now settles with `WaitForIdle` (all sessions, bounded) before asserting; reproduced 1/1
    before, green 10/10 after.

- **Background commands land as a polling slice: `bash background=true` returns an acceptance
  receipt, and the model fetches the output with `async_output(handle)`.** A long command no longer
  holds its tool call (and with it the whole turn) open — the dispatch call's `tool_result` is
  `{status:"accepted", handle, log_path, state:"running"}` and never the command output, so the
  `tool_call`/`tool_result` pair still completes inside its own unit: history stays append-only,
  nothing is rewritten later, and there is no "wake an idle session when the result lands" link
  (that link would re-enter the session lock `ChatStream` holds for the whole round).
  `async_output(handle, wait_ms)` returns **only the bytes produced since the previous call for that
  handle** (a cursor over the log file, 4000 chars per call — repeated polls never replay the log),
  `wait_ms` defaults to 5s and is clamped at 60s (negative returns immediately), the terminal poll
  carries `exit_code`, and an unknown handle is an error rather than an empty success (an empty
  success would read as "the command produced no output"). Isolation and budget: handles are valid
  only in the session that dispatched them; the same command in the same session is not re-run while
  it is still running (`repeated=true`), a dedup key that applies only to `state=running` so one
  failure never permanently blocks that command; the registry caps in-flight runs at 32 and records
  at 256, evicting the oldest finished record **and deleting its log file** (record slots are capped,
  files are not — otherwise a long session grows the temp dir without bound), and the runtime removes
  the whole output directory on shutdown because the directory is process-scoped and nothing else
  would ever reclaim it (before that hook, a dev box had accumulated 44 orphaned `seelex-async-*`
  directories): the removal is attempted immediately and, when a still-running command holds the log
  file open, retried by the last execution to finish — the one point where the handle is certainly
  closed — while dispatching after shutdown is refused rather than creating a directory nobody will
  collect; each run gets its own 30-minute hard cap that synthesizes `exit_code 124` and writes a
  note into the output, because the synchronous path's tool timeout cannot apply once the receipt has
  been returned; and output is truncated at 1 MiB with the writer still reporting every byte as
  consumed — reporting a short write would make the command itself fail, dressing an infrastructure
  limit up as a command failure. `async_output` is routed to the read-only permission group:
  retrieval inherits the
  authorization of the dispatch that was already approved, so it is not re-prompted. The slice is
  gated by `limits.async_exec.enabled` (**default false**) and off is off, not "quietly
  synchronous": the `background` property is absent from the bash schema, `async_output` is not
  registered, and `background=true` is refused outright. Teeth: `seelebridge/tools/async_exec_test.go`
  (the receipt carries no command output; deltas of repeated polls concatenate to the command output
  exactly once; cross-session retrieval refused; unknown handle errors; dedup only while running;
  in-flight cap; eviction deletes the log; incremental cursor; truncation without short writes;
  `wait_ms` clamping; the switch gates schema, registration and handler together; permission group)
  and `seelexctx/limits_test.go:TestLimitsAsyncExecDefaultsOff`. See
  [`docs/2026-09-24-async-tool-deferred-ack/README.md`](docs/2026-09-24-async-tool-deferred-ack/README.md) §8.
- **The fold boundary swallowed the round's own question, so the model lost the goal.** A follow-up to
  the "boundary must land on a round start" fix: the boundary was still judged on a *unit whose first
  row is a user-role row*, and a round's injected internal material (`role=user`, provider role
  `system`, `wire_material=true`) is exactly that. Material rows sit *after* the question, so the
  walk-back stopped on them and the question just before them fell into the folded prefix — the kept
  window began with the round's continuation and the model could no longer see what it had been asked.
  "What counts as a real user question" now has one definition — `isUserQuestionEvent`
  (`application/core/task_context/plan_transcript.go`: role, `WireMaterial`, active-skill marker,
  logical role name, kind) — shared by the fold boundary (`opensRound`) and by the maintenance
  objective's "last real user input" lookup, which already used that criterion; the drift between the
  two was the bug. Teeth: `TestTranscriptTailWindowKeepsRoundStartAcrossMaterialInjection`,
  `TestIsUserQuestionEventMatchesMaintenanceObjective`
  (`application/core/task_context/plan_transcript_window_test.go` — with the old predicate the window
  start is 3, the material row, instead of 2, the question).
- **`insufficient tool messages following tool_calls message`: the provider's second tool-pairing
  wording is now repaired, not just classified.** `859c360` taught the classifier that wording, but
  the history was still sent in a shape the provider rejects. The repair paired results to
  declarations by *first result per `call_id`*, which breaks the moment a `call_id` is declared twice
  (a retry/interrupted call reusing its ID — the field shape: "session loop 0" dying on the first
  request): the second same-name result was discarded as a duplicate, leaving the second declaration
  with nothing adjacent to it. A declaration carrying an empty or within-row-duplicated `call_id` was
  waved through as "cannot pair, leave the chain as is" — but the provider counts receipts by
  `tool_calls` entries, so such a call can never be satisfied. Pairing is now per occurrence (k-th
  declaration ↔ k-th result), unpairable calls are dropped from the declaration, and a synthetic
  placeholder is emitted only when the call is provably not in flight (the ReplaceHistory path, or a
  repeated declaration). A placeholder also no longer shadows the real result that arrives later —
  that ordering used to make the model believe a tool had not run. Both sides of the twin
  implementation are fixed together (`application/core/context_runtime/history.go`,
  `seelexctx/history_safety.go`), and the wire assertion now covers the whole rule (per-declaration
  receipt, not only orphan/adjacency/duplicate). Teeth: `history_pairing_gap_test.go`,
  `wire_pairing_gap_test.go` — all four cases fail before the fix with the exact reported wording.
- **The work-table modal had three nested vertical scroll containers, so one wheel notch jumped three
  blocks.** `.modal-card[data-resizable]` brings `overflow: auto`, the modal body added a second one,
  and the table area capped its own height — the same shape as the session list. The card and the body
  now only pass height down (flex column, `overflow: hidden`) and the table area is the single
  scroller. Teeth: `TestEmbeddedWorkTableSingleScrollContainer` (`gui/bridge_test.go`).
- **A new session wore the previous session's permission tier.** The chip and the runtime panel read one
  field — the view snapshot's `Runtime.PermissionTier`/`FullAccess` — so every path that moves the view
  pointer to another session has to recompute it inside the same critical section. Switching sessions did
  (2026-09-17, `TestPermissionTierSwitchMirrorsViewSnapshot`); *entering* one did not. `BeginNewSession`
  reset the plan, the session row and the chat state and left the tier field untouched, so the new session
  displayed the tier of the session the user had just left while the gate itself ran with the new session's
  own tier — and the quiet direction is the dangerous one ("looks manual, is actually full access"). The
  recomputation now has a single definition, `syncViewPermissionTierLocked` (session slot first, process
  default when the session never chose), and the three entry paths — new session, unload-to-draft, failed
  restore fallback — call it; `BeginNewSession` reads the (reusable) draft slot's persisted tier outside the
  lock, exactly as cold start, hot mount and cold restore already do. Teeth:
  `TestBeginNewSessionMirrorsDraftPermissionTier`, `TestBeginNewSessionRestoresPersistedDraftTier`
  (`application/core/session_permission_tier_persist_test.go`).

### Changed

- **Seele moves from the local in-loop `replace` to the released `v0.3.1`.** `go.mod` now requires
  `github.com/RedHuang-0622/Seele v0.3.1` (6a04a7c, `feat(session): 环内历史把手 InLoop`) and the
  2026-09-26 temporary `replace => G:/Program/go/Seele` is gone; `go mod vendor` regenerated the ignored
  vendor tree, so `vendor/modules.txt` pins v0.3.1 with no replace. The handle Seelex already uses
  (`session.InLoopFrom` → `EnginePort.HistoryInLoop/ReplaceHistoryInLoop/SetSystemPromptInLoop`) is the
  released API, so the version bump required no Seelex source change; `AGENTS.md`, the root `README.md`,
  `seelebridge/`, `sessionstore/` and `seelebridge/multimodal/` now cite v0.3.1. Verified with
  `go build ./...`, `go build -tags "gui,desktop,production" ./...` and `go test ./...` (only the
  pre-existing local `e2e` vendor-README gate stays red, and that gate walks gitignored `vendor/`).
- **Compaction thresholds now ship at 95% soft / 98% hard / 80% target instead of 75/90/60.** The code
  default and the checked-in `config/seelex.yaml` carry the same numbers (`seelexctx.DefaultLimits` is the
  single source; `task_context.newContextBudget` no longer keeps a second copy of the fallbacks), and
  `context_target_percent` is a live cap rather than documentation: post-fold retention is capped at
  `target`, so `soft − target` is the headroom the next turn has to consume before folding again. The
  75/90/60 figures survive only where a test pins the *mechanism* it exercises (`controllerTestLimits`,
  `pinMechanismCompactionRatios`) or in point-in-time research notes.
- **Compaction is now readable in three places, each with one job.** ① The status sub-page's
  Overview section holds **only** the compaction stack, as a table: one row per folded frame, newest at
  the top, frontier row flagged 栈顶, older folds stepping down in the stale gray, and the frame body
  read back by `ref` when a row is opened. The English scope sentence that used to sit there is gone —
  the project name, root path and the status table already say both things it claimed, and the card list
  it shared the section with misaligned in a 300px rail. Rows keep carrying the *original* array index in
  `data-compact-open`, so reordering the stack cannot make row 1 open row 2's body. ② The context axis
  marks each compaction frame with a **dashed** vertical tick instead of a solid bar, and the gray step
  encodes recency: the current frontier frame in `--compaction-frame-latest`, every superseded frame in
  `--compaction-frame-stale`. Both tokens alias the neutral ramp (`--text-strong` / `--faint`), so the
  requested inversion in dark mode comes from the two mode bases themselves and no second palette was
  written; the axis cut line and its label use the same latest-frame color. ③ The conversation area
  separates the frontier with two fading dashed rules around a chip, and the chip now states in visible
  text — not only in a tooltip — that everything above the line is still readable by scrolling, and that
  it is simply no longer sent to the model. Browser-checked against the real renderers and `styles.css`
  at rail width in both modes (`_logs/s3c_visual.html`); the first table draft broke Chinese text
  mid-token at 280px, which is why the row is three columns with a two-line body.
- **GUI highlights are a tint of the skin, and the conversation column is a rounded panel.**
  A selected tab or highlighted button is no longer a black block or a black frame: the fill is
  a thin tint of the current skin's primary signal (`--hl-fill: color-mix(in srgb, var(--accent)
  14%, var(--surface))`, ink `--accent-strong`), with no border, no `inset 0 -Npx 0` bottom bar,
  no shadow and no offset. The "rounded corner + black bottom edge" skeleton is gone from the
  whole tab family (conversation sub-tabs, right-column sub-tabs, sheet tabs, terminal tabs,
  schedule pill), and the right-column "paper bookmark" skeuomorph is retired. The conversation
  column itself is now a *rounded panel*: `.workspace` carries an 8px radius (`--r-md`) and the
  shell gradient shows through the four corner arcs while the edges stay flush with the window —
  the same 7–8px paper corner measured on Qoder. In the right column, the context-compaction
  block moved back **inside** the 状态 fold right after 概要, and `repaintCompactions` opens that
  fold while a round is in flight (historical records alone never force it open, so a manual
  collapse is not fought back). The Agent Team panel now states one fact per block: the employee
  library is the only editor for an employee's profile (prompt / permissions / kind), the staff
  rail only edits the current session's roster and speaking order, and its per-row 编辑 button
  (which wrote a second, session-scoped copy of the same profile) is gone.
- **The GUI skin and the light/dark mode are two independent axes now.** The skin used to *be*
  the mode: six whole-palette themes (`qoder-light`, `qoder-dark`, `graphite`, `verdigris`,
  `paper`, `silver`), each pinned to one `data-theme`, so choosing depth meant choosing a
  different skin and no skin could offer both. Now the neutral base (surfaces, text, borders,
  status colours, translucent fills, shadows, ticks) lives in `styles.css` under `:root`
  (light) and `:root[data-theme="dark"]` (dark), while `themes/<skin>.css` carries only brand
  tokens — the primary signal and the ambient gradient — in both depth variants (8 `--skin-*`
  tokens, one `:root` rule). `theme.js` grows two orthogonal actions (`applySkin` → `<link>`,
  `applyMode` → `data-theme`) persisted separately (`localStorage["seelex.skin"]` /
  `"seelex.mode"`); the settings “Appearance” section shows two card rows
  (`#theme-picker` / `#mode-picker`). Five skins × two depths = ten looks, and “add a skin” is
  still one CSS file plus one manifest entry (manifest schema 2: `skins[]` + `modes[]`).
- **Each skin ships its own ambient gradient.** `--shell-gradient` — the equal-luminance,
  purely vertical colour-temperature sweep that makes the shell read as one continuous field
  (Qoder's `#EBEBC6 → #D1DAE2`) — is now a skin token with a light and a dark variant, bridged
  by `styles.css` (`:root` / `:root[data-theme="dark"]`) so each skin's sweep follows the depth.
  The five skins get five distinct sweeps (Qoder yellow→cyan, graphite ivory→steel, verdigris
  green→teal, paper warm→cool paper, silver silver→cool grey). Section 27 applies it: the shell
  (`.app-shell`) carries the gradient, the top bar and side panels go transparent, and the
  centre column stays a pure-white “paper” on top, joined by neutral `--border-hairline` seams.
  The Wails window background moves to `#EBEBC6` (the default light shell's gradient start) so
  the first frame no longer flashes the old fill. Render evidence:
  `docs/design/qoder-skin/tools/verify-skins.py` (headless-Chrome pixel sampling over all ten
  combinations) shows the five light shell-top
  colours are all distinct, while every skin's content-paper colour depends only on depth
  (`#FBFAF6` light / `#1F1F1F` dark) — the two axes really are decoupled.
- **Border temperature decided (P2-1, option A).** `--border` moves from warm `#e6e3da` to
  neutral `#e7e7e4` so warm hairlines stop muddying the cold bottom of the gradient; the new
  `--border-warm: #e6e3da` keeps warmth for the few places that genuinely want it.

### Fixed

- **Long sessions no longer compact once per turn: the turn-boundary judge re-accumulated the whole
  transcript instead of looking past the last fold.** The soft/hard decision compared
  `min(whole transcript, budget) + system + tools + input` against `budget × percent`, while the retained
  window was chosen by summing each event's *recorded* `TokenCount` — two different rulers. Once a session
  was long enough to fill the budget, the compared number sat at or above the whole budget (measured
  frames: `compared 224159` against `budget 158616`), so raising the soft percent from 75 to 95 changed
  nothing and every user turn recorded another fold (13:20:20.96 user message → 13:20:21 frame). The fix
  keeps `TaskExecutionState.ContextRetainedFrom` — the transcript event index already covered by the last
  fold — and makes the judge, the retention window and the assembly all read only `events[retainedFrom:]`;
  `TranscriptTailWindowBy` lets the window be selected with the same counter the judge and the final
  estimate use, so a window trimmed to `target` no longer measures larger than `target` on re-estimate.
  Teeth: `TestContextBudgetDoesNotRecompactWithoutNewContent` (red before the fix: two records with no
  new transcript content), `TestTranscriptTailWindowByUsesInjectedEstimator`, and the existing prefix/
  range/restore suites in `application/core` + `seelexctx`. The running dev GUI still loads its own
  `dist/seelex-gui-dev/config/seelex.yaml`, which has no `limits.context_*` keys — the repo-root
  95/98/80 edit only takes effect once that file (or the rebuilt product) carries it. The fold target is
  no longer dead configuration either: `RetainDecision` now applies `limits.context_target_percent` as a
  hard cap (`TargetTokens`/`TargetApplied`) on top of the `min(token1, ratio × all)` retention rule, so
  "keep more, but leave soft − target of headroom" is one config line rather than a code constant.
- **A fold issued between turns could still freeze every session: `EnginePort.mu` is no longer held
  across a wait on a session lock.** The in-turn self-deadlock was closed by the in-loop handle, but the
  out-of-loop path kept three fuses, all the same shape — take the process-wide `port.mu`, then call into
  a session engine whose `Session.mu` is held for a whole round by someone else:
  `replaceRawHistoryFor`'s non-active branch (`ClearHistory` + per-message `AppendHistory` with no
  in-flight check at all), `ReplaceRawHistory` and the active branch (`replaceActiveHistoryLocked` ran
  even when `engineCalls > 0`, so the "defer the install" guard only deferred half of it — and touching
  that view mid-round is pointless anyway, because the loop overwrites it with its own `rl.history` at
  exit), and `RawHistoryFor` (`engine.History()` inside `port.mu.RLock()`, which also parks every later
  reader once a writer queues). The consequence was never "this call is slow" but "no other session can
  start a turn", since `ChatStreamFor` needs that same lock to increment its own counter.
  The rule is now stated once and enforced everywhere: **no `port.mu` hold may span a possibly-blocking
  `Session.mu` wait**. Under the lock the port only reads and writes its tables and engines nobody else
  can reach yet; a session with a turn in flight gets its fold **registered** in a per-session pending
  registry (previously one slice plus one target id, which also let the legacy `ChatStream` exit install
  another session's fold onto itself) and installed at that session's own turn exit, where the count has
  reached zero while `port.mu` still keeps new turns out. Reads resolve the engine under `RLock` and call
  `History()` outside it, so the authoritative semantics survive and only the caller waits. Two defects
  fell out of the same sweep: the background replace path re-appended the system row after
  `ClearHistory` had deliberately kept one — duplicating the prompt on every background fold — so both
  paths now share `installHistoryInPlace`, and `installSessionEngineLocked` flipped the active alias for
  whichever engine it built, letting a background session's fold switch the active session away.
  Teeth: `engine_port_lockfuse_test.go` drives a real hanging tool round on one session and asserts both
  halves per fuse — the fold returns immediately **and** an unrelated session still runs a turn; with the
  pre-fix shapes restored, those assertions fail at 2s and 10s respectively
  (`_logs/s3b_prefix_write.log`, `_logs/s3b_prefix_read.log`). `internal/adapters/README.md` gained the
  lock-discipline section, and the existing lazy-resume test caught an intermediate version that built two
  engines per replace.
- **`compact_context` called from inside a running turn folded nothing and froze the whole process: it
  now compacts on the spot.** `Session.ChatStream` holds `Session.mu` from entry to exit and dispatches
  tools on that same goroutine, so the tool's landing point started with `engineHistory →
  EnginePort.HistoryFor → engine.History()` — a second acquire of a non-reentrant mutex already held by
  the caller, i.e. a permanent self-deadlock (`TestEngineHistoryFromToolHandlerSelfBlocks` measured it:
  the probe never returned, and cancelling the context did not end the round). Worse, that read held
  `EnginePort.mu` in read mode *while* blocked, and Go's `RWMutex` stops admitting new readers once a
  writer queues — so every other session's `ChatStreamFor`, which needs that same lock to start a turn,
  queued behind it. One model tool call therefore froze history reads, replacements, session start and
  resume across the entire process, and the compaction round taken from the previous slice was never
  released either.
  The fold's history input and output now move to the only legitimate place they can be touched without
  re-locking: the handle the engine injects into the turn's own context once it holds the lock (Seele
  `session.InLoop`, reached through `contract.InLoopEngine` and `context_runtime.loopHistoryChannel`, a
  value that exists for one call only — no counter, no timestamp, nothing that has to *guess* whether
  the engine is running). `Session.mu` is acquired exactly once per turn instead of twice,
  `EnginePort.mu` is not involved at all, so the blast radius shrinks from process to that one session.
  Crucially the fold no longer hands its result to the next load: the out-of-loop path arms
  `PrepareMainSessionHistory` because the engine may be rebuilt before the next turn, but the in-loop
  path is rewriting the history this very turn is reading, and the loop persists it at its own exit
  (`saveToCache → DurableHistory.Save`). The compacted frame is therefore the first message of the next
  provider request **in the same round**, and the receipt carries the real numbers instead of a promise.
  In-flight lower bound: at that point the assistant `tool_calls` is already in history while its result
  is not, so `withInFlightTail` keeps the current round's own tail in the replacement (the engine refuses
  a replacement that would drop it, `ErrInLoopInFlightDropped`, rather than let the soon-appended tool
  message become an orphan). **The compaction strategy did not change**: thresholds, soft/hard lines,
  window size, frame construction, record gates and the six-gate order are untouched, and
  `TestCompactContextInLoopAndOutOfLoopAgree` pins that claim by asserting judgement counts, folded
  ranges (event and message ends), `reason`/`origin` and the written-back provider history are
  field-for-field identical between the two paths on the same history — while requiring the channel
  counters to be non-zero on one side and exactly zero on the other, so "identical" cannot just mean both
  runs silently took the same route. Lock-free paths (idle-session `/compact`, between-turn assembly)
  behave exactly as before, since `inLoop == nil` falls back to the previous accessors.
  Teeth: `internal/adapters/engine_port_inloop_test.go` (immediate effect in the same turn; dropping the
  in-flight tail refused and history untouched; unavailable outside a turn), the new Seele
  `session/inloop_test.go` (handle visibility, generation guard rejecting a handle from a finished turn,
  refusal never mutating history) and `_logs/s3_*.log` for the build/vet/test run.

- **An explicit compaction round and a newly opened turn no longer read and write the same engine
  history concurrently.** Pressing `/compact` in a session with no in-flight turn folds the loaded
  context immediately (see the 2026-09-24 entry), and that fold deliberately writes nothing into
  `ChatState.Running` — faking a running turn would leak "someone is executing this session" into the
  snapshot, the task registry and the stop button. The three honest constraints left one hole: the
  submit path's only busy test is `runtime.ChatState().Running`, so a message typed while the fold was
  in flight opened a turn whose assembly read the engine history, replaced it, and won the race against
  the fold's own `ReplaceHistoryFor` — the user got a compaction record that immediately disappeared
  plus an answer built on the pre-compaction context. `Service` now keeps a per-session
  `compacting` set (same lock, same shape as `restoring`) that `CompactContextNow` claims before
  folding and releases once the receipt is built; `submitConversation` and `submitConversationFor`
  check it after the `Running` test and hand the input to `deferSubmitUntilCompacted`, which waits for
  the settle point and replays the submit **on the same target session**. The run-time input queue is
  deliberately not reused: its promotion point is *turn end*, and an explicit compaction has no turn,
  so a queued message would sit there until the user sent something else.
  `TestSubmitParksWhileCompactionRoundOpen` / `TestCompactionRoundIsAcquiredExclusively` /
  `TestCompactionGateIsScopedToItsSession` pin the three behaviours (parked, serially acquired, scoped
  to one session), and `internal/adapters` grew a real-`Session` probe documenting the neighbouring
  engine-lock fact that in-turn compaction still has to respect (`Session.ChatStream` holds `Session.mu`
  from entry to exit, so history writes issued from a running round's own goroutine never return).

- **Both compaction layers now read one configured source: `limits.context_soft_percent` (and its
  siblings) used to steer only the assembly layer.** `seelexctx.ContextWindowPolicy` computed
  `safety = window/8` and soft/hard/target as 75/90/60 % of budget **in code**, while
  `task_context.newContextBudget` read `context_safety_reserve_divisor` / `context_soft_percent` /
  `context_hard_percent` / `context_target_percent` from `config/seelex.yaml`. Lowering the soft
  line moved the per-turn assembly gate while the per-tool-result controller kept folding at 75 % —
  one name, two sources, which is the "criteria and report disagree" failure this repo has now been
  bitten by twice. `NewContextWindowPolicy` takes the loaded `Limits` (normalised through
  `WithDefaults`, so a zero value falls back to the shipped ratios instead of yielding a 0 threshold
  that would fold every single round), and `policy()` now overrides only the account window and
  output when a `Budget` provider supplies them: rebuilding the policy there was silently dropping
  the configured ratios back to factory defaults. Tool output had the same gap from the other side —
  `seelebridge` built both the `ToolResultProcessor` and the controllers without `MaxToolResultChars`,
  so `limits.max_tool_result_chars` never reached the "is this result oversized" verdict (factory
  60000 always won). `seelexctx/controller_limits_test.go` pins all three facts: changing a ratio
  changes the numbers, a `Budget` override keeps them, an injected limit changes the verdict. Shipped
  defaults are bit-identical to the old constants, so an untouched config behaves exactly as before.
  `config/seelex.yaml` now states what each knob does and which way to turn it, and
  `seelexctx/README.md` gains a “阈值与上限的来源” section naming the injection points; two stale
  comments claiming the tool-result default was 20000 (it is 60000) are corrected.

- **`/compact` on a freshly cold-loaded session folds right away instead of telling you to
  send another message first.** Pressing `/compact` in a session that had just been
  cold-loaded (or just cleared) returned only
  *「已登记：下一条消息组装上下文前立即压缩」*; the user's next question — “I need your
  summary content” — ran after the command had already ended, so the promised fold looked
  like nothing happened. The gate read “no in-flight turn” as “nothing to fold”: the
  transcript and the engine history were both loaded, only the request epoch (a `RequestID`)
  was missing. The 2026-09-23 refusal to *fake* an epoch still holds (a real epoch would leak
  “someone is running this session” into `ChatState.Running` / the task registry / the snapshot
  `RequestID`), so `task_context` now lends the session a **session-level maintenance identity**
  (`session-maintenance:<sessionID>`, `StatusIdle`): `compactSessionContextWithoutEpoch` folds
  the loaded context through the same explicit path (criteria, record, frame body,
  engine-history replacement, per-session persistence), then revokes the identity — it never
  writes `ChatState.Running`, never sets the snapshot `Chat.RequestID` and never creates a
  task-registry entry. A session with genuinely nothing to fold (a new / just-cleared **empty**
  session) keeps the registered semantics, since folding an empty context would only produce an
  empty-range record — booking “did nothing” as “did something”. `ContextCompactionResult`
  gains `NoEpoch` (`no_epoch`, omitempty) so the shared `compactionRecordNote` — used by both
  the command and the tool — says why the fold landed immediately and still carries the frame
  `frame_ref`. Teeth: `TestCompactContextWithoutTaskExecutionCompactsImmediately`,
  `TestCompactWithoutEpochKeepsExecutionFacesClean` (no execution face is left behind and the
  identity is revoked) and `TestCompactCommandWithoutEpochFoldsImmediately`;
  `TestCompactEmptySessionRegistersAndRedeemsOnNextMessage` pins the unchanged empty-session
  register-and-redeem path. See
  [`docs/devlog/2026-09-24-cold-load-compact-immediate.md`](docs/devlog/2026-09-24-cold-load-compact-immediate.md).

- **The governance panel now reports *why* a round did not advance, instead of guessing from a
  wall clock.** `active · Round 0 · peer advisory_pending · governance stalled` was never a
  backend state: `goalCoordinator` stamped `heartbeat_at` on every advance and
  `app.js startGoalStallMonitor` printed `governance stalled` once `floor(now) > heartbeat_at + 10`
  — so an idle goal waiting for the user's next message lit up exactly like an aborted seat
  rotation. The guess existed because the fact had nowhere to go: the ADVISOR round is driven
  synchronously at the end of a turn, `Service.goalAdvanceAfterChat` discarded
  `AdvanceAfterChat`'s error, and `techleader.go`'s already-classified reason
  ("b 已作答但裁决不可用" vs "429/超时 → B4 缺席矩阵") never reached any projection.
  `GoalGovernanceView` therefore drops `heartbeat_at`/`heartbeat_seq` (their only readers were
  the stall heuristic and the `心跳 #N` decoration; no backend consumer ever existed) and gains
  `round_error`, which `AdvanceAfterChat`/`Next` write on failure and clear on the next
  successful advance or a new `Begin`. GUI and TUI render it as 「本轮治理未完成: …」 beside the
  existing 「断环」 banner, and the 1s timer keeps only its in-flight-snapshot duty
  (`startGoalInFlightPoller`), gated on `peer_state`/`in_flight`. Idle now shows nothing — idle
  is not an anomaly. Teeth: `TestGoalCoordinatorRoundErrorVisible` (failure visible, goal still
  active, cleared by a new goal) plus the `RoundError == ""` assertion on the happy path and a
  TUI panel assertion on the new line. See
  [`docs/devlog/2026-09-24-governance-round-error-replaces-heartbeat.md`](docs/devlog/2026-09-24-governance-round-error-replaces-heartbeat.md).

- **A `/compact` between turns now leaves a compaction record, so the receipt no longer
  says "no record this time".** The fold always happened; what was missing was the evidence.
  `task_context._RecordContextCompactionLocked` accepted a compaction only while the task
  execution was `Running` — and pressing `/compact` *after* a turn finished is the most
  natural way to use it. Every such request replaced the engine history with a bounded
  checkpoint frame, persisted it per session, and then produced no record: the receipt read
  `…故本次不留记录`, the status page's 「上下文压缩」 block stayed empty and the trajectory's
  compaction axis rendered nothing, which is exactly what "nothing is wired up" looks like
  from outside. The gate now keys on the record's **origin** (`ContextCompaction.Origin`:
  `auto` / `explicit` / the new `explicit_after_turn`) instead of only on `Running`: "the
  user or the model asked for this compaction right now" is auditable in its own right,
  while the automatic path keeps the old rule (never back-fill a record onto a finished
  turn — that would mislabel the turn as having compacted). The same function had a second
  hole: with no task face in the snapshot at all (cold-restore, then an explicit compact) the
  record lived only in memory, so widening the gate alone would still have shown nothing —
  the face is now rebuilt from the owning session. `CompactFoldedUnrecorded` survives as the
  honest mapping for a refused write (that request's execution face already replaced by a
  newer turn), but the explicit path cannot reach it in normal flow anymore. Teeth:
  `TestCompactManualAfterTurnRecordsExplicitOrigin` (origin + record + snapshot row +
  record-shaped receipt + `frame_ref` in the note),
  `TestCompactAfterTurnSurfacesRecordWithoutTaskFace` (no task face in the snapshot),
  `TestCompactCommandNeverReportsFoldWithoutRecord` (the sentence itself is banned: the
  notice must carry `已压缩上下文：`, `explicit_after_turn` and `帧正文 ref `, and must not
  carry `不留记录`), and `TestAutoCompactionAfterTurnKeepsRecordGate` (the automatic path
  must **still** not record after the turn — the only thing keeping the widened gate from
  being a blanket change).
- **A forked session can read back the compaction frames it inherited.** A session fork
  (`ForkSessionLatest` / `/fork`) prunes the child's tool-result registry to the refs
  reachable before the cut point, and compaction frame bodies
  (`ContextCompactions[].FrameRef`) were not in that set — so every inherited “context
  compaction” entry in the child session failed to open, even though the bytes were still
  in the content store (the read path rejects any ref absent from the child's refs index:
  `jsonRepository.ReadToolResult` returns not-exist). The reachability set now carries
  those frame refs, so it follows the records the fork actually inherits — the code says
  so explicitly, including what it does *not* claim: the rule is “follow the inherited
  records”, not “filter by time”, because compaction records are inherited wholesale
  (a conclusion later than the cut is dropped, the compaction history is kept).
  **Scope is declared in the code**: this is **session fork** only. **Subagent fork**
  (`fork_subagents`) lands in `subagent_<hash>/` with no blob directory of its own and
  large tool output refers to the main session's `big_tool_result` (storage design §8.2 /
  T-FK-05/06); that dispatch chain never calls `PrepareFork`, so it never runs
  `truncateForkRecord` and has no per-ref pruning step at all — changing subagent
  inheritance means changing the storage side (`sessionstore/fork_store.go`), not
  `application/core/session_runtime/fork.go`. Teeth:
  `TestForkSessionKeepsCompactionFrameRefReachable` (the inherited frame ref is present
  **and** the post-cut `result:later` is still pruned), plus the registry assertion in
  `TestPrepareForkTruncatesToRequestBoundary`.
- **The cold-start session row no longer disappears from the session tree after you
  switch away.** The assembly root allocated an early draft session ID at cold start
  (`service_assembler.go`) but never stored it in the process-singleton draft slot
  (`service.draft`), so the `Snapshot()` draft-row injection — gated on
  `service.draft != nil` — never fired for the “startup is a draft” session. That row
  was therefore shown only by the frontend's “current session fallback row”
  (`app.js renderSessions` unshifts the active session when it is absent from the
  catalog); as soon as the view switched to another session the fallback moved with it
  and the initial session had no source left. The cold-start path now records the same
  `draftSlot` that `BeginNewSession`/`resetViewToDraftAfterRestoreFailure` do, which also
  restores the other slot-keyed invariants for that session (idempotent `BeginNewSession`
  reuses the same ID instead of minting a new one, and the explicit-submit materialize
  path recognises it). Teeth: `TestColdStartDraftSlotIsRetainedAcrossSwitch` (red before
  the fix); `TestResidentLimitEvictsLeastRecentlyUsedIdle` now excludes the draft slot
  row, which is a separate data layer from the resident catalog.
- **`task.changed` increments carry the owning session key, so “this session only” no
  longer drops the row that just changed.** Increments come only from the live task
  registry, whose records carry no `SessionID`; the full-table path
  (`seelebridge taskSnapshotAll`) stamps `currentTaskSessionID`, but the single-row
  increment left it empty. The frontend replaces the whole row by `task_id`
  (`protocol.js`), so a row that had its key was overwritten by a keyless copy and then
  vanished under the session filter — exactly the reported “running subagent rows can't
  be found with the current-session filter”, since every subagent status transition emits
  an increment. `publishTaskChanged` now fills the key from the same attribution source as
  the full projection when the record has none. The frontend filter also treats an empty
  owner as the current view session in one shared predicate
  (`work-table.js rowBelongsToViewSession`, used by the filter, its counts and the “sent”
  axis) so a keyless legacy payload can never make a row disappear. Teeth:
  `TestTaskChangedIncrementCarriesOwningSession`, plus the frontend
  `session filter treats rows without an owning key as the view session` (both red
  before the fix).
- **Work-table refreshes are trailing-coalesced instead of re-rendering once per
  event.** `runtime.changed` / `worktable.changed` / `task.changed` each rebuilt the whole
  work table synchronously (batch tabs re-sorted, changed rows `replaceWith`-ed, new rows
  inserted with a scroll jump), so a task burst looked like the panel “kept jumping”.
  `app.js` now merges those three kinds over a ~120 ms trailing window and re-renders once
  with the latest snapshot (latest-wins; the render is a pure function of the snapshot, so
  dropped intermediate frames lose no information). Message/tool/interaction/team events
  stay immediate, and the applied-sequence watermark is untouched. Teeth: covered by the
  existing frontend suite (`node --test dist/*.test.mjs`, 433 pass), which pins the
  per-kind dispatch strings the buffering wraps.
- **`/compact` (and the `compact_context` tool) no longer refuse to compact, and the
  refusal notice no longer contradicts itself.** The explicit path still required
  `rawTokens ≥ soft threshold` even though it is the *user's* explicit request; worse,
  when the fold *did* happen but the compaction record was refused (the gate only let
  records through while the task execution was `Running` — the after-turn case is dealt
  with in the `explicit_after_turn` entry above), the
  caller saw `below_threshold` and the notice printed `state.TokenAudit.EstimatedPromptTokens`
  — the **assembled** request size, not the quantity the gate compared — producing
  sentences like “当前上下文估算 129409 tokens，未达压缩阈值 118962”. Now: the explicit path
  folds unconditionally (`forceCompact` is a third fold criterion next to soft/hard
  threshold — a cold-loaded session with loaded material now folds immediately through a
  session-level maintenance identity, see the entry above), the decision facts
  (`ComparedTokens` / `AssembledTokens` / both thresholds) travel back in
  `CompactResult`, and the notice says what actually happened: compacted+recorded,
  folded-but-refused-a-record or scheduled. A session that is genuinely **empty**
  (just cold-loaded with no messages / just cleared) is registered and honoured on the
  next context assembly (`ScheduleForceCompact`), so the compaction lands in the very
  next message. Teeth: `TestCompactManualFoldsBelowThreshold` turns red when the explicit
  path is put back behind the soft threshold;
  `TestCompactCommandNeverReportsFoldWithoutRecord` keeps the after-turn behaviour pinned.
- **The `/compact` notice no longer prints a record-shaped sentence when no record
  exists.** `CompactFoldedUnrecorded` sets `Compacted` (the fold really happened) but
  deliberately leaves `Reason` and `MessagesBefore` unset, and the command only checked
  `Compacted` — so a fold whose record was suppressed printed
  `已压缩上下文：v3（），压缩前 0 条消息 / 估算 32295 tokens`: an empty reason and
  "0 messages before" for a compaction that had just folded the transcript, with the
  outcome's own `Note` (which says the fold happened and why no record was written)
  thrown away. The command now emits the record line only when `Recorded` is true and
  otherwise returns the outcome's `Note` — the same wording the `compact_context` tool
  already used. Teeth: `TestCompactCommandNeverReportsFoldWithoutRecord` — in the final
  shape of this batch the explicit path always gets its record, so the test pins both halves
  (the notice is record-shaped **and** the `不留记录` wording can never come back); the
  `Note` fallback still carries the automatic path's refused-record case.
- **The `/compact` record line now reports the folded range, not an engine-history
  count.** `MessagesBefore` is `len(engineHistory(sessionID))` at assembly time — engine
  messages (system rows included), legitimately **0** for a cold-loaded/routed session —
  so the same sentence could print `压缩前 0 条消息` for a compaction that had just folded
  `message-1..message-103`, and `压缩前 2 条消息` when the only engine rows were system
  prompts. The record already carries the exact boundaries (`message_from/to`,
  `event_from/to`), so `compactionRangeLabel` renders them —
  `已压缩上下文：v3（context_budget），被压区间 消息 message-1..message-103 / 事件 1..6，估算 4863 tokens`;
  an empty range is skipped rather than back-filled with a count. `messages_before` stays
  in the record/JSON as diagnostic metadata and is now documented as such in
  `application/model/state.go`. Teeth: `TestCompactCommandNoticeReportsFoldedRange`
  (reprinting the old `压缩前 %d 条消息` shape turns it red) plus the table-driven
  `TestCompactionRangeLabel`.
- **The agent-facing skill catalog told the model to use the wrong input prefix.** The
  passive `## Available Skills` hint read “ask the user to send `#<name>`”, but `#` has
  been the *plugin* sigil since the 2026-09-17 one-sigil-one-meaning change (`$` recalls a
  skill) — the hint was never updated, so the model kept teaching users a prefix that
  switches plugins. The hint now takes the sigil from the core sigil table
  (`prompt_layer.Deps.SkillSigil` ← `core.SigilSkill`), the shipped
  `plugins/default/plugin.md` / `README.md` say `$plan` instead of `#plan`, the
  `manual_smoke_test.go` smoke submits `$goal`/`$plan`, and two guards keep it that way:
  `TestRenderSkillCatalogHintUsesInjectedSigil` (unit) plus
  `TestPluginDocsDoNotUsePluginSigilForSkills` (plugin docs must not use `#<skill>`; it
  fails on the historical `#plan` text).
- **`/help` and the unknown-command path no longer dead-end.** The `/` palette is the
  “full entry” and lists **tools** next to commands and skills, but the submit path only
  executes commands and skills — picking `/compact_context` produced a bare “未知命令”.
  `unknownCommandNotice` now names the tool case (“it is a model-side tool, it cannot be
  executed from the input box”) and points at the matching command (`/compact`), and
  performs a near-miss search (prefix/contains/edit-distance ≤ 2) for typos like
  `/comapct`; `/help` documents the same contract.
- `durable_queue_wire_test.go` was never assigned to a core README volume, so
  `scripts/gen_core_readme_index.py` had been failing (and the per-volume index silently
  stale) since it was added. It is mapped to the `input` volume and the index is refreshed.

### Added

- **A compression round now reports its gates while it runs.** New session-routed event kind
  `compaction.progress` (`application/core/context_runtime/compaction_progress.go`): a
  **begin frame** (`phase=begin`, `index=0`, `gate=judge`) followed by one frame per gate in
  the authoritative order `judge → assemble → replace → frame → store → record`, then exactly
  one terminal (`done` / `failed`, `reached=n/6`). Each frame carries `elapsed_ms` = the
  wall clock of **the segment that just ended** (begin carries none), so the frontend can sum
  the frames for a total and still see which gate was slow. The GUI right column draws it as
  a bar on the existing plan-board track (`.plan-board-progress`, `role="progressbar"`) plus a
  per-gate duration checklist; the round is transient — `revision=0`, never enters the
  snapshot, is deliberately **not** in the 120 ms buffered-incremental set (coalescing would
  drop gates), and the bar's whole lifetime is one round (kept 2.5 s after `done`, 6 s after
  `failed`). Why the begin frame exists: the explicit path's first gate is the round's long
  wait (two whole-request token estimates), so without it the panel is blank for the entire
  expensive phase and then shows a finished record. The automatic path gets **no** begin
  frame on purpose — whether to fold *is* the outcome of that estimate, so announcing intent
  early would lie in the rounds that do not fold; "no fold, no progress" stays true.
  Measured today on this machine over 30 rounds of `TestExplicitCompactGateTimeline`
  (4×160 KB fixture): judge **31–64 ms** (median 40), assemble 6–18 ms (median 11), replace
  ≤1 ms, frame 0 ms in 29 of 30 (one 54 ms scheduling outlier), store and record 0 ms in all
  30; the whole `CompactContextNow` window is 40–121 ms. Those ranges are *not* a property of
  the code: the same fixture on the same machine read judge 26 ms under `-count=1` and
  31–64 ms under `-count=30` minutes apart, and an earlier sample read 46–90 ms. The numbers
  are load; the contract is the **shape** — one begin frame, six gates in order, per-segment
  durations that together fit inside the round. Teeth:
  `TestExplicitCompactEmitsOrderedProgressGates` (six gates in order, one terminal),
  `TestExplicitCompactGateTimeline` (the summed per-segment timings must fit inside the round
  window; re-run PROBE today: not advancing the timing base — i.e. reporting cumulatives — is
  red in 3 of 3 rounds at 913–2049 ms against a 138–299 ms window, and the same test goes red
  if a second emitter mixes into the round, which breaks the gate sequence),
  `assertBeginFrame` (begin is first, `index=0`, version 0 = "not yet determined", never
  invents a duration), `TestAutoCompactionEmitsProgressGates` (automatic path has no begin
  frame), `TestNoProgressEventsWithoutFold`, `TestCompactProgressTerminatesOnAssemblyError`
  (a mid-round error must still close the bar — half a progress bar is worse than none),
  `TestFrontendGateLabelsMatchBackendOrder` (the JS label table is pinned to the Go gate
  list, across languages), `TestBridgeRelaysCompactionProgressToRenderer` (the GUI relay does
  not swallow it) and the frontend suite (`a begin frame opens a round with nothing counted
  and nothing timed`, `a new round never inherits the previous round's checklist`,
  `the begin frame never becomes a checklist row of its own`,
  `durations below a millisecond say so instead of claiming zero`,
  `dispatches compaction.progress with its payload to the view`). Not done here: the TUI does
  not consume this event yet.
- **A compaction record now carries a readable frame body.** `ContextCompaction` gained
  `frame_ref` / `frame_bytes` / `frame_tokens`: the bounded checkpoint frame written at the
  moment of the fold goes into the session content store through the same tool-result
  channel, and the record keeps only the reference — the snapshot never grows a copy of the
  body. The status page's 「上下文压缩」 entries and the trajectory axis detail both gained a
  查看帧正文 / 收起帧正文 toggle that pages the body back through
  `Bridge.ToolResultContent` (`app.js` reads 12 000 bytes at a time, reusing the trajectory
  detail container and its pagination component instead of a second one). Records written
  before this change have no ref, so their entry simply renders no toggle. Teeth:
  `TestCompactionFrameBodyIsReadableByRef` (the ref in the record reads back the frame,
  including the `origin:` line and a matching `TotalBytes`),
  `TestCompactionFrameBodyReportsFoldedRangeAndInjection` and
  `TestCompactionFrameBodyAdmitsMissingEvidence` (the body states what it does not have
  instead of inventing it), plus `renders compaction records with range, origin and a frame
  entry` and `axis detail renders compaction range, origin and read-back entry`.
- **Computer use can now see which panels scroll, and the wheel reports where it landed.**
  `computer_scroll` could only push `WHEEL_DELTA` at a coordinate, so the model had no way to
  tell *which* panel would move or whether there was more context off-screen. Two additions close
  that gap. A new read-only `computer_scroll_targets` tool enumerates the scrollable panels of a
  window through **UI Automation** (`ScrollPattern`): readable name, control type, class,
  rectangle, center point, and per-axis scroll position/viewport ratio with explicit
  `at_start`/`at_end` flags — so "is there anything below?" is answered before scrolling.
  `computer_scroll` now accepts `window` (focus + target that window's largest vertically
  scrollable panel), splits the delta into per-notch `SendInput` wheel events (large jumps are
  silently clamped by Chromium/Electron hosts), and reads the panel position **before and after**
  scrolling so the result distinguishes "scrolled" from "already at the end" (a failed read-back
  is reported as `unavailable: ["scroll_panel"]` instead of being invented). The UI Automation
  client is hand-written COM (`uia_windows.go`, slots pinned to the Windows SDK
  `UIAutomationClient.h`), read-only, bounded (node budget + depth + timeout) and apartment-safe
  (MTA per query on a locked OS thread). Because Chromium materialises its accessibility tree
  lazily, the query falls back to a bounded raw-view walk whenever `FindAll` fails to surface a
  panel covering ≥20% of the window (VS Code's fast path returns only Monaco list rows), and
  merges both batches; sub-panel-sized elements (<≈64×64 px) are dropped so virtualised list
  rows do not flood the list. Verified on a real desktop against Edge, Qoder, VS Code and the
  Seelex GUI window, including a wheel round-trip on Qoder's message list (100.439% → 97.251%
  with `delta=+120`, exactly back with `delta=-120`). The same capability ships on the MCP face
  as `scroll_targets`.

- **Uncommitted message tails are now recovered at session load, and the user can see
  it.** The storage layer already had the explicit probe/restore/discard entry points for
  the `seq_draft` tail (`sessionstore/pending_tail.go` → Router → `SessionPort`), but the
  application side had **no caller**: recovery was a port capability, not a behaviour. Cold
  load now runs `probe → decide → restore|discard` **before** the record/history/transcript
  reads (visibility is gated by the publish point, so recovering first is what makes the
  restored rows land in this same load) and reports the outcome as a visible `system` row:
  `recoverable` publishes and says how many rows came back (with the publish-point move),
  `gap` only reports — nothing is published or cleaned (red line 3). A session whose turn is
  still running is not touched at all, so load can never fight the in-flight writer over the
  draft tail. New optional capability `session_runtime.SessionPendingTailPort` + pure DTO
  `dto.PendingMessageTailReport` (the adapter maps, so no layer below the adapter leaks
  storage types). Tests: `application/core/session_pending_tail_test.go`.
- **C1/H3 cost breakdown probe and write-side "no whole-shard decode" commit path.**
  `-tags lockprobe ./sessionstore -run TestProbeCommitCostBreakdown` splits a commit into
  its phases and runs an in-run before/after A/B. Measured on 1500 rows × 8 KB (last shard
  834 KB): **full head rewrite is 1.3–1.6 ms** (so the checklist's premise that "head is
  rewritten in full on every commit" is the dominant cost is **wrong**), whole-shard
  **read + JSON decode is 15–27 ms**, whole-shard sha256 2.5–3.3 ms, append + `Sync`
  1.2–1.5 ms; head size is linear in shard count (826 B @1 → 19.8 KB @80, ≈247 B/shard).
  Commits were paying that whole-shard decode **twice** (reap and the shard writer). The
  writer now takes the tail shard's row count/range from the head index when it can be
  trusted (`shardInfo.Bytes` + byte count match + newline terminator + index end == publish
  point) and computes the digest incrementally from the bytes it already read; otherwise it
  falls back to the old whole-shard read, so the only failure mode is "slower". A/B:
  whole-shard decodes per commit **3 → 1** (100-row commits; 2 → 0 for single-row commits),
  commit median **96.6 ms → 88.0 ms**.

### Added

- **The four items the prefix-chain design left open are implemented, with instrumentation
  points, and the three gates (live smoke / `-race` / pprof lock contention) are green.**
  `docs/arch/context-prefix-chain.md` described them under 《待落地（目标设计）》; the design
  came first and the code matched it, so that section is now 《已落地与已决定不改》 with the
  implementation site, the **instrumentation site** and the verification for each item.

  - **Protection floor (`limits.context_retain_floor_percent`, default 0 = unconfigured).**
    The retained prefix was `min(token1, token2)` clamped only to `[1, all_context]`: a small
    `window.retain_tokens` (or a small `window.ratio`) could squeeze the protected window down
    to a single unit, and the failure mode was "the model forgot". The decision is now
    `retained = clamp(min(token1, token2), floor, token1)` with
    `floor = max(latest complete protocol unit, percent × budget)`
    (`seelexctx.RetainedContextTokensWithFloor` / `RetainFloorTokens`). The invalid combination
    `floor > retain_tokens` is an **error**, not a silent `min`: `main.initRuntime` validates it
    right after the Runtime exists (account window known, budget derived by the same formula the
    compaction criteria use) and refuses to start; out-of-range percentage knobs
    (`context_soft/hard/target/single_item/retain_floor_percent`) are rejected by
    `seelexctx.LoadLimits` instead of silently falling back to the default.
  - **Frame-summary carry cap (`limits.context_frame_carry_tokens`, default 1024).** Local
    folding merged the previous frame's Chapter 2 body verbatim, and **that body had no length
    limit at all** — yet it is the only frame content that enters the model context and the
    cache prefix, so the stack top grew with every frame. The merge now goes through
    `seelexctx.CarryPreviousChapter2` (shared by the controller, the DAG and the vacuum-gap
    path): within the cap the body is carried verbatim, beyond it the body degrades to an
    **anchor** (`segment_id` + request range + one-line summary + why the body is gone), with
    `search_history` / `read_compressed_turn` for detail.
  - **Chunked replay chain.** `seelexctx.ChunkReplayMessages` splits the overflow region on
    **protocol-unit boundaries** (a tool chain is never cut; a single unit over budget gets its
    own chunk) and `SummarizeChunkPlan` replays chunk by chunk, forwarding each chunk's summary
    into the next chunk's instruction tail (no change to the summarizer contract). The chunk
    budget comes from `CompactionDAGOptions.ReplayInputTokens`; the chunked path is started only
    when the overflow region cannot be sent in one request, and any chunk failing exits the whole
    chain so the caller falls back to the deterministic local fold.
  - **Four-zone layout made explicit.** `application/core/context_runtime/layout.go` defines
    `ContextLayout` / `ContextZone` (① `stable_prefix` ② `folded` ③ `protected_window`
    ④ `tail` ⑤ `current_input`, each with tokens, message count and source). Criteria and report
    read **the same** layout, sampled once per assembly, so the numbers the user sees and the
    numbers the fold was decided on cannot drift apart. Zone classification uses facts of the
    messages themselves (role + prefix markers), never positional inference; what this layer
    cannot see (the project/memory/compact stack blocks rendered downstream by the framework
    assembler) is documented as a lower bound on ① rather than papered over.

  **Instrumentation sites:** the judge gate now reports
  `all= budget= cap= ratio= floor= retained= floor_applied=` (with `floor_applied` true only
  when the floor actually raised the result); the assemble gate reports the five zone token
  counts; the compaction frame body (readable by `result_ref`) gained a
  `## Context zones (四区)` block carrying the zones, their sources, the criteria and the retain
  decision. The carry decision lands in `CompactFrame.Evidence` as
  `frame-carry:{kept|anchor}:<carried>/<limit>` and flips `anchor_source=degraded` when it
  degrades; a chunked replay lands as `replay-chunked:<n>` (nothing is written when the replay
  was not chunked, so the report never carries an "1 chunk" non-fact).

  **Verification:** `seelexctx/{window_floor,frame_carry,replay_chunk,contention_gate}_test.go`,
  `application/core/context_runtime/layout_test.go`, `application/core/context_retain_floor_test.go`
  (the floor raises the protected window from 1 to 4 settled rounds end-to-end; the frame body
  carries the zones), the live smoke `TestPrefixChainRetainFloorLiveSmoke` (real provider:
  baseline `retained=1361 floor=0` → floored `retained=16476 floor=16476`, `floor_applied`
  flips, zones and gate details observed on the wire), `go test -race` across
  `seelexctx/... application/core/... application/event/... seelebridge/... .`, and
  `TestPrefixChainLockContentionGate` (pprof mutex/block: the changed pure functions record
  **zero** user-level lock contention; in the shared-compact-stack arm every contention sample
  blocks at the pre-existing `memoryCompactStack` locks, never at a changed symbol; profile text
  dumped to `tmp/lock-contention-gate/`). The one `-race` failure seen —
  `TestWorkspaceSwitchConcurrentWithBackgroundPersist` — reproduces **2/8 on the pre-change
  baseline** (Windows `TempDir` cleanup racing a background persist) and is unrelated; the gate
  runs with `-skip` for it.

### Changed

- **`-tags redprobe ./sessionstore` is green again: the failure was a runtime failure of a
  retired probe, not a build failure.** `TestProbeStackStatusUpdateUnpublishedInvisible`
  pinned "an unpublished status change must stay invisible after a cold reload", which
  assumed `active.jsonl` was an append-type channel gated by a stamp. That reading was
  withdrawn by D12 / the §2.0 channel-type table (whole-replacement: the file has no
  "appended but unpublished" state at all), so the probe was pinning a withdrawn contract
  and had been red since; the checklist already claimed it had been "converted to the T-STK-13
  semantic assertion", which it had not. The probe is retired with its provenance recorded
  and the semantics stay covered by the existing `TestStackChannelActiveWholeReplacement`
  (T-STK-13, `channel_semantics_test.go`).
- `snapshotFailingSessions` (A1's durable-queue test double) now embeds
  `*queueRecordingSessions` instead of by value: `go vet ./...` flagged the by-value mutex
  it copied into every method.

### Removed

- **Retracted the composer "unsent-input draft", which had conflated the input box
  with a session identity.** An unmaterialized "draft session" that only held the
  composer's unsent text used to appear as a session row while typing, persist that
  text through a lifecycle draft channel, and be restored into the view on startup.
  The product meaning of a draft is instead an **unfinished session**: a dispatched
  task that was interrupted (shutdown / exhausted quota) and must be resumable after
  restart or retry. Removed `SaveComposerDraft` (core + `Bridge`), `composer_draft.go`
  (persist/restore/clear, `DraftCandidates`), the GUI draft row and
  `draft-lifecycle.js`, the sessionstore/adapters lifecycle draft API
  (`SaveComposerDraftWorkspace` / `LoadComposerDraftWorkspace` / `setComposerDraft` /
  `lifecycleDraft`), and the feature's tests and devlogs. `SessionRecord.Composer`,
  `SessionState.Composer` and `model.ComposerDraft` stay as inert fields for on-disk
  compatibility. The `draft_<nanos>_<seq>` early allocation for a new session is
  unchanged and is the subject of the follow-up "draft = unfinished session" design
  (see `docs/devlog/2026-09-22-draft-semantics-retraction.md`).
- **The user no longer holds a seat in the team speaking ring.** `order_roles` still
  carries `user` (it is the chat's opening and closing turn, and `resolveOrderRoles`
  requires it), but the ring a session runs on is now `order_roles − user`: a teammate's
  "next speaker" can never point at the human, and the user speaks only through the
  queue promotion at the end of a react loop (`application/core/chat.go`). The old
  three-state "user seat" (`queued` / `member` / `absent`, derived from `order_policy`)
  was removed because all three states assumed user occupied a ring slot — that is
  exactly what made "next speaker" resolve to the human. Removed
  `dto.TeamSchedule.UserSeat` and `dto.UserSeatQueued/Member/Absent`,
  `agentteam.UserSeatPolicy` (`UserSeatPolicyFor`, `SetUserSeat`,
  `RuntimeOptions.UserSeat`), `Runtime.NoteUserQueued`, `Service.NoteTeamUserQueued`,
  `noteTeamUserSeat` and their call sites, plus the GUI "user 席位" chip and the TUI
  schedule line (see `docs/devlog/2026-09-22-agentteam-ring-excludes-user.md`).
- **Tools are no longer offered by the `/` panel, which now lists only what the input box
  can submit.** The palette used to append model-side tools next to commands and skills, so
  a user could pick `compact_context` from a menu whose entire purpose is "type this and
  run it" and get an unknown-command error. A capability gets a `/name` entry only once it
  is registered as a command; otherwise it is a tool, visible to the model and callable by
  the model behind the permission gate, and it stays out of both suggestions and routing
  (`/` never had a tool branch). Removed `SuggestionKindTool`, `Service.toolSuggestions()`
  and the `toolAliases` fold-in table, dropped the tool-only `Executable` marker from the
  `Suggestion` DTO, and re-ranked the sort so commands and skills lead. Discovery is
  unaffected: the unknown-command notice still names `compact_context` as a model-side tool
  and points at its command counterpart `/compact`, and `/help` says the same. Teeth:
  `TestSuggestionsExcludeModelSideTools` pins both sides (the tool names are absent **and**
  `/compact` is present), so deleting tools from the panel cannot silently delete the only
  way to find the compaction entry.

### Added

- **The resource explorer now has a workspace-changes pane, so uncommitted work is
  visible without leaving the workbench.** The pane lists staged / unstaged /
  untracked / conflicted entries (status letter, path, rename original path, staged
  marker, plus a `branch · staged x · unstaged y · untracked z` summary) from
  `Bridge.WorkspaceChanges`, and it is read-only metadata: no diff, no patch, no file
  content ever leaves the backend. Backend
  `workspace.GitChanges(root, limit)` runs a fixed argv
  (`git --no-optional-locks -C <root> status --porcelain=v1 -b -z -uall -- .`, 8s
  timeout) — `-z` because the default porcelain output C-escapes non-ASCII paths
  (`\344\270...`), which the renderer would have to unescape, while `-z` returns them
  verbatim and emits a rename as "new path NUL old path"; `-- .` because
  `-C <subdir> status` otherwise reports changes outside the bound workspace. Two
  differences are settled once in the workspace layer instead of in each consumer:
  paths are rebased from the **repository root** to the **workspace root**
  (`rev-parse --show-toplevel`, siblings dropped and counted), and the visibility
  boundary is the same one ListTree/ReadFile already enforce (sensitive names such as
  `accounts.yaml` / `*.local.yaml` are hidden and counted in `Result.Filtered`, which
  the pane states explicitly rather than dropping silently). Directory noise is left to
  git's own `.gitignore` on purpose — reusing `ignoreDirNames` (`node_modules`/`dist`/…)
  would also hide genuinely tracked `dist/` changes. `Total` and the four counters
  cover all post-filter entries including the ones `limit` cut, so the header describes
  the workspace rather than the rendered rows. The porcelain XY is classified exactly
  once (`dto.Change*`, staged side wins), and a non-git directory returns
  `Result.Error` with the git message instead of a Go error.
  Regression: `workspace/gitchanges_test.go` (15 cases incl. real-git integration:
  modify/stage/delete/`git mv`/untracked/Chinese names, subdirectory root rebasing,
  sensitive-name filtering, limit truncation, non-git `Result.Error`),
  `application/core/workspace_tree_usecase_test.go`,
  `gui/bridge_test.go`, `gui/frontend/dist/workspace-changes.test.mjs` (7 cases).
  Recorded on 2026-09-20 in `docs/devlog/2026-09-20-workspace-changes-pane.md`;
  contract documented in `docs/gui/modules/right-sidebar.md` and `workspace/README.md`.

- **The resource explorer's three code panes became three sibling sub-pages, each with its
  own scroll container and a refresh button.** Work tree / commit log / workspace changes no
  longer stack vertically: one embedded tab strip (`role="tablist"`) shows a single sub-page
  at a time, the active page and order persist in `seelex.right.explorer.v1` (the old
  `seelex.right.codePanes` order is migrated once, then removed), and every sub-page head
  carries a refresh button — re-activating the explorer sub-page or clicking its tab again
  re-reads the data as well. The refresh itself is an atomic capability
  (`gui/frontend/dist/explorer-refresh.js`): single-flight (a trigger while a refresh is in
  flight reuses the same promise) plus generation/root-stamped commits, so a stale response
  can never mix old and new rows across the three panes, and a failed refresh keeps the
  previous data and only reports. Page semantics (order/active/dirty storage) are pure
  functions in `explorer-pages.js`; `app.js` only renders what they return. Recorded in
  `docs/devlog/2026-09-22-explorer-subpages-refresh.md`.
- **A session's unsent input is now part of the page it belongs to.** The conversation page
  renders the established messages plus one trailing unsent-draft row (`chat:draft`,
  `kind="draft"`, `is-draft`, `data-draft`/`data-unsent`) whenever the current page still
  holds text that was never submitted. Previously that text only existed in the composer, so
  a draft session (which has no messages yet) opened as an empty page and the unsent half of
  the page context was invisible after a switch or a restart. The rule set lives in
  `gui/frontend/dist/draft-lifecycle.js` (`draftLifecycleFromSnapshot`, `draftLifecycle`,
  `composerDraftRows`, `draftRoundEvent`) and is wired once in `app.js`: append on edit,
  `submit` with the `clearSubmittedText` remainder, `materialize` when a round settles,
  `cancel` when the user stops it. The row is a projection: it never writes back to the
  composer. Boundary: a persisted draft is still not restored into the page when the app
  boots with an existing session (`initialDraft=false`), so the new headless smoke records
  that state instead of asserting it. Verified end-to-end by
  `composer_draft_live_smoke_test.go` (`-tags draftsmoke`): the unsent draft never reaches
  the provider, adjacent wire requests stay prefix-identical with the draft in play, history
  survives a restart, and a cancelled round's settled history survives both the cancel and
  the restart.
- **Agent Team role sessions show unsynchronised role drafts in their own block instead of
  folding them into the record table's `is-own` cells.** `renderRoleDraftBlock` lists
  `draft_rows` after the published rows, keyed by round/unit, each row carrying `is-draft`
  plus an "未同步" chip and `data-draft-round/-unit/-kind` credentials; the record table marks
  the matching column head and cell as draft (`.role-record-draft-head`,
  `.role-record-cell.is-draft`). The end of a round is the only place drafts become published
  messages (`SyncRoleDraft` in `sessionstore/role_session.go`: sort → idempotent replay check
  → atomic head+floor publish → delete the draft file), so a cancelled round keeps its rows
  as drafts and the frontend only renders that authoritative projection. Regression tests:
  `gui/frontend/dist/agent-team-view.test.mjs`,
  `application/core/goal_team_recorder_test.go`, `sessionstore/role_session_test.go`.

### Changed

- **The vacuum-region boundary is now reported by the tail-window selector instead of being
  re-derived by the coverer.** `seelexctx.GapCoverageOptions` used to receive `TailEvents` and
  compute the uncovered range as `len(CompleteEventUnits(all)) - len(CompleteEventUnits(tail)) - 1`
  — two separate unit spaces, which the code itself flagged as possibly "slightly misaligned" and
  which therefore needed a conservative clamp plus a post-hoc "skip when the new frame does not
  advance coverage" dedup. `selectEventTailWindow` now returns the index of the first selected
  unit **in the full stream's unit space**, `sessionstore.GapCoverer` carries that index instead of
  the tail events, and `CoverHistoryGap` computes `[top.To+1, tailStartUnit-1]` directly. The clamp
  and the dedup are gone: `gapStart > gapEnd` now provably covers the repeat case, and
  `TestCoverHistoryGapRestoresCoverageContiguity` asserts the invariant itself (after coverage the
  stack top must abut the window, `top.To == tailStartUnit-1`). Because both indices now arrive
  from outside the function, the unit-space contract is checked up front: a `tail_start_unit`
  outside `[0, unit count]` or a stack top `To < -1` returns an error instead of reaching the
  slice expression — the deleted clamp was what used to keep that second case from panicking on a
  corrupted state blob. `TestCoverHistoryGapRejectsInputsOutsideUnitSpace` covers both the rejected
  values and the still-valid boundary values (`tailStartUnit == unit count`, `To == -1`). `application/core` reads the same
  source for its cold-restore read width — the transcript tail cap is `window.min_rounds` instead
  of a second literal `4`. The wire assembly path still passes its own `3`: that number has no
  documented rationale, so it was left alone rather than guessed at. Covered by
  `seelexctx/gap_test.go` and `sessionstore/durable_history_test.go`; full suite green on
  `go test ./... -count=1` (67 packages, 0 failures) and `go build -tags "gui,desktop,production" ./...`.

- **Reading a session's history no longer blocks the writer, and a concurrent publish can no
  longer make a read fail.** `readRows` / `readTailRowsForSelection` used to hold `messageMu`
  across the whole decode, so the lock profile showed 66% of the lock delays as *writers waiting
  for readers* (one full history read made a commit wait 91.7 ms against a 30 ms budget, baseline
  63.7 ms). The read path now takes a single **lock-free head snapshot** and decodes **outside**
  the lock (`storeEngine.withPublishedHead`): the snapshot is taken once, decode runs against it,
  and a decode that fails because a concurrently cleaned shard vanished under it is retried once
  with a fresh snapshot (coordinates unchanged means real corruption and is reported as-is).
  `readRowsLocked` stays for write-side callers (LRU delete, compaction, fork pre-read) and
  `verifyMessage` keeps the exclusive lock, because its judgement is exactly "head and shards
  frozen at the same instant" (sharing a lock-free snapshot with a concurrent cleanup would
  report healthy deletions as corruption). Visibility is snapshot-consistent (MVCC-style stale
  read); rows above the snapshot's `last_seq` stay invisible, so the draft tail can never leak.
  Teeth: putting the decode back inside the lock turns `TestMessageReadDecodeOutsideWriterLock`
  red (10 s timeout, the commit never finishes) and `TestCommitNotBlockedByFullHistoryRead` red
  (extra wait 72.6 ms against the 30 ms budget).
- **Windows readers of published files now open them with `FILE_SHARE_DELETE`.** Go's
  `os.ReadFile`/`os.OpenFile` always use `share=READ|WRITE` without `DELETE`, which stayed
  invisible while reads shared `messageMu` with the writer: heads are published by an atomic
  rename (`writeAtomic`), and shards are deleted by LRU eviction / reap / compaction rewrite.
  Once the read path became lock-free, `go test ./...` caught the reader losing that race — the
  root package's `TestStorageConcurrentSessionLockProfile` failed inside `LoadEventTail` with
  `metadata/message.json: The process cannot access the file because it is being used by another
  process` (`ERROR_SHARING_VIOLATION`), while the same test passes on the parent commit.
  Measured on one file over 4 s: 141 of 5248 default-mode reads failed while a writer looped
  temp+rename, and two `O_RDWR` readers made 2077 of 2192 eviction deletes fail — with the share
  bit, 0 of 9130 reads failed and every delete succeeded. `openSharedRead` / `readSharedFile`
  (`sessionstore/file_shared_read_windows.go`, `..._other.go`) are wired into the five lock-free
  read sites (`readModuleHeadFileRaw` for every module head, `readHeadEnvelopeLenient`,
  `readMetaFromDir` for directory enumeration, `readMessageRowsFileAt`, `scanShardUserInputs`),
  keeping the `*fs.PathError` shape so `errors.Is(err, fs.ErrNotExist)` is unchanged. Renaming
  over a target still requires that no handle is open (`ERROR_ACCESS_DENIED`), so `writeAtomic`'s
  backoff stays; this batch only removes the reader-side failure and the reader blocking deletes.

### Fixed

- **LRU prefix eviction could delete the shard it had just published, silently emptying a
  session's history.** Small shards are named after the surviving row range, so evicting 1..20
  out of 30 rows rewrites the survivors as `message_21_30.jsonl` — the same name as the old last
  shard, which the cleanup loop then removed by name. A missing shard reads as "no rows" (the LRU
  tolerance in `readMessageRowsFileAt`), so `head.TotalRows` said 10 while the reader saw 0, with
  no error anywhere. The cleanup now skips files published in the same pass;
  `TestRetentionPrefixEvictionKeepsRewrittenShard` pins the three judgements (every shard in
  `head.Shards` exists, visible rows equal `head.TotalRows`, `verifyMessage` passes) and turns red
  with `head 引用的分片不存在: message_21_30.jsonl` as soon as the guard is dropped.
- **A session can no longer self-lock while injecting a TL/ADVISOR directive at a
  tool-iteration boundary.** The `OnIterationComplete` hook runs synchronously
  inside `session.Session.ChatStream`, which holds the framework session mutex for
  the whole stream, and `GoalIterationCompleted` used to drain the pending TL
  directives and append them to the engine history right there — `AppendHistory`
  takes that same mutex, so the goroutine blocked on itself. The round never
  finished: the session stayed "running" forever, a message queued while it ran
  was never promoted and never sent, and the task could not be cancelled or
  closed either (cancellation only cancels the context; the goroutine was stuck on
  a mutex). Triggering shape: a goal/Agent-Team session whose queued input is
  promoted into the next round — the promotion path goes straight to `runChat`
  and skips `startChatFor`. The boundary hook now only registers
  `turn_completed`, and the trusted injection happens at the documented safe
  point ("before the next ChatStream") for every round, promoted rounds included
  (`injectGoalDirectivesForStart` from `runChat` as well). The visible replay of
  the verdict (assistant row with `role_name=tl`) is unchanged, as is the timing
  of the trusted injection. `contract.ChatEngine.AppendHistory`'s comment now
  states the real rule (never call it from loop callbacks). Regression:
  `application/core/goal_directive_session_lock_test.go` (Seele v0.3.0 lock
  discipline stub + hard timeout + goroutine dump); see
  `docs/devlog/2026-09-23-iteration-hook-session-lock-reentry.md`.

- **A session's draft input is no longer invisible in the session tree, and materialising it
  no longer leaves a ghost draft behind.** `SaveComposerDraft` wrote the record and the
  in-memory unit but never registered the draft slot, while the "a draft row is always
  `draft`" rule only applied to slot rows — so the very session the user was typing in was
  rendered as `idle`. The first non-empty draft now registers the slot, which is the same
  criterion the record uses (`Status=draft` only while the text is non-empty).
  `clearComposerDraft` also converges on **residue** instead of the single project key it
  happens to remember: rebinding a draft to another workspace (`BindWorkspace` after
  `BeginNewSession`) used to leave a `Status=draft` record under the old project, and
  `DraftCandidates` — which enumerates every project — then offered it as a restore
  candidate after a restart, handing an already-materialised session back to the page as a
  draft with stale text. Covered by `TestComposerDraftSessionRowVisible` and
  `TestComposerWorkspaceRebindConvergesOnMaterialize`. Real-API sessions restore smoke
  (`-tags manualsmoke2`) and the prefix smoke (`-tags manualsmoke`, 9 requests / every
  adjacent pair prefix-identical / 92.5–96.8% prompt-cache hit) were re-run green.

- **A provider request can no longer carry a `tool` message that no assistant
  declared, so the session loop survives the 400 instead of dying on it.** The
  observed failure (twice, on two different account roles):
  `session loop 15: seelebridge: stream with account "agent-1": ChatClient
  stream: HTTP 400: {"error":{"message":"Messages with role 'tool' must be a
  response to a preceding message with 'tool_calls'"}}` — a provider validates
  that **every `tool` message immediately follows the assistant message that
  declares its `tool_call_id`**, not that the history merely contains a pair.
  Three shapes violate it: an orphan result (no assistant ever declared it), a
  misordered result (a `user` message between declaration and result), and a
  duplicate result. The framework-side twin of the pairing repair
  (`seelexctx.repairInterruptedToolChains`) only *added* placeholders for
  missing results — it never dropped orphans nor moved misordered ones — while
  the application-side twin
  (`context_runtime.RepairInterruptedToolChains`) had done both since the
  2026-09-17 measurement, and neither request-assembly exit
  (`seelexctx.NewAssembler` for main sessions, `node.ScopeAssembler` for
  subagent sessions — the latter bypasses the former entirely) sanitised
  `WorkingHistory` at all. The window projection keeps non-unit messages on
  purpose (audit R3: an orphan `tool` row stays inside the window), so an
  orphan can land at the *head* of the projected history and go out verbatim.
  Now: `seelexctx.repairToolPairing` performs the full protocol repair (drop
  orphans/duplicates, move misordered results back next to their declaration,
  placeholder completion on the `ReplaceHistory` path only), both exits call
  the exported `seelexctx.SanitizeProviderToolProtocol` (drop/move only — never
  synthesise a placeholder, since a request may be assembled while a tool call
  is still in flight), and a rejected wire is classified as `invalid_history`
  so the bounded-checkpoint recovery runs instead of killing the loop.
  Regression: `seelexctx/wire_protocol_safety_test.go` (four protocol cases
  plus the projection-chain reproduction),
  `seelebridge/node/coordinator_tool_protocol_test.go`,
  `application/core/history_safety_test.go`
  (`TestToolProtocolRejectionsAreHistoryFailures`). Legal histories are left
  byte-identical (projection == sent bytes; prefix-cache safety).
  See `docs/devlog/2026-09-20-wire-tool-protocol-orphan-400.md`.

- **A running session no longer derails an idle session's composer submit.** Two
  independent mechanisms, both exposed once the renderer pins every plain submit
  to an explicit session ID (`gui/frontend/dist/composer-input.js`
  `composerSubmitPlan` → `SubmitToSession`):
  1. `SubmitToSession` treated an **unmaterialized draft** as "target not loaded"
     and recovered it through `ActivateSession` → cold restore of the
     `Status=draft` record. The whole materialization path
     (`materializeDraftSession`: engine bundle by early-assigned SID, project
     binding, title, composer cleanup) was skipped, so a brand-new session
     opened with a "已恢复会话: draft_…" restore marker, the draft slot stayed
     armed, and the already-sent text survived in the draft record — reappearing
     in the input box after a restart. While another session ran, the same submit
     additionally flipped the view into a `restoring` shell and parked on the load
     completion point. `SubmitToSession` now runs a draft gate
     (`session_draft.go` `materializeDraftForSubmit`) before the loaded check; a
     draft that is no longer the viewed session fails explicitly
     (`ErrDraftNotInView`) instead of being routed into some other session.
  2. `BeginNewSession` read and cleared the engine through the **process-level
     active alias** (`Engine.History()` / `Engine.ClearHistory()`). That alias
     names whichever session was activated last — possibly one whose framework
     `Session` lock `ChatStream` holds for the entire turn — so one click on
     "new session" queued behind that turn *while holding the view transition
     key* (the only key while `PerSessionExecution` is false: every resume,
     unload and ambient submit waits on it), and then wiped the running
     session's working history. Both accesses now route per session
     (`engineHistoryFor`, new `clearEngineHistoryFor`).
  Regression: `application/core/session_running_idle_submit_test.go`
  (`TestSubmitToSessionMaterializesDraftWhileOtherSessionRuns`,
  `TestSubmitToSessionMaterializesIdleDraft`,
  `TestBeginNewSessionDoesNotSerializeBehindRunningSession`).
  3. The **input box content was not scoped to a session**: text left unsent in a
     running session's composer stayed in the box when the user switched to an
     idle session, and the next Enter submitted it to that idle session
     (`composerSubmitPlan` only honours the current view session). The same
     global dirty flag made `shouldRestoreDraft` refuse the idle session's own
     draft text, so the box kept showing the other session's words. Composer
     content is now scoped per session (`gui/frontend/dist/composer-input.js`
     `composerViewSwitch`, LRU-bounded stash of unsent text) and the render pass
     aligns content and dirty state with the view session *before* the draft
     restore. Regression: `gui/frontend/dist/composer-input.test.mjs`,
     `application/core/session_running_idle_submit_test.go`
     (`TestIdleSessionSubmitWhileOtherRunningLandsInViewSession`).

- **The retained context window is one rule everywhere, and the compacted
  range is recorded, not derived.** The compaction retained prefix was a
  hard-coded share of the budget (`TargetAfterCompaction` = 60%) and the cold
  restore tail reused the same number with a fixed unit cap, so the configured
  `window.retain_tokens` / `window.ratio` knobs had no effect on either. Both
  now go through one implementation
  (`seelexctx.WindowConfig.RetainedContextTokens`), `min(token1, token2)`:
  `token1 = window.retain_tokens` (unset → the account context window),
  `token2 = window.ratio × all_context`; everything outside the retained prefix
  is folded by `compact_context` (raw turns stay in session storage and are
  readable by reference). Request assembly passes the assembled full context as
  `all_context`; the read tail (cold restore of transcript / history) happens
  before any request exists, so it uses the account context window — a session
  below its ceiling is not truncated by the ratio window, since restoring
  rebuilds existing history instead of making a new compaction decision
  (`core.RetainedReadTailBudget`, `session_runtime.Deps.TranscriptTailBudget`).
  `window.force_compact_tokens` adds the hard side: `all_context` at or above it
  forces autonomous compaction and bypasses the progress-epoch throttle.
  Compaction records now carry the compacted range as **recorded** data
  (`message_from/message_to`, `event_from/event_to` on the application side;
  `CompactFrame.From/To` taken from the compacted units' own ordinals in the
  framework DAG), so no consumer re-derives the boundary from unit counts.
  Regression: `application/core/context_window_rule_test.go`
  (`TestCompactRetainedPrefixIsMinOfTwoWindows`,
  `TestCompactHardThresholdForcesCompression`,
  `TestCompactHardThresholdBypassesEpochThrottle`,
  `TestCompactContextHandlerReportsRecordedRange`,
  `TestReadTailBudgetFollowsRetainedWindowRule`),
  `seelexctx/window_retain_test.go`.

- **Wire assembly now consumes the §5.2 soft budget, so "the soft threshold
  triggers compaction" actually fires.** `Settings.WireBudgetTokens` /
  `WireSoftRatio` / `WireTargetRatio` were resolved by `wireBudget()` but read
  nowhere outside tests, and the assembler hard-coded a single
  `Budget = 200_000`: the trigger condition was isolated, so the reader only
  ever collapsed at 100% of the budget, never at the configured 75% soft
  threshold. `storageSettings.applyWireBudget` now turns the request's absolute
  budget into the soft threshold (falling back to the configured value when no
  budget is given), and `assembleWireWorkspace` / `assembleRoleWire` inject it;
  the hard-coded default is demoted to a last-resort constant
  (`defaultWireBudgetTokens`). Behavior change: restored engine history and role
  wire now stop at `wire_soft_ratio` of the requested budget and set
  `need_compact`, instead of filling the whole request budget. Regression:
  `sessionstore/wire_soft_budget_test.go`
  (`TestApplyWireBudgetDerivesSoftThreshold`, `TestWireAssemblyStopsAtSoftThreshold`).

- **Work table content is a project/global ledger, not session-granular.** The
  work table aggregates `plan` / `todo` / `task` / `subagent` rows across
  sessions: switching (or starting) a session must not drop rows produced in
  another session. `RuntimePort.TaskSnapshot()` (the `worktable` projection
  source) now merges the live registry (current session) with every session
  scope partition, de-duplicated by cross-session identity (idempotent `Key`
  first, row ID only as fallback — auto row IDs are process-unique, see the next
  item). Session-scoped reads stay on `TaskSnapshotFor(sessionID)`, which now
  serves exactly two consumers: per-session persistence (`SessionRecord.Tasks`)
  and the request-tail trace block (a session must not read another session's
  active tasks into its context). Regression:
  `seelebridge/worktable_global_scope_test.go`
  (`TestWorkTableGlobalScopeRepro`, `TestWorkTableGlobalReadKeepsSessionReadScoped`).
  Behavior change: `/new` (`BeginNewSession`) no longer wipes the ledger — it
  only moves the current-session pointer
  (`TestBeginNewSessionKeepsGlobalWorkTable`, formerly
  `TestBeginNewSessionClearsWorkTable`).

- **Auto work-table row IDs are allocated process-wide and never reused.**
  `task:<n>` came from a per-registry counter and session scope partitions used
  `task:<len(records)+1>`, so two sessions producing key-less rows received the
  same ID: the ledger's ID-keyed merge silently dropped one row, and a session
  switch / disk restore could re-issue an ID that was already occupied — the
  registry is keyed by ID, so the new row **overwrote** the restored one. The
  auto-increment source now lives in `seelebridge/task` (`NextAutoID`, shared by
  the live registry and every session scope partition), `ObserveAutoID` raises
  the water mark when records are loaded/restored so external numbers are never
  re-issued, and `nextFreeIDLocked` additionally skips IDs already present
  (`TaskRegistryState.nextID` is gone). Regression:
  `seelebridge/worktable_ledger_identity_test.go`
  (`TestWorkTableLedgerRowIDsUniqueAcrossSessions`,
  `TestWorkTableLedgerNeverReusesRestoredRowID`) and
  `seelebridge/task/auto_id_test.go`
  (`TestRegistryAutoIDsNeverReuseRestoredIDs`).

### Known issues

- Work-table `todo:<n>` row IDs encode an allocator number, not the todolist
  index, while the GUI three-state write-back parses the number as an index
  (`parseWorkItemID` → `SetTodoStatus(index)`). The number now comes from the
  same process-wide counter as `task:` rows, so it is unrelated to the list
  position: one prior task allocation plus a two-item list yields `todo:2` /
  `todo:3`, and toggling the first row (`todo:2`) resolves index 2 → out of
  range. The `application/core` cases go
  through `fakeRuntime`, whose `todo:<index>` IDs happen to line up, which is why
  this has not surfaced. Fixing it means addressing the write-back by row ID
  (auto IDs are now process-unique, so they are addressable) and routing it to
  the owning session's scope instead of mutating the current session's list.
  Recorded on 2026-09-19 in
  `docs/devlog/2026-09-19-worktable-global-scope.md` §7.5; not fixed in this
  batch (separate surface: GUI contract + fakes).

- **The D1 main-session tail budget never actually derives its round count.**
  `seelebridge/runtime_context.go` `windowTailBudget` fills only
  `ProviderContextInfo.ContextTokens`, so `AvgRoundTokens` / `ReservedTokens`
  are absent and `seelexctx.WindowRounds` always takes its "inputs unavailable"
  fallback: the durable-history read width is `window.rounds` when explicitly
  configured, otherwise `window.min_rounds` — the clamp formula never runs on
  this path, while `config/seelex.yaml` presented it as where the number comes
  from. This batch only corrects the wording (docs/config comments, plus the
  unreachable `rounds = 4` fallback now references
  `DefaultWindowConfig().MinRounds` instead of duplicating the default). Two
  consequences: `window.retain_tokens` has no effect on this path, so a Run not
  preceded by application context assembly would carry only `min_rounds`
  verbatim units plus summaries; and subagent sessions attach
  `DurableHistory` without a tail budget (full load), role-turn sessions attach
  none. Static reading finds no such un-assembled entry point in the main chat
  flow — every assembly arms the one-shot `PrepareNextLoad` handoff, deferred
  install included — so wiring the derivation inputs is not required yet, but
  proving "never runs" needs runtime evidence. Recorded on 2026-09-20 in
  `docs/devlog/2026-09-20-retained-window-and-read-tail.md` §6.

## [v0.1.0] - 2026-09-18

### Added

- **Session permission tiers for the main agent: the binary `full_access` toggle
  becomes an ordered tier table `manual / edit / auto / full`, selectable per
  session and rendered as a list in the runtime panel.** A tier is a **declarative
  overlay on the root subject's rule table** (`seelebridge/tools/permission_tiers.go`
  `ApplyTier`): it only **removes `ask` rules** (edit drops `write_file`/`edit_file`
  asks; auto additionally drops `bash` asks), never adds an allow and never touches
  a `deny` — so `rm -rf /`, `dd if=* of=*`, `mkfs*` stay hard-blocked in every tier.
  The gate pre-builds one checker per tier (`PermissionGate.rebuildLocked`) and
  picks the checker by **subject class + session tier** (`gate`): root reads its
  session's tier table, `sub`/`emp_*` always read the base table. The `full`-tier
  short-circuit in `Enforce` is **tightened from `class != sub` to `class == root`**,
  so `full` no longer silently auto-approves employee over-reach (requirement 3):
  employees still elevate through the existing approval panel, subagents still
  cannot route out-of-grant tools. Tier state is per session (`SessionUnit` slot +
  per-session gate resolution); the process default comes from `-permission`
  (`manual|edit|auto|full`, legacy `full_access` → `full`). `SetFullAccess*` is kept
  as a compatibility shell (`true ⇔ full`, `false ⇔ manual`). Catalog is delivered by
  the backend (`RuntimeState.PermissionTiers`); the composer chip stays at the old
  "full access" position and now shows the current tier, with the authoritative list
  in the runtime panel. Evidence:
  `seelebridge/tools/permission_tiers_test.go` (`TestTierDecisionMatrix`,
  `TestTierDoesNotBypassEmployeeBoundary`, `TestTierDoesNotBypassSubagentBoundary`,
  `TestTierSessionIsolation`), `application/core/session_permission_tier_test.go`,
  `gui/bridge_test.go:TestBridgeForwardsPermissionTier`.
- **Role-turn execution body: agent roles now really run a bounded,
  tool-holding turn on their own session, under their own permission subject**
  (`seelebridge.RunRoleTurn`, implementing the new `contract.RoleTurnPort`).
  Until now only `tl`'s ADVISOR review round existed (`RolesWithExecutor` =
  `user`/`main`/`tl`): every other registered role could get a role session and
  a member-list row but nothing ever drove it to produce a turn, so the
  "per-role interception" chain delivered by the same-day permission work had
  no load-bearing surface. The body now:
  (1) opens the role's session (own framework `Session`, node-level context
  components so compaction never writes into the main session's stack) **and,
  in the same action, assigns that employee's permissions** into the single
  account table as the `emp_<role>` subject (non-assignable policies — inherit
  / `full` / unknown — write nothing, so "inherit" really inherits);
  (2) binds the role session's tool path root to the main session's project
  root;
  (3) puts the employee subject into the turn ctx **by construction**
  (`tools.WithEmployeeSubject`), so gating and tool-face narrowing hold even
  when the role-session → policy reverse index is cold; and
  (4) runs one bounded round (`roleTurnMaxLoops = 12`) with the registered
  employee prompt (or a minimal role frame). Wired through
  `Dependencies.RoleTurn` → `goalCoordinatorDeps.RoleTurnFor`, so the goal
  loop's `agent` seats execute; without the port, agent roles still take no
  governance seat (pilot shape, nothing pretends to work). The round's work
  text reaches the seat via ctx; `ReleaseRoleSessions` drops the derived
  engines at shutdown. Evidence:
  `seelebridge/runtime_role_turn_test.go` (8 cases: role-session identity,
  assign-on-open, tool-face narrowing for `readonly` vs `readwrite`, engine
  reuse, error propagation, inherited policy writes nothing, release/rebuild),
  `application/core/role_turn_test.go` (dict ↔ request field parity, round text
  through ctx, explicit input wins, nil runner without the port). Known gaps at
  that point: the real-API smoke and the computer-use live round are the *next*
  task (this change makes them possible; their acceptance here uses a fake engine);
  the ADVISOR round still held **no** tools at that commit (read-only tools landed
  later the same day — see the ADVISOR entry below); `Progress` is the
  conservative "non-empty conclusion" proxy, not a goal-advancement metric.
- **The ADVISOR review round now holds read-only tools, so a verdict can be
  grounded in evidence instead of plausibility** (`f43f635`). The evaluator's
  round runs on its own session coordinate (`advisor:<main session>`), keeps its
  context isolated from the executor (that isolation is why the review is worth
  anything), and is granted the read-only tool face only
  (`dto.ToolPolicyReadonly`: `read_file`/`grep_search`/`glob` + result/plan
  readers) — it can inspect the tree and the diff, and deliberately **cannot**
  run `bash` (that is the `rw` group), so "run the tests yourself" is still not
  in reach and remains a designed-later step. Evidence:
  `git show --stat f43f635`; the read-only face is asserted in
  `seelebridge/runtime_goal_tl.go` (`ToolsPolicy: dto.ToolPolicyReadonly`) plus
  the permission decision tests that pin `ro`-only visibility for that subject.
- **The main-session permission tier now survives a restart.** Selecting a tier
  is "this session's permission setting", so it is persisted per session instead
  of living only in the in-memory `SessionUnit` slot. The storage target matters:
  the v8/S20 layout **retired the record channel** (Open supports `BackendJSON`
  only → `jsonRepository` → `LayoutV8()` is always true; `SaveRecordRaw` writes
  through only `status`/`title` and `LoadRecordRaw` returns a *derived*
  `(version/id/status/updated_at/conversation)` payload), so a new
  `SessionRecord` field would silently drop. The tier therefore rides the
  **session-level settings** channel: `sessionstore.SessionDisplayMeta` gains
  `PermissionTier` and a dedicated `SetPermissionTier`/`PermissionTier` pair,
  exposed to the application as the new optional `session.SessionSettingPort`
  (`internal/adapters.SessionPort` implements it). Writes go through
  `Service.SetPermissionTier` **before** the in-memory change (a failed write
  reports an error and leaves the tier untouched); reads happen on cold start,
  hot attach and cold load, and land in the unit slot + per-session gate +
  per-session approval auto-approval. Display-meta writes are now a
  field-merge (unpinning no longer wipes the tier). Evidence:
  `application/core/session_permission_tier_persist_test.go`
  (`TestPermissionTierSurvivesSessionReload`,
  `TestPermissionTierReloadKeepsManualChoice`,
  `TestPermissionTierWithoutSettingPortStaysInMemory`,
  `TestPermissionTierSettingPortErrorSurfaces`),
  `sessionstore/session_meta_test.go:TestSessionMetaStorePermissionTierIsolation`,
  `internal/adapters/session_setting_ports_test.go:TestSessionPermissionTierRoundTrip`.
- **Runtime tier switching is now reachable from the CLI/TUI: `/permission`
  `<manual|edit|auto|full>`.** `main.go`'s composition comment had claimed
  "runtime switching by GUI/CLI per session" since the tier work, but the command
  registry only had `/effort`, so headless/TUI hosts had no way to change tiers.
  The command shares the single `Service.SetPermissionTier` path with the GUI
  chip/list and the headless `SetPermissionTier` RPC (write-side validation,
  per-session landing, persistence), echoes the current tier plus the
  backend-supplied catalog when called without arguments, and deliberately has
  **no running guard** — switching while a turn runs is exactly how a user
  allows or tightens the pending approval (unlike `/effort`). Evidence:
  `application/core/permission_command_test.go`
  (`TestPermissionCommandRegisteredInHelp`,
  `TestPermissionCommandWithoutArgsShowsCurrentAndCatalog`,
  `TestPermissionCommandSwitchesTier`),
  `gui/headless_permission_test.go:TestHeadlessSetPermissionTierDispatch`.
- **Bottom terminal panel (VS Code style): a local PTY terminal docked in the
  middle column, with multi-session tabs, collapse and drag-resize.** New
  backend package `gui/terminal` owns the PTY lifecycle (one `go-pty` session
  per terminal: Windows ConPTY, unix `creack/pty`), exposing
  `Bridge.TerminalOpen/TerminalWrite/TerminalResize/TerminalClose/TerminalList`
  and a dedicated `seelex:terminal` event (`output` with base64 bytes — PTY read
  boundaries can split multibyte runes — and `exit` with the exit code). The
  panel lives at the bottom of `.workspace`, remembers its layout
  (`seelex.terminal.v1`: open/collapsed/height, height clamped to `[120px,
  72vh]`), and is driven by `` Ctrl+` `` (toggle), `` Ctrl+Shift+` `` (new) and
  the topbar terminal button; tabs auto-number duplicate shells and mark exited
  ones. Rendering is xterm.js 5.3.0 + `@xterm/addon-fit` 0.10.0, vendored
  offline under `gui/frontend/dist/vendor/xterm/` (MIT, versions/licences
  registered). The terminal is **user-facing only**: it is not part of the agent
  tool surface, the snapshot or the headless control plane, and its cwd comes
  from the backend's current workspace (never from the renderer). Terminal
  convergence is order-critical: wait for the child, close the PTY handle, drain
  output, then emit `exit` (ConPTY does not close the output pipe when the child
  exits; unix returns EIO on read). `Close` is `sync.Once`-guarded — a second
  `ClosePseudoConsole` on a recycled HPCON killed the desktop process outright in
  testing. The explorer subpage's file-detail drawer gains the mirrored
  「收起详情，让出内容页」 control: it folds into a 28px rail so the work tree +
  commit log own the subpage, mutually exclusive with the existing
  hide-panes mode. Its header is now the multi-file chip strip itself — the
  redundant 「文件详情 · <path> · <size>」 title/meta line is gone (a chip is the
  file identity) and the action buttons sit flush right. Evidence: `gui/terminal/terminal_test.go`
  (`TestManagerEmitsBase64OutputThenExit`,
  `TestManagerKeepsCreationOrderAndRejectsUnknownSession`,
  `TestManagerWritesInputAndKillsOnClose`,
  `TestManagerRejectsOpenWithoutHandlerAndOverLimit`,
  `TestManagerShellFailureIsReported`, `TestManagerRealShellRoundTrip`),
  `gui/frontend/dist/terminal-panel.test.mjs`,
  `gui/frontend/dist/terminal-panel-controller.test.mjs`.

### Fixed

- **The provider no longer rejects a turn as `Messages with role 'tool' must be
  a response to a preceding message with 'tool_calls'` (HTTP 400).** The engine
  history that is projected into a provider request could contain a tool result
  that was not adjacent to the assistant message declaring that call — e.g.
  `assistant(tool_calls c1) → user → tool(c1)` — because the durable
  reconstruction reads each record on its own. `context_runtime`'s repair pass
  only *filled missing* results; it documented "result exists later ⇒ leave the
  order alone" (`TestRepairInterruptedToolChainsSkipsWhenResultExistsLater`),
  and that shape is exactly what the provider refuses. The repair now normalises
  the projection instead: a result that exists elsewhere is moved back to sit
  immediately after its declaration (`TestRepairInterruptedToolChainsReorders
  ResultBackToDeclaration`), a duplicate result for the same call id and a tool
  row with **no** declaring assistant message are dropped from the projection
  (`TestRepairInterruptedToolChainsDropsOrphanResult`), and a tuple that has both
  an in-place result and a late-arriving one keeps the placeholder *after* the
  in-place result so the result block stays contiguous. The rule is asserted by
  `looksLikeProviderValidToolPairs`, a local encoding of the provider's own
  ordering rule, so "repaired but still invalid" fails in tests instead of in
  production. Scope: only the working-history projection is rewritten
  (`EnginePort.replaceRawHistoryFor` → `prepareHistory`); the durable message
  rows are untouched, so nothing is lost from the record — a dropped orphan is
  re-derived (and re-dropped) on the next cold load.

- **A crashed dev GUI no longer blocks the next launch.** The JSON data root takes
  a single-writer lock (`<root>/lock.owner`); a clean exit releases it
  (`jsonRepository.Close` → `releaseDataRootLock`), but a crash or force-kill
  leaves the file behind. With the previous default (`lock_auto_recover` unset =
  `false`) the next `Open` treated that residue as a refusal to start and exited
  with `session storage: stale data root lock found (set
  session_storage.lock.auto_recover=true to take over)` — the user-visible effect
  was "the GUI won't open again until someone deletes the lock file".
  `config/seelex.yaml` now sets `lock_auto_recover: true` (with `config/seelex.yaml`
  kept in sync in the dev baseline), so a lock whose owner process is gone and
  whose heartbeat is older than `lock.stale_after_seconds` (default 300) is taken
  over on the next start. The single-writer invariant is unchanged: takeover still
  requires **both** staleness conditions, so a live process holding the same data
  root continues to get an immediate error. Evidence:
  `sessionstore/data_root_lock_test.go:TestJSONDataRootCrashedGUIResidualLockRecovered`
  (reproduces the residue → refuses under the conservative setting → takes over
  under the new one → releases on clean close), with the counterpart
  `TestJSONDataRootLockForeignProcessRejected` pinning the live-holder refusal.
  The Windows liveness probe this relied on was still wrong and is fixed below.

- **The dev GUI opens again after a force-kill: the Windows process-liveness probe
  now asks the kernel, not `OpenProcess`.** Follow-up of the bullet above. On Windows
  a process object whose process has already terminated but whose handle is still
  referenced by someone else can still be opened — the PID stays reserved — so
  `os.FindProcess` + `Release` reported the dead dev GUI as a live lock holder. The
  data root lock was therefore never judged stale (no matter how long you waited),
  and the next launch died during storage init with `session storage: data root is
  locked by another process` before any window appeared (`-H windowsgui` means no
  console output either, so the symptom is just "the GUI won't open"). Liveness is
  now `OpenProcess(SYNCHRONIZE|PROCESS_QUERY_LIMITED_INFORMATION)` +
  `WaitForSingleObject(handle, 0)`: only `WAIT_TIMEOUT` counts as alive, while
  `WAIT_OBJECT_0` (terminated) counts as gone even if the handle/PID is still around;
  an access-denied holder is treated as alive, matching the unix `EPERM` rule.
  Evidence: `sessionstore/data_root_lock_windows_test.go:TestJSONDataRootTerminatedProcessLockIsStale`
  (spawns a child, lets it exit while keeping its handle, and pins `processAlive ==
  false` plus stale-then-takeover `Open` semantics). Docs:
  `sessionstore/README.md` §并发、存储、安全.

- **A malformed ADVISOR verdict is no longer reported as "b absent (429/timeout)",
  and the verdict text is read leniently.** The 2026-09-16 GUI smoke run (a fresh
  `dist/stage-gui` build, goal `g-1`) shows the goal loop classifying a
  *content-complete* `verdict_done` as B4 absence: `goal_propose_finish` returned
  `outcome=escalate_human` with `goal TL 输出非 JSON: invalid character 'å' after
  object key:value pair` (`dist/stage-gui/.seelex/sessions-json/.../message_1_7.jsonl`,
  seq 13) — `'å'` is `0xE5`, the first byte of the Chinese character that followed a
  **prematurely closed string**: the verdict's `content` carried unescaped inner
  quotes. The user-facing closure note therefore said the goal was "still active"
  while the same run closed it `completed`. Two changes:
  - `seelebridge.parseGoalDirective` (and `parseRolePromptOptimization`) now read
    the first **balanced** JSON object with a syntax-only repair pass — unescaped
    inner quotes, raw control bytes, invalid escapes — never touching field
    semantics (`seelebridge/json_object.go`; table-driven tests include the
    incident's exact byte shape that reproduced `invalid character 'å'`).
  - `gate.ProposeFinish` and `Supervisor.runRoundLocked` now separate
    **`ErrBadDirective` (b answered, verdict unusable)** from **absence
    (429/timeout)**: both still keep the goal `active` (the safe default — an
    unusable verdict never closes a goal), but the message says which one happened
    instead of labelling a parse failure as a rate limit.
- **The ADVISOR verdict now reaches the visible chat in the turn that produced
  it, instead of one user turn later.** The governance round (ADVISOR) runs at
  the *end* of a turn (`goalAdvanceAfterChat`), but its b→a directive stayed in
  the `TechLeaderMailbox` until the **next** `Submit`: only then did
  `injectGoalDirectivesForStart` drain it into the trusted injection zone, and
  only at that turn's tail did `injectGoalDirectivesFor` replay it as a visible
  row. A user (or the team-work live probe) therefore never saw the verdict after
  submitting — the probe's bounded wait could not succeed by construction
  (`_tmp/teamwork-computer-live.log`: `FAIL (911.47s)`, "等待 ADVISOR 裁决超时
  （15m0s）"). A new end-of-turn step
  (`Service.publishPendingGoalDirectivesFor`) publishes the directives the
  governance round just produced, **without consuming them** (new
  non-destructive `TechLeaderMailbox.PeekDirectives`): the trusted injection
  still happens, with unchanged timing and semantics, at the next
  `ChatStream`. Replies are deduplicated per `corr`
  (`goalCoordinator.DirectivePublished`/`MarkDirectivePublished`), so the regular
  replay at the next turn's tail does not write a second row. The visible row is
  unchanged in shape (`assistant` + `role_name=tl`) and now also carries the
  machine-readable class `kind=tl_directive` (`MessageOrigin.Kind` →
  `model.Message.Kind`, constant `goaldomain.DirectiveRowKind`, shared with the
  role-draft row). The probe's evidence 3 was aligned with the fix: it reads the
  visible verdict row (kind + role) and takes the verdict `kind` from the
  ADVISOR's raw round output (`role.snapshot(tl)` `tl_directive` row JSON), and
  the 15-minute wait (a structural false-negative guard) is gone. Evidence:
  `application/core/goal_directive_visible_immediately_test.go:TestAdvisorVerdictVisibleInProducingTurn`
  (red → green: 0 rows without the wiring),
  `application/core/goal/techleader_test.go:TestMailboxPeekDoesNotConsume`,
  `application/core/visible_role_attribution_test.go`; see
  `docs/devlog/2026-09-16-advisor-verdict-visible-immediately.md`.

- **Scratch Go files under the git-ignored `tmp/` no longer break the repository
  gates.** `tmp/final/*.go` mixed two packages (`dto` + `contract`), so
  `go build ./...`, `go build -tags ...`, `go vet ./...` and `go test ./...` all
  exited non-zero while `git status` stayed clean (the directory is ignored). The
  scratch archive was renamed to `_tmp/` (the Go tool ignores `_`-prefixed
  directories when expanding `./...`, while explicit paths such as
  `go test ./_tmp/goal-tl-live-smoke` keep working; nothing was deleted). The one
  scratch file that `gofmt -l .` flagged was reformatted. Evidence: all six
  AGENTS §5 commands green (see `docs/devlog/2026-09-17-*.md`).

- **Employee (role-session) escalation now reaches the existing approval panel.**
  A role whose `tools_policy` is inherit (`""`) already inherits the host's full
  tool face, and every call is already decided per-call by the framework `Gate`
  (bit/group routing + rules) — but the approval request was routed to the
  *role session id* (`goal-a2a-pm` / `advisor:<main>`). `application/core` only
  mirrors approvals whose session is the view session (or empty) into the single
  `Snapshot.Interaction` slot, and a role session has no session unit to carry
  the catalog's `awaiting_approval` badge — so the escalation was invisible to
  the host and the tool call could only wait out the approval timeout. The
  permission gate now takes a `RoleSessionOwner` resolver (injected from
  `main.go` via `app.RoleSessionOwner` — the same reverse index the
  session-policy read uses) and rewrites the approval's session to the owning main
  session, reusing the existing panel reads (single-slot modal, catalog badge,
  session-snapshot approvals) unchanged; judgement is untouched (the subject
  class still reads the raw dispatch session), so folding the attribution cannot
  widen a read-only employee. A nil/unmatched resolver keeps the old
  per-session attribution. Evidence:
  `seelebridge/tools/permission_inherit_test.go` (inherit → full tool face,
  per-call middleware review, owner attribution, judgement unchanged) and
  `permission_employee_panel_test.go` (end-to-end: an employee escalation lands on
  the owner session's pending list, and the call runs once the human allows it).
- **A terminal ADVISOR verdict in a routine turn now actually ends the goal
  loop** (the `goal_loop` governance loop could stay `active` forever). The
  escape-hatch contract in
  `docs/2026-09-08-govern-loop/design.md` §5 says `verdict_done` →
  `Controller.Finish` (goal popped, peer unbound/reaped). Only the finish gate
  (`ProposeFinish`, reached when EXEC calls `goal_propose_finish`) honoured it;
  a `verdict_done` produced in a normal ADVISOR round
  (`AdvanceAfterChat` → advisor seat → `RunEval`) merely broke the *governor* —
  so the goal stayed `active`, the governance panel kept rendering "in
  progress", and the ADVISOR was never called again (`Next` returns false while
  broken). The advisor seat now closes the top goal on a terminal
  `verdict_done` via `Supervisor.CloseTopGoalOnTerminal`; `verdict_not_done` /
  `correct` / `checkpoint_ok` still leave the goal untouched and
  `escalate_human` still keeps it `active` for a human. Also fixed alongside:
  a new `goal_begin` in the same session now rebuilds the governor, so a goal
  created after a previous one terminated is not silently starved of ADVISOR
  rounds by the old, already-broken loop. Regression:
  `TestAdvisorSeatVerdictDoneClosesGoal`, `TestAdvisorSeatNonTerminalKeepsGoal`,
  `TestGoalCoordinatorRoutineVerdictDoneClosesGoal`,
  `TestGoalCoordinatorBeginResetsBrokenGovernor`.
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

- **Tool-permission decisions now come from the framework `permission.Gate`**
  (via the local Seele replace, ahead of the next framework version).
  `seelebridge/tools.PermissionGate` keeps owning the three harness pieces — the
  authorization table (`PermissionChecker`), the execution-choice page
  (`ApprovalHandler`) and the per-session elevation map — but no longer
  hand-rolls the allow/ask/deny switch: `Middleware` is now a
  `tools.MetaMiddleware` that hands every call to `Gate.Decide`, with the
  session resolved from the dispatch ctx becoming both the authorization subject
  (`WithEngine`) and the approval routing key (`WithSessionID`). That is what
  lets a tool carry its own cluster metadata (`ToolMeta{Kind,Groups,Bits}`) into
  the decision instead of every policy living in a name-keyed allow list.
  Per-session full access is preserved as a `BitEnforcer` that short-circuits
  before bits/groups/rules, and `DenyWithoutPrompt` keeps the existing contract:
  a configured `deny` is still an immediate refusal — now classifiable with
  `errors.Is(err, tools.ErrPermissionDenied)` / `tools.ErrToolNotVisible`
  instead of hand-built strings. Middleware nesting is unchanged
  (event → permission → diagnostic): the event and permission middlewares moved
  to `WithMetaMiddleware` behind an adapter that promotes the plain event
  middleware, so the effective order is what it was. Regression:
  `permission_framework_gate_test.go` pins the denial classification, the
  tool-declared-cluster routing (same tool — a granted session runs, an
  ungranted session gets `ErrToolNotVisible`) and the framework-filled approval
  request (`ID`, `SessionID` from ctx, `Timeout` passthrough, `Preview`/`Options`).
- **Employee panels: no footnote-style hints** (follow-up of the GUI de-noising
  round). The "修改员工 · ADVISOR / 入职员工 / 新建员工 · 员工库" cold-load
  panels no longer render per-field hint lines or the scope sentence under the
  submit row: the facts moved into the controls' `title` (hover-only) and the
  select options now carry their own wording ("只读（不写文件 / 不执行命令）"
  already says what the old hint repeated). The "优化提示词" runtime receipt got
  its own `.team-prompt-state` class instead of borrowing the hint style, so a
  panel can be asserted to have **no** hint spans at all. Regression: a new
  `agent-team-view.test.mjs` case covers both scopes and all four panel
  instances (no `team-field-hint` / `team-editor-hint`, hint sentences survive
  in `title` only, the 7-field skeleton and numbering unchanged), and
  `gui/bridge_test.go` pins the same invariant on the embedded bundle.
- **Agent Team audit published**:
  [`docs/devlog/2026-09-15-agentteam-panel-optimization.md`](docs/devlog/2026-09-15-agentteam-panel-optimization.md)
  ranks the follow-ups found while reviewing the panel — silent field loss on
  employee edit (`RoleSpec` 10 fields vs. `TeamMember` 7 + whole-entry replace
  semantics), unsaved hire drafts dropped on Esc/repaint, team-draft drags
  writing straight into the session, disabled-button reasons living on
  `data-tip` (which a disabled element never fires), keyboard-less ordering —
  plus the backend capability gaps and the industry comparison that says which
  multi-agent UI patterns do *not* fit a 220–480px rail.
- **GUI de-noising round 2** (panel annotations, session rows, message
  highlight, agent-team libraries):
  - Messages no longer carry *any* speaker colour highlight. The previous
    batch had already dropped the block tint in favour of a 3px left status
    rule; that rule is gone too, so EXEC / ADVISOR are distinguished by the
    speaker name alone (`styles.css` §20).
  - Session rows are compact (24px, 1px/4px padding) and their `⋯` button is
    always visible. Clicking it opens a **floating menu** (`#session-menu`,
    `position: fixed`, flipped up when it would overflow the viewport) with
    labelled items for pin / fork / delete, instead of expanding three icon
    buttons sideways into the row. The menu and the in-row actions share one
    dispatcher (`dispatchSessionListAction`).
  - The agent-team rail heads lost their annotation text ("全局·跨会话 ·
    不依赖团队", "N 个内置形态", "拖拽行首手柄调整发言顺序", "装配与编排") and
    the whole "default order vs this session" key/value table was deleted.
- **Agent Team panel: libraries read as "what I actually have"**.
  - The employee library block now shows the **merged available pool**
    (global master ∪ this session's roster, merged read-only in
    `employeePool()`), each row tagged `库` / `本会话`. A session with
    employees can therefore never show "employee library: 0"; rows that only
    exist in the session get a one-click 「入库」 (writes the global master).
    The roster table dropped its type column (three columns: identity /
    position / actions) so chips stop wrapping mid-word in a narrow rail.
  - The team library lists **the user's own teams** only, one row each; the
    name is a button that opens *that* team's cold-loaded team panel
    (title = the team, ✕ to close). Built-in presets are demoted to a chip
    row under the table (click = assemble in place) instead of masquerading as
    library entries, and the 「存当前会话」/「入库当前会话」/「顺序设为默认」
    whole-table write actions were removed from the GUI (the Bridge/Application
    methods stay available for headless and tool surfaces).
  - Team members can be dragged **into** the order from anywhere there is an
    employee: the team panel's member list (row order = speaking order, ✕ to
    remove, drop a library row before a member), and the session staff order
    (a library-only employee is instantiated into the session first,
    `AgentTeamInstantiateRole`, then placed with `AgentTeamSetOrder`).
  - 「发言调度」is no longer a key/value table: it renders the order as a
    pill chain (index + identity) with "speaking now" / "next" highlighted,
    a round badge, and one meta line for the user seat and stop reason —
    the interaction convention group chats use. (Research notes: SillyTavern
    group chats express turn order as a member list with per-member
    enable/disable plus an explicit next-speaker indicator, and show the
    queue position as `#n`; we borrowed the position-and-number language and
    skipped its toast-based announcements and hover-only controls.)
- **GUI de-decoration batch**: the conversation no longer tints whole message
  blocks by speaker, the shell drops its radial glow for a single `--bg`, and
  the right-rail tab strip loses its decorative gradient. Panel collapse
  controls moved from the topbar into each rail's own header row as
  `chevron-left` / `chevron-right` icons; a collapsed rail keeps a 26px spine
  with the toggle still reachable (Ctrl+B / Ctrl+J and the stored collapsed
  state unchanged). Ad-hoc text glyphs (session row `⋯ ★ ☆ ⑂ ✕`, account
  marks `● ○`, the full-access `✓`, the team drag handle and panel close) are
  now inline SVG icons from the shared registry.
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

- The tool-permission authority model now has a **long-term architecture
  document** (`docs/arch/agent-permission-subjects.md`, indexed from
  `docs/README.md` and `docs/arch/README.md`): subjects (root / sub / emp_ro /
  emp_rw) × route groups (ro / rw / rw_session / rw_desktop / ctl / adm) × bits
  (r=4 / w=2 / x=1) + sudo, the per-call evaluation order, the error taxonomy
  (`ErrToolNotVisible` = not routable for that subject vs `ErrPermissionDenied` =
  EPERM), the three product stances (sub-agent has no human in the loop,
  employee does, root falls back to the framework), and an explicit
  *current implementation vs target design* table (tools still register no
  `ToolMeta`, the employee execution face is not wired yet, the legacy
  hard-coded sub-agent lists still coexist with the bit model, the elevation
  ledger is still `once`).
- The permission live smoke now runs through the **production approval bridge**
  (`main.go:newPermissionBridge` → `application.ApprovalBroker`, the same path
  the GUI renders and resolves) instead of an approval stub, and pins three
  facts end to end against a real model: a whitelisted `bash` command executes
  with the choice page never opening; an out-of-scope command opens the page
  once (carrying `ToolName: bash`) and executes after the page allows it; a
  `fork_subagents` round opens **zero** pages while the sub-agents still attempt
  their writes (their bits allow them — nobody to ask). The full-chain harness
  now exposes the broker (`fullChainHarness.approval`) so permission cases can
  assert on the page itself rather than on a stub's call count. Measured on this
  machine: `--- PASS: TestRealAPIPermissionSmoke (46.76s)`.

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
