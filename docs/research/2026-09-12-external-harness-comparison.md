# External comparison: coding-agent harness mechanisms (late 2026) vs Seelex

> Scope: harness *mechanisms*, not marketing. Seelex's own mechanisms are taken as given from this
> repo (`sessionstore/compact_frames.go`, `sessionstore/big_tool_result.go`,
> `sessionstore/checkpoint_store.go`, `plugin/`, `seelexctx/`, `workplan/`).

## Method and confidence

- `web_search` was **unavailable** (HTTP 403 quota errors). I instead fetched **primary sources
  directly**: vendor docs as markdown, open-source repos on `main`, and spec sites. Most claims are
  cross-checked against two artifacts (vendor doc + source file); single-source rows are flagged.
- Codex docs now live on `developers.openai.com` (403 here), so Codex claims cite the open-source
  repo; some Cursor/Windsurf/Cline bodies are JS-rendered → low confidence. No version numbers or
  benchmark scores are asserted.

## Mechanism matrix

| Target | Loop model | Context / compaction | Permission / sandbox | Subagent / orchestration | Persistence / resume | Extension model | Differentiator |
|---|---|---|---|---|---|---|---|
| **Claude Code / Agent SDK** ([glossary](https://code.claude.com/docs/en/glossary.md)) | Single agentic loop per session; hooks fire per session / turn / tool ([hooks.md](https://code.claude.com/docs/en/hooks.md)) | Auto-compact window is configurable (`/autocompact`, `--autocompact`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW`; default ≈967K tokens on 1M-context Sonnet 5); compaction keeps a structured summary and re-injects items *per mechanism* (skill index dropped; invoked skill bodies re-injected ≤5k tokens each) ([model-config](https://code.claude.com/docs/en/model-config.md), [context-window](https://code.claude.com/docs/en/context-window.md)). CLAUDE.md layering: managed policy → `~/.claude/CLAUDE.md` → project `./CLAUDE.md`, parent dirs at launch, subdirs on demand; `.claude/rules/`; auto memory `MEMORY.md` per repo shared across worktrees ([memory](https://code.claude.com/docs/en/memory.md)) | Modes `default`(Manual)/`acceptEdits`/`plan`/`auto`(classifier)/`bypassPermissions`; allow/ask/deny rules, deny applies in every mode; OS-level sandboxed Bash plus sandbox runtime, dev containers, VMs ([permission-modes](https://code.claude.com/docs/en/permission-modes.md), [sandboxing](https://code.claude.com/docs/en/sandboxing.md)) | Subagent = own context window, fresh isolated context (delegation message only), own tools/model/permissions, `isolation: worktree`; `fork` inherits the conversation; background by default; agent teams + dynamic workflow fan-out ([sub-agents](https://code.claude.com/docs/en/sub-agents.md), [agents](https://code.claude.com/docs/en/agents.md)) | Sessions resume/branch/`--teleport`; snapshot before every prompt (100 most recent, ~30 d), `/rewind` restores code, conversation, or both; Bash-made edits and most subagent edits are **not** tracked ([checkpointing](https://code.claude.com/docs/en/checkpointing.md)) | Hooks (command/HTTP/MCP/prompt/subagent), skills, plugins (skills+agents+hooks+MCP marketplaces), MCP | Classifier-model "auto" permission mode; deep rewind UX |
| **Codex CLI (Rust)** | Turn/step loop with collaboration modes (default / plan) | Token-budget compaction "installs a fresh context window" instead of model summarization, but reuses the compaction lifecycle (pre/post-compact hooks, `ContextCompaction` turn items); auto window keeps an absolute prefill-token baseline, preferring server-observed usage over estimates ([compact_token_budget.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/compact_token_budget.rs), [auto_compact_window.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/state/auto_compact_window.rs)) | `SandboxPolicy` = read-only / workspace-write / danger-full-access × `AskForApproval` including `Never`; exec-policy and guardian layers; managed-hooks-only via `requirements.toml` ([protocol.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs), [docs/config.md](https://github.com/openai/codex/blob/main/docs/config.md)) | Multi-agent v2 tools: `spawn_agent`, `send_message`, `followup_task`, `wait_agent`, `interrupt_agent`, `list_agents`; children may spawn children; `fork_turns` = `none` \| n \| `all` decides inherited context (full-history forks inherit parent model/reasoning) ([multi_agents.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/session/multi_agents.rs)) | Append-only rollout JSONL per session: recorder + writer lock, reverse JSONL scanner, seekable reader, session index, SQLite state db ([rollout/src/lib.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/lib.rs)); resume/fork with truncation at user-turn boundaries and `ThreadRolledBack` markers ([thread_rollout_truncation.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/thread_rollout_truncation.rs)) | Skills, MCP, lifecycle hooks, session-level plugin selection | Plan Mode is a *mode with strict mutation rules and a `<proposed_plan>` artifact*; multi-agent is a first-class tool surface |
| **Gemini CLI** | Event-driven scheduler; streaming tool-call feedback | Auto-compression when history exceeds **0.5 ×** model token limit, keeps latest **0.3**, plus a reverse token budget of **50k** for function responses and a configurable tool-output truncation threshold ([chatCompressionService.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/context/chatCompressionService.ts)) | `GEMINI_SANDBOX=true\|docker\|podman\|sandbox-exec\|runsc\|lxc`; macOS Seatbelt profiles; container-based isolation ([docs/cli/sandbox.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/sandbox.md)) | Subagents have their own context and a private ToolRegistry, are exposed to the main agent as a tool of the same name (`AgentRegistry` / `LocalAgentExecutor`), and can be routed remotely over A2A ([docs/core/subagents.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/core/subagents.md)) | Session history + git checkpointing for tool calls; `packages/a2a-server` task/event model | Hook system in core (registry/planner/runner), MCP, extensions, skills; auto memory proposes `MEMORY.md`/skill diffs and requires human approval via `/memory inbox` ([docs/cli/auto-memory.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/auto-memory.md)) | Human-confirmed memory (diff proposal + inbox) instead of silent writes |
| **OpenHands** | Event-driven agent step over an append-only EventStream | Condenser triggers on `max_tokens`/`max_size` (soft) or explicit request (hard); replaces the first half of events with one summary; condensation is a **tombstone `Condensation` event** applied by a `View`; hard context reset when structure would break ([condenser README](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/context/condenser/README.md)) | Runtime sandbox: Docker/Apptainer/remote/API sandboxed servers; agent-server conversation leases and event router ([examples](https://github.com/OpenHands/software-agent-sdk/tree/main/examples/02_remote_agent_server)) | Agent delegation + conversation fork examples; sub-agent visualizer | Append-only event store + conversation state, local or remote (`event_store.py`, `conversation/state.py`) | MCP tools, microagents/skills, automations | Compaction as reversible tombstones in one immutable log |
| **Aider** | Per-message edit loop; chat modes `code`/`architect`/`ask`/`help` ([modes.html](https://aider.chat/docs/usage/modes.html)) | Repo map built with tree-sitter, ranked by a graph algorithm, fitted to a token budget that adapts to chat state ([/repomap.html](https://aider.chat/docs/repomap.html)) | No sandbox, no permission gate; safety comes from diff review + git | Architect model proposes, editor model turns that into concrete edits (dual-model) | Git auto-commit after each edit, dirty files committed first, `/undo`, `--no-auto-commits`, model-written Conventional Commits ([git.html](https://aider.chat/docs/git.html)) | Config/YAML, `/commands`; not MCP-centric | Retrieval-as-compression: structured repo map instead of summarization |
| **Cline** | Dual Plan/Act modes; Plan cannot edit files or run commands; `/deep-planning` for large tasks; separate models per mode ([plan-and-act](https://docs.cline.bot/features/plan-and-act)) | Memory Bank + rules + skills; no public thresholds (low confidence) | Auto-approve settings; no OS sandbox documented | Agent Teams / Subagents in nav ([subagents](https://docs.cline.bot/features/subagents)) | **Shadow-git checkpoints**: commit after each tool use into a separate repo; restore any checkpoint, keep the conversation ([checkpoints](https://docs.cline.bot/features/checkpoints)) | Rules, Skills, Plugins, MCP, Hooks, connectors, scheduling | Shadow-git checkpointing incl. untracked files |
| **Cursor** | Agent loop + Plan Mode: Shift+Tab, clarifying questions, reviewable/editable plan before building ([plan-mode](https://cursor.com/docs/agent/plan-mode)) | Agent Review + context tooling; no public compaction thresholds found | Nav lists Hooks/MCP/Plugins/Skills/Subagents/Cloud Agents/Automations; sandbox details not read | Subagents + Cloud Agents + Automations + Bugbot in nav | Dedicated checkpoints page returns 404 on current docs — **uncertain** whether removed or moved | Plugins, Rules, Skills, Hooks, MCP | Cloud agents with PR routing/approval |
| **Windsurf (Devin docs)** | Cascade loop with modes, Arena mode, ACP preview | Memories & Rules, Skills, AGENTS.md, Workflows ([docs](https://docs.windsurf.com/windsurf/cascade/memories)) | Nav lists Worktrees, MCP, Cascade Hooks (low confidence) | Agent Command Center + Spaces (Kanban of local/cloud agents) | Not established (**uncertain**) | Skills, Workflows, MCP, Hooks, plugins | Many-agent oversight workbench |
| **SWE-agent** | ACI-governed thought/action loop; templated prompts and tool bundles in YAML configs | Windowed custom file viewer (≈100 lines/turn), search listing matching files only, explicit "no output" message; no compaction | No permission gate; isolation via per-instance containers | Single agent | Per-instance trajectories / run configs | New ACIs/tools via config, not MCP-first | ACI design as a first-order object: a linter blocks invalid edits ([aci.md](https://github.com/SWE-agent/SWE-agent/blob/main/docs/background/aci.md), [arXiv:2405.15793](https://arxiv.org/abs/2405.15793)) |
| **Frameworks** | LangGraph: graph nodes/edges. Agents SDK: agent loop with handoffs. MAF: graph workflows. Magentic-One: orchestrator + task ledger | LangGraph: summarization is an author-written node; checkpointers hold thread state, stores hold cross-thread memory ([persistence](https://docs.langchain.com/oss/python/langgraph/persistence.md)). Agents SDK: pluggable sessions incl. "Responses compaction sessions" with auto-compaction that can block streaming ([sessions](https://openai.github.io/openai-agents-python/sessions/)) | Agents SDK: SandboxAgent with capability manifest (filesystem/shell/memory/skills/compaction), permissions, snapshot spec, Unix/Docker sandbox clients | Agents SDK: handoffs + agent-as-tool; MAF: sequential/concurrent/handoff/group ([README](https://github.com/microsoft/agent-framework/blob/main/README.md)); Magentic-One generalist multi-agent system ([docs](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/magentic-one.html)) | LangGraph checkpointer = time travel, HITL, fault tolerance; MAF checkpointing + time travel; Agents SDK session resumption by session id | MAF hosts A2A; Agents SDK and MAF both speak MCP | LangGraph splits thread-scope vs cross-thread memory; Agents SDK turns sessions into a storage SPI plus sandbox capability packs |
| **MCP vs A2A** | — | — | — | A2A: agent-to-agent collaboration plane (discovery, agent cards, tasks, streaming) ([what-is-a2a](https://a2a-protocol.org/latest/topics/what-is-a2a/)) | — | MCP: connecting apps to external tools/data, spec version 2026-07-28 ([MCP docs](https://modelcontextprotocol.io/docs/getting-started/intro)) | A2A frames the stack as A2A (agents) + MCP (capabilities) + frameworks + models — horizontal vs vertical layering |

## Cross-cutting observations

1. **The append-only log as single source of truth has won.** OpenHands treats the EventStream as the
   only authoritative state and expresses forgetting as tombstone events applied by a view; Codex
   appends rollout JSONL (writer lock, reverse scanner, user-turn-boundary truncation); Seelex writes
   a JSON v8 append-only store with compact frames/heads — its "head + tail after `message_to`" is
   the same idea as Codex's turn-boundary truncation. Convergence, not divergence.
2. **Compaction is now a lifecycle, not a call.** Codex routes auto/manual compaction through
   pre/post-compact hooks plus a visible `ContextCompaction` item; Claude Code documents what
   survives per mechanism; OpenHands separates soft triggers from hard context resets. Seelex's
   window-external frames resemble Codex's non-summarizing token-budget compaction; its tool-result
   externalization with read-back is rarer — Gemini is the only peer found that bounds tool output by
   budget (50k function-response tokens) instead of summarizing it.
3. **Threshold philosophies differ more than mechanisms.** Gemini hardcodes ratios (0.5 trigger /
   0.3 preserved); Claude Code exposes a tunable window with settings/env/flag precedence; Codex
   calibrates from server-observed input usage. Seelex sits in the Codex camp but exposes no
   comparable precedence tiers.
4. **"Own context window" subagents are universal; inheritance policy is the differentiator.**
   Claude Code subagents start fresh unless `fork`; Codex exposes `fork_turns` (`none`/n/`all`);
   Gemini pairs a private tool registry with A2A routing. Seelex's independent-session subagents with
   snapshot injection equal `fork_turns=n` plus a formal prompt-block contract — a cleaner spec, but
   without Claude's per-subagent model/tool/permission frontmatter or Codex's nested spawning.
5. **Seelex's biggest gap is isolation depth.** Claude Code, Codex, Gemini (bubblewrap/Seatbelt/
   runsc), OpenHands (Docker/remote) and the OpenAI Agents SDK all ship OS- or container-level
   isolation. Seelex's two layers (path containment + allow/ask/deny gate) constrain *what the
   harness does*, not *what a spawned process can reach*; today's mitigation is hosting Seelex
   itself in a container.
6. **Permission UX converged on allow/ask/deny with "deny always wins"; Seelex lacks the autonomy
   dials.** Claude Code adds a classifier-driven `auto` mode, Cline/Cursor add auto-approve and
   plan-review gates, Codex pairs a sandbox matrix with approval policy. Seelex's gate has no
   documented classifier/auto tier.
7. **Hooks and skills are converging into one extension substrate; Seelex's transactional plugin
   reload is unusual.** Claude Code hooks (command/HTTP/MCP/prompt/subagent), Gemini's hook
   registry/planner/runner and Codex lifecycle hooks with managed-hooks-only policy are all
   lifecycle-event-bus designs, and `SKILL.md`-style skills now span Claude Code, Gemini and Codex.
   No peer reviewed here swaps tool visibility + system prompt + skills + MCP servers in one
   transaction or scopes tools by role the way Seelex does.
8. **User-visible rewind is table stakes; Seelex has only the plan-level half.** Claude Code
   (per-prompt snapshots + `/rewind`), Cline (shadow git after each tool use), LangGraph and MAF
   (time travel) all expose file-level rollback. Seelex's `checkpoint_store.go` resumes *plans*, not
   files.
9. **Two mechanisms Seelex lacks outright: cross-session memory and a judged goal loop.** LangGraph
   separates thread checkpointers from cross-thread stores, and Claude Code/Gemini/Codex all ship
   memory persistence. Claude Code `/goal` and Codex's `ext/goal` (accounting/metrics/runtime)
   add "keep going until a condition is judged met"; Seelex's worktable/tasklist tracks work but
   shows no equivalent judged-completion primitive in the materials reviewed.

### Confidence notes

- Partial/single-source: Cursor checkpoints (docs page 404 — do not assert removal), Windsurf
  memory/permission details (JS-rendered), Cline thresholds, Aider compaction (none documented).
- Not asserted: benchmark scores, context arithmetic beyond vendor statements, version numbers.
