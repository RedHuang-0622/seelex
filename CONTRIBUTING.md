# Contributing to Seelex

Seelex is currently maintained primarily by its original author. External bug reports, reproductions, design reviews, documentation fixes and focused pull requests are welcome; contribution history is never fabricated or inferred from automated commits.

## Before you start

For a bug, open an Issue with a minimal reproduction, expected behavior, actual behavior and environment details. For a material API, persistence, security or architecture change, open a proposal first so the boundary can be reviewed before implementation.

Never attach real `config/accounts.yaml`, API keys, tokens, passwords, DSNs, private prompts, session data or local filesystem paths. Use `config/accounts.example.yaml` and redact logs.

Read [AGENTS.md](AGENTS.md) and the README in every module you intend to change. Current code and tests are the source of truth; documents under `docs/YYYY-MM-DD-topic/` are historical work packages unless explicitly marked as current architecture.

## Architecture boundaries

- `application/` owns product use cases and frontend-neutral state.
- `tui/` and `gui/` consume Application DTOs, snapshots and events; they do not duplicate the business state machine.
- `seelebridge/` adapts Seele runtime capabilities and prevents upstream types from leaking through the product.
- `workspace/`, `session/` and `sessionstore/` preserve project/session scope and atomic persistence semantics.
- Tool additions must pass ProjectScope, visibility and permission boundaries.
- Planned behavior must be labelled as planned; do not describe it as implemented.

## Development workflow

Use Go 1.25 or newer. Keep changes focused and add tests at the owning layer.

```bash
go test ./... -count=1 -timeout=120s
go vet ./...
go build ./...
node --test gui/frontend/dist/*.test.mjs
```

On Windows, validate the real Wails adapter when GUI code changes:

```powershell
go build -tags "gui,desktop,production" ./...
```

Linux CI additionally runs the race detector, coverage and release archive safety checks. A pull request does not need a live model account unless it explicitly changes provider integration; deterministic tests must remain network-free by default.

## Pull requests

A reviewable pull request should explain the problem, the chosen trade-off, compatibility impact, tests run and remaining limitations. Update module documentation when behavior, flags, schema, persistence or public interfaces change.

Run `gofmt`, `git diff --check` and the relevant tests before submission. Do not include generated runtime caches, private configuration or unrelated formatting changes.

By contributing, you agree that your contribution is licensed under the repository's [MIT License](LICENSE).
