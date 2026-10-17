# M5 剩余工作切片表（可交给串行子代理执行）

- 工作包：`docs/2026-10-15-architecture-reshape/`
- 分支：`refactor/architecture-reshape`（`main` 未动）
- 生成时间：2026-10-16
- 事实源：`tmp/reshape/remaining.py` 对 `plan.md` §2 表格逐条比对当前工作树（329 条 from，
  213 条已落位、116 条仍在原位），以及本文件列出的实测命令。

## 0. 这份文件回答什么

`plan.md` 的 M1–M4 已达成（`seelebridge/` 目录下 `.go` 归零）；§2 表格里仍有 116 条
"from 路径还在原位"，其中约 90 条是真·待搬迁（其余是 §2.1 "不动"项与 M4 已按新落点重建的
同义条目）。这份文件把它们切成**可独立提交、可独立回退**的切片，并写明每个切片的文件所有权，
让并发/串行执行时不互相改同一个文件。

复跑清单（这是唯一的进度事实源）：

```text
python tmp/reshape/remaining.py
```

## 1. 待搬迁清单（按 plan 小节归并）

| 组 | plan 小节 | 内容 | 规模 | 依赖 |
|---|---|---|---|---|
| R1 | §2.10 | `internal/adapters/` → `wiring/adapters/`；空目录 `seelebridge/workunit/`、`internal/toolmetrics/` 删 | 7 文件 2454 行 | 无 |
| R2 | §2.10/§2.12 | `internal/toolrecord/`（208）与 `application/core/tool_record.go` 的判据 → `store/record/`（会话级 module，钩子直连 JSONL，**不进 Snapshot**） | 2 文件 | R1（同为 `internal/*` 归位） |
| R3 | §2.3 | `application/model`（5 文件 1037 行）→ `middle/protocol/model/`；`application/event`（346）→ `middle/protocol/event/`；`application/core/internal/state`（27）→ `kernel/state/` | 7 文件 | 无（`contract` 现引用 `application/model`，搬迁后须同步改路径） |
| R4 | §2.4 | 中间层叶包：`application/core/view_state/` → `middle/view/state/`；`subagent_view/` → `middle/view/subagent/`；`worktable/` → `middle/view/worktable/` | 3 包 1042 行 | R3（`middle` 顶层先立起来） |
| R5 | §2.4 | 中间层根文件（`project_board.go`、`runtime_projection.go`、`diagnostics.go`、`perf_stats.go`、`service_snapshot.go`…）→ `middle/view/*` | ≈1200 行 | R4 + 核心切分技术（§2） |
| R6 | §2.4 | 常驻/切换域（`resident_lru.go`、`content_lru.go`、`snapshot_budget.go`、`session_*.go`、`service_scheduler/queue/*`、`async_completion.go`、`service_interaction.go`、`workspace_usecase.go`、`error_*.go`、`session_history.go`）→ `middle/residency/`、`middle/switch/*`、`middle/view/errors/`；`session/` → `middle/switch/sessionusecase/` | ≈4000 行 | R5 + 核心切分技术 |
| R7 | §2.5–2.8 | `application/core` 的引擎域归位：`chat.go`+`chat/` → `backend/engine/react/`；`task_context/` → `backend/engine/task/`；`context_runtime/`+`context_compact*.go`+`history_safety.go` → `backend/engine/context/`；`prompt_layer/`+`application/prompt/stack.go` → `backend/engine/prompt/`；`input_router/`+`service_input.go`+`input.go`+`command.go`+`completion.go`+`context_control/` → `backend/engine/input/`；`session_runtime/` → `backend/engine/session/`；`skill_activate.go`+`skill_context.go` → `backend/engine/skill/`；`seelexctx/` → `backend/engine/context/seelectx/`；`internal/promptassets` → `backend/engine/prompt/assets/` | ≈11k 行 | 核心切分技术 |
| R8 | §2.5–2.8 | 多代理与工具域：`goal/`+`goal_*.go` → `backend/multiagent/goal/`；`agentteam/`+`role_session.go` → `backend/multiagent/team/`；`teamwork_service.go` → `backend/multiagent/teamwork/`；`resume/`+`service_subagent_resume.go` → `backend/multiagent/resume/`；`review_files.go`、`reference_tools.go`、`tool_hooks.go`、`plan_tools.go` → `backend/tool/*`；`service_knobs.go` → `backend/limit/`；`session_permission_tier.go` → `backend/limit/tier/`；`service_account_window.go` → `backend/limit/accountwindow/` | ≈6k 行 | 核心切分技术 |
| R9 | §2.5 | `plugin/`、`skill/`、`mcpstack/` → `backend/engine/{plugin,skill,mcp}/`（一个目录一个 package） | 17 文件 2834 行 | 无（叶包，可早做） |
| R10 | §2.10/§2.2 | `application/core/aliases.go`（204）由门面取代后删；门面收窄五组窄接口 + I5 断言 | — | R5–R8 |
| R11 | §2.5/§2.10 | M4 尾：`backend/engine/assemble/ports.go`（1020 行兼容别名+逐行委托）删；`Runtime` 按域下沉 | 39 文件 9555 行 | R5–R8（同时收口） |

## 2. 核心切分技术（R5–R8 的施工法，必须逐条遵守）

`application/core` 是**一个 package** 132 个生产文件、27k 行，`Service` 是跨文件单类型
（`Service = *serviceState + components`，`serviceState` 已内嵌 `*state.Core` 与四个
状态组）。因此 §2.4 的"搬"**不是 `git mv`**，而是两段式：

**第一段（同包内、可独立回退）——接收者转移**

1. 为目标域建 `<domain>_component.go`：`type xComponent struct { host <窄接口>; <该域自有状态> }`。
2. 该域的方法从 `func (service *Service) M(...)` 改为 `func (c *xComponent) M(...)`；
   方法体逐字保留，只把 `service.<别的域的东西>` 改成 `c.host.<端口方法>`。
3. `Service` 持有该组件字段；对**门面已导出的方法**保留一行转发
   （`func (service *Service) M(...) { return service.x.M(...) }`），保证门面别名与外部
   调用点零改动。未导出的内部方法直接改调用点为 `service.x.M(...)`。
4. 验收：`go build ./...` + `gofmt`；**同一份输入跑旧路径/新路径得到同一结果**的跨实现
   一致性用例（`AGENTS.md` §8.6）。第一段结束即可单独提交。

**第二段——分包**

5. 把该组件文件（及其专属测试）整块搬进 `middle/...` 或 `backend/...`，类型导出为
   `type X struct`，构造签名 `NewX(host Host) *X`，`Host` 定义在**新包自己的
   `ports.go`**（跨板块共用才上 `contract/`）。
6. `Service` 侧的转发方法保留（这是"门面稳定"的钉子）；装配在 `application/core` 的
   `serviceAssembler` 里注入 `Host`。
7. 验收：`go list -deps ./middle/<...>` 里不得出现 `application/core`（反向依赖即失败）；
   `middle` 与 `backend` 之间无环（I5 断言）。

**禁止**：为了让文件搬得动而改行为、改文案、改字段名、改 JSON 标签、改错误字符串；
测试断言只允许改 import 路径与接收者。

## 3. 文件所有权（防并发改同一文件）

以下文件被多个切片需要，**同一时刻只允许一个切片改**，且按表内顺序串行：

| 共享文件 | 被谁改 | 约定 |
|---|---|---|
| `e2e/layout_test.go`（模块表） | R1、R3、R4、R7、R9 | 各切片**只追加/改自己那一行**；冲突时后做的切片先 rebase 式重读再改 |
| `e2e/facade_boundary_test.go`（闭包清单 + 上限） | R3、R4、R7、R9、R10 | 只许**收缩**；上限常量随之下降，不许为了过闸抬高 |
| `e2e/subagent_status_vocabulary_gate_test.go`（写死路径表） | R4、R7 | 路径改新落点，判据一字不动 |
| `wiring/bootstrap/*.go` | R1、R2、R7、R9、R11 | 改 import 路径或构造调用，不许把业务规则塞回来 |
| `wiring/registry/*.go`、`internal/adapters/*` | R1、R2、R8 | 同上 |
| 各模块 `README.md` + 索引 | 所有切片 | 只改自己新落点的 README；索引由 `scripts/gen_core_readme_index.py` 与既有生成器产出 |

## 4. 每个切片的交付格式（我会按此逐条复核）

1. commit SHA + `git show --stat` 摘要（一个切片一个 commit）
2. **删除清单**：`原位置 → 新唯一位置` 逐条列出
3. 保真证据：为什么是"只搬位置"（import/接收者差异、`go list -deps` 前后对比、跨实现一致性用例）
4. 原始命令输出：`gofmt -l .`、`go build ./...`、`go build -tags "gui,desktop,production" ./...`、
   `go vet ./...`、`go test ./e2e/ -count=1`、`go test ./... -count=1 -timeout=900s`
   失败集合与基线的**逐条**比对
5. 偏差登记：任何没按 plan 字面做的地方 + 理由

**基线（开工前即红，不许去修）**：`application/core` 3 例；`backend/tool/registry` 1 例
Windows 临时目录偶发。

## 5. 尚未裁决/未开始（不在本表内，别顺手做）

- M2-D（loop 预警 `Verdict`）、M2-C5（中间件定序：路径门前移）、V10 看板读数通道 —— 用户已裁决延后。
- `seelebridge/.seelex/` 运行期 trace 数据：删除属"移动/删除用户数据"，须用户确认。
- `R11` 的 `Runtime` 按域下沉与 `ports.go` 删除是同一收口动作，等 R5–R8 决定最终落点后再做。
