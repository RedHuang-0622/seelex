# docs/arch — One-line summaries (agent fk1)

Reviewed 2026-09-08. Two docs read from `docs/arch/`.

- **docs/arch/README.md** — Defines docs/arch as the home for long-lived cross-module architecture facts (dependency direction, protocol semantics, concurrency/storage models, known structural flaws), indexes every doc in the directory, and lays out the session data-flow stack for a conversation operation (GUI/TUI → gui.Bridge → application.Service → session_runtime.Coordinator / sessionstore.Router → adapters.EnginePort → framework session).

- **docs/arch/seele-v2-runtime-architecture.md** — Describes the stable (migration-completed, 2026-08-01) runtime boundary where Seelex pins the remote module `github.com/RedHuang-0622/Seele v0.1.1` via the `seelebridge/` composition root — Seele owns execution (agent/tools/session/workplan/seelectx/accountpool/event/telemetry), Seelex owns product semantics (task lifecycle, plan DSL, project scope, visibility policy, context compression, telemetry), and nodes decouple from the task state machine through `event.Sink` projection; an open migration-cleanup section still lists old-engine references (`Seele/engine`, `agent/core/*`) that keep `go build ./...` broken.
