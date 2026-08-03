# Changelog

All notable changes to Seelex are documented in this file.

The repository is in Developer Alpha. Source builds report <code>dev</code>;
release builds receive their version from the Git tag through ldflags.

The next planned release is <code>v0.0.2</code>. The <code>v0.1.0</code>
line is reserved for the later breaking architectural rewrite and is not used
for this stabilization batch.

## [Unreleased]

### Changed

- Reworked the repository entry documentation around verifiable Harness
  behavior, technical decisions, current limitations, and DeepSeek-compatible
  configuration.
- Started the engineering-trust remediation covering release consistency,
  error boundaries, concurrency, testing, and open-source governance.

### Fixed

- Removed stale README claims about a local <code>go.work</code> and a
  <code>replace</code> directive. Seelex currently resolves Seele v0.1.1 from
  the Go module graph.
- Replaced the stale source release identifier with the neutral
  <code>dev</code> version. Tagged builds remain authoritative.

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
