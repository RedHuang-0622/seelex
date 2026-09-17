# Seelex — Open-Source Coding Agent Harness

Seelex is a local-first coding-agent harness built in Go. It turns LLM providers, tool calling, agentic workflows, context engineering, permission policy and persistence into an observable and recoverable software-engineering agent.

> Status: **Developer Alpha**. The Bubble Tea TUI is the default interface. The Wails/WebView GUI is usable but remains Alpha. Seelex is not advertised as production-ready or as an OS-level sandbox.

## What it implements

- streaming ReAct execution with bounded Effort profiles;
- optional WorkPlan DAG orchestration, typed nodes and task terminal states;
- parallel subagents with isolated sessions, parent-evidence injection and structured merge-back;
- session-scoped goal stacks with an independent adjudication role (ADVISOR/TechLeader), frame-throttled
  review, a bounded directive mailbox and an append-only governance audit;
- agent-team factory presets and a four-source work table (plan / tasklist / subagent / todo);
- context-window policy, reversible compaction (compaction-as-DAG), prompt stacks and externalized tool results;
- layered memory: related-memory blocks, history read-back indexed by the compaction stack, and
  user/project `MEMORY.md` indexes;
- OpenAI-compatible endpoints, including DeepSeek deployments that satisfy the streaming and
  tool-calling contract (the provider name is a free-form string, but only OpenAI-compatible
  endpoints are exercised);
- P2C account pooling, role-aware routing (agent / subagent / goalplan / websearch) and lease-until-EOF
  streaming safety;
- project-scoped tools with roots resolved per session, plus a Linux-style permission model
  (subject × routing group × rwx bits): `root` / `sub` / `emp_ro` / `emp_rw` subjects over `ro` / `rw` /
  `rw_session` / `rw_desktop` / `ctl` / `adm` groups, where a missing bit means the tool is simply not
  routable for that subject (subagents structurally lack `ctl` and `adm`), and human approval covers every `ask`;
- per-session permission tiers for the main agent (`manual` / `edit` / `auto` / `full`): a tier only
  removes `ask` rules, never adds an allow and never touches a dangerous `deny`, so `rm -rf` roots,
  `dd if=* of=*` and `mkfs*` stay hard-blocked in every tier; the `full` short-circuit applies to the
  `root` subject only, so subagents and employees still escalate through the approval panel;
- multimodal input and desktop control sharing one session media partition: screenshots are stored
  content-addressed, referenced from tool results and attached to the next model request once, while
  desktop-changing tools (focus, mouse/keyboard injection) stay approval-gated and are invisible to
  subagents;
- user image/document attachments, with an inline-text fallback for documents;
- declarative plugins, Agent Skills and dynamically scoped MCP servers, plus plugin/skill/MCP
  self-management tools;
- scheduled (periodic and one-shot) tasks, web search providers (tavily / bochaai / searxng) and
  worktree-scoped execution;
- JSON v8 session persistence with an append-only order log, project/session isolation, per-module head
  publishing, a media partition and plan/task/goal stack channels (the SQLite, PostgreSQL and Redis enum
  values are retired and fail with explicit guidance; new backends wait for an interface rewrite);
- a shared headless Application Core consumed by the TUI, the desktop GUI, a headless RPC frontend and a
  diagnostic backend console;
- deterministic offline scenarios, cross-platform CI, race/coverage gates and release archive audits.

Seelex builds on the [Seele](https://github.com/RedHuang-0622/Seele) agent runtime. The `seelebridge/` anti-corruption layer keeps product semantics separate from runtime primitives.

## Quick verification

```bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
go test ./... -count=1 -timeout=120s
go build .
```

These checks do not require a live model account or network access. To run an agent, copy `config/accounts.example.yaml` to the ignored local `config/accounts.yaml` and provide your own endpoint and credentials.

```bash
go run . -frontend tui -permission manual
```

For an OpenAI-compatible DeepSeek endpoint, keep `provider: openai` and configure a compatible model and base URL in the local account file. Never commit that file.

## Architecture

### Layered architecture

```mermaid
flowchart TB
    subgraph L1["Clients (consume Snapshot / Event, submit Actions)"]
        TUI["TUI (Bubble Tea)"]
        GUI["GUI (Wails / WebView)"]
        HL["headless RPC"]
        BE["backend console"]
    end

    subgraph L2["application/ — use-case orchestration + authoritative state"]
        SVC["Service facade"]
        STATE["model · event · approval · contract · prompt"]
    end

    subgraph L3["seelebridge/ — runtime anti-corruption layer"]
        RT["Runtime assembly and shutdown"]
        TOOLS["tools: Router · RegistryState · PermissionGate"]
        ORCH["plan · node · fork · scheduler · task"]
        CAP["account · mcp · plugin · session · search"]
    end

    CTX["seelexctx/ — Assembler · Compressor · DAG · Memory · Merger"]
    PERSIST["sessionstore/ · session/ · workspace/"]
    EXT["plugin/ · skill/ · mcpstack/"]
    SEELE["Seele runtime: agent · session · tools · workplan · accountpool · mcp"]

    TUI --> SVC
    GUI --> SVC
    HL --> SVC
    BE --> SVC
    SVC --> STATE
    SVC --> RT
    SVC --> CTX
    SVC --> PERSIST
    RT --> TOOLS
    RT --> ORCH
    RT --> CAP
    CTX --> RT
    EXT --> RT
    TOOLS --> SEELE
    ORCH --> SEELE
    CAP --> SEELE
    PERSIST -.->|history / records| RT
```

### Use cases

```mermaid
flowchart LR
    DEV(("Developer"))
    LEAD(("Team lead"))
    AUTHOR(("Extension author"))

    UC1(["Submit a task, watch streaming execution"])
    UC2(["Approve tools, pick the session permission tier"])
    UC3(["New / switch / fork / resume sessions"])
    UC4(["Inspect subagent nodes and evidence in the Plan panel"])
    UC5(["Register employees and teams, summon a team"])
    UC6(["Set a goal stack judged by the ADVISOR role"])
    UC7(["Audit the frame ledger and tool evidence"])
    UC8(["Install plugins / skills / MCP to switch the capability surface"])
    UC9(["Route model accounts and control cost"])
    UC10(["Let the agent drive the desktop with screen evidence"])

    SYS["Seelex Application Core"]

    DEV --> UC1
    DEV --> UC2
    DEV --> UC3
    DEV --> UC4
    LEAD --> UC2
    LEAD --> UC5
    LEAD --> UC6
    LEAD --> UC7
    AUTHOR --> UC8
    AUTHOR --> UC9
    AUTHOR --> UC10
    UC1 --> SYS
    UC2 --> SYS
    UC3 --> SYS
    UC4 --> SYS
    UC5 --> SYS
    UC6 --> SYS
    UC7 --> SYS
    UC8 --> SYS
    UC9 --> SYS
    UC10 --> SYS
```

### One request, end to end

```mermaid
sequenceDiagram
    autonumber
    participant U as User
    participant F as TUI / GUI
    participant S as application.Service
    participant R as seelebridge.Runtime
    participant L as Seele ReActLoop
    participant T as tools.Registry + PermissionGate

    U->>F: prompt
    F->>S: Submit
    S->>R: startChat / ChatStreamFor
    R->>L: agent.ChatStream
    L-->>F: streaming tokens (via EventHub projection)
    L->>T: tool_call
    T->>S: Interaction approval request (ask)
    S-->>F: approval dialog
    U->>F: approve
    F->>S: ResolveInteraction
    S->>T: allow and execute
    T-->>L: tool_result (oversized results archived as result_ref)
    L-->>S: turn finished
    S->>S: persist append-only order log
    S-->>F: Snapshot + Event delta
```

```text
TUI / Wails GUI / headless RPC / backend console
       │ Snapshot · Event · Action
application/ — Chat · Task · Plan · Goal/Govern · AgentTeam · Worktable · Session · Workspace
       │
seelebridge/ — runtime adaptation · tools (incl. computer use) · multimodal · node scope · provider/account routing
       │
Seele runtime — Agent · ReAct · Tool Registry · WorkPlan · Account Pool · MCP

plugin/ · skill/ · mcpstack/ · seelexctx/ · sessionstore/ · session/
```

The important design choice is that frontends do not own the agent state machine. They consume a versioned Snapshot + Event Delta protocol from the same Application Core, which keeps chat, plan, approval and persistence behavior testable without a UI or live model.

## Current limitations

- ProjectScope and permission rules are not an OS, container or VM sandbox.
- There is no git checkpoint/rewind abstraction yet.
- There is no semantic repository index, repo map or IDE extension.
- Multi-agent orchestration is in-process and is not an A2A Protocol implementation. The team turn
  scheduler (`TurnScheduler`) is only **partially wired**: linked-list order (`Move` / `Remove` /
  `Restore`), `SetPrefix`, `NoteTurn`, `SyncOrder` and `Snapshot` have production consumers, while
  `Next()` / `Advance()` are primitives without one; the goal governance seat loop is what actually
  drives turns.
- The `review-team` `reviewer` and `research-team` `researcher` roles only have role sessions and member
  rows, no executor yet (`RolesWithExecutor` contains only `user` / `main` / `tl`); the assembly surface
  states this explicitly via `DesignNotice`.
- Standard SWE-bench or Terminal-Bench results have not been published.
- Real WebView E2E is not yet a release gate.
- Total statement coverage is 58.6% (2026-09-14); the TUI (35.6%) trails the core orchestration packages.
- The media partition enforces quotas and reference-based collection, but automatic per-session garbage
  collection is still pending.

For the detailed implementation rationale, configuration and module map, read the [Chinese README](README.md) and [documentation index](docs/README.md).

## Contributing and security

The repository is currently maintained primarily by its original author; external reviews and contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

Licensed under the [MIT License](LICENSE).
