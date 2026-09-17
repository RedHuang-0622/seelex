# Changelog

All notable changes to Seelex are documented in this file.

The repository is in Developer Alpha. Source builds report <code>dev</code>;
release builds receive their version from the Git tag through ldflags.

The next planned release is <code>v0.0.2</code>. The <code>v0.1.0</code>
line is reserved for the later breaking architectural rewrite and is not used
for this stabilization batch.

## [Unreleased]

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
  Known limit (unchanged by this fix): on Windows `os.FindProcess` can still report
  a dead pid as alive, so stale residue is recognised only once the heartbeat
  timeout has passed; during that window the error surfaces as
  `ErrDataRootLocked` rather than `ErrDataRootStaleLock`.

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
