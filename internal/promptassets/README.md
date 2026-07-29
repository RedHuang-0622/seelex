# Prompt Assets

`internal/promptassets/assets/` is the source of truth for Seelex-owned
system, effort, and PlanAct prompts. Assets are embedded at build time so a
release binary has no mutable prompt-file dependency.

## Structure

| Path | Purpose |
|---|---|
| `assets/system/` | Identity and cross-cutting engineering/evidence rules. |
| `assets/effort/` | One user-selected effort policy per level. |
| `assets/plan/` | Mandatory preflight and explicit recovery-plan templates. |

## Authoring rules

Each complex rule follows: principle, positive scope, negative scope,
paired do/don't examples, fallback, and a self-check. Use `**MUST**` or
`**NEVER**` only for runtime-enforced or safety-critical facts. Template
variables are limited to `PlanData` policy fields; do not add user content,
credentials, or hidden runtime state to assets.

## Verification

```text
go test ./internal/promptassets ./application/prompt ./seelebridge -count=1
go build ./...
```
