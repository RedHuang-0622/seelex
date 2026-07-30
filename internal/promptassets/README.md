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

Plan preflight and replan assets lead with complete, copyable canonical JSON
shapes. A model chooses one shape and changes only node `input` text; structural
rules and the final schema checklist follow the positive examples.

## Terminal protocol

System instructions require a tool-using request to converge through
`task_complete` or `task_failed`. The former records delivery and evidence;
the latter records bounded failure facts. Prompt prose does not replace the
Application-side payload validation or token/checkpoint context controller.

## Verification

`PlanActHarnessCases` 是确定性的提示词回归 harness。它覆盖“验证完成后交付”“导出 Markdown 后再答复”和“拒绝未计划的第二轮验证”等正反场景；它检查渲染后的 system + effort 指令仍包含这些收束契约，不以真实模型输出冒充确定性测试。

```text
go test ./internal/promptassets ./application/prompt ./application/core ./seelebridge -count=1
go build ./...
```
