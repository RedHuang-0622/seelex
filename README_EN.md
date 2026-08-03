# Seelex — Open-Source Coding Agent Harness

Seelex is a local-first coding-agent harness built in Go. It turns LLM providers, tool calling, agentic workflows, context engineering, permission policy and persistence into an observable and recoverable software-engineering agent.

> Status: **Developer Alpha**. The Bubble Tea TUI is the default interface. The Wails/WebView GUI is usable but remains Alpha. Seelex is not advertised as production-ready or as an OS-level sandbox.

## What it implements

- streaming ReAct execution with bounded Effort profiles;
- optional WorkPlan DAG orchestration, typed nodes and task terminal states;
- parallel subagents with isolated sessions, parent-evidence injection and structured merge-back;
- context-window policy, reversible compaction, prompt stacks and externalized tool results;
- OpenAI-compatible endpoints, including compatible DeepSeek deployments, plus Anthropic providers;
- P2C account pooling, role-aware routing and lease-until-EOF streaming safety;
- project-scoped tools, allow/ask/deny permission policy and human approval;
- declarative plugins, Agent Skills and dynamically scoped MCP servers;
- JSON, SQLite, PostgreSQL and Redis session persistence;
- a shared headless Application Core consumed by both TUI and GUI;
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

```text
TUI / Wails GUI
       │ Snapshot · Event · Action
application/ — Chat · Task · Plan · Approval · Session · Workspace
       │
seelebridge/ — runtime adaptation · node scope · provider/account routing
       │
Seele runtime — Agent · ReAct · Tool Registry · WorkPlan · Account Pool

plugin/ · skill/ · mcpstack/ · seelexctx/ · sessionstore/
```

The important design choice is that frontends do not own the agent state machine. They consume a versioned Snapshot + Event Delta protocol from the same Application Core, which keeps chat, plan, approval and persistence behavior testable without a UI or live model.

## Current limitations

- ProjectScope and permission rules are not an OS, container or VM sandbox.
- There is no git checkpoint/rewind abstraction yet.
- There is no semantic repository index, repo map or IDE extension.
- Multi-agent orchestration is in-process and is not an A2A Protocol implementation.
- Standard SWE-bench or Terminal-Bench results have not been published.
- Real WebView E2E is not yet a release gate.

For the detailed implementation rationale, configuration and module map, read the [Chinese README](README.md) and [documentation index](docs/README.md).

## Contributing and security

The repository is currently maintained primarily by its original author; external reviews and contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

Licensed under the [MIT License](LICENSE).
